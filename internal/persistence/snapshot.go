package persistence

import (
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"os"
	"path/filepath"
	"sort"
	"time"

	"minikv/internal/kverrors"
)

// Snapshot file layout — all integers little-endian. The format is
// deliberately simple: a header, then one record per live entry, then a
// CRC over everything after the magic. Documented in docs/design-decisions.md.
//
//	offset  size  field
//	0       4     magic: SnapshotMagic ("KVMS")
//	4       1     format version: SnapshotVersion
//	5       8     entry count (uint64)
//	13      8     created-at, unix seconds (uint64)
//	21      N     entries, each:
//	                8  key length
//	                8  value length
//	                8  expiration, unix nanoseconds; 0 = none
//	                K  key bytes
//	                V  value bytes
//	end     4     CRC32-Castagnoli of bytes [4, end)
const (
	SnapshotMagic   uint32 = 0x534D564B // "KVMS"
	SnapshotVersion byte   = 1

	snapshotHeaderSize = 21
	snapshotCRCSize    = 4
)

// SnapshotStats summarizes one snapshot write for logs and status.
type SnapshotStats struct {
	// Entries is the number of live entries written.
	Entries int
	// Bytes is the size of the snapshot file in bytes.
	Bytes int64
	// Duration is how long the atomic write took.
	Duration time.Duration
	// Path is the final snapshot file path.
	Path string
}

// WriteSnapshot atomically replaces the snapshot file with the given data.
//
// Durability sequence required by the specification:
//  1. Write to a temporary file in the same directory.
//  2. Flush and fsync the temporary file.
//  3. Close it.
//  4. Rename it over the final snapshot path (atomic on POSIX; best-effort
//     on Windows where an existing target can make rename fail).
//  5. Best-effort fsync of the parent directory so the rename itself is
//     durable where the platform supports directory syncs.
//
// On any failure the previous snapshot is left untouched and a typed error
// is returned; no data is deleted.
func WriteSnapshot(dir string, data map[string][]byte, expirations map[string]time.Time, now time.Time) (SnapshotStats, error) {
	const op = "snapshot.Write"

	start := time.Now()
	if data == nil {
		data = map[string][]byte{}
	}

	finalPath := filepath.Join(dir, SnapshotFileName)
	tmpPath := filepath.Join(dir, SnapshotTmpName)

	frame, err := encodeSnapshot(data, expirations, now)
	if err != nil {
		return SnapshotStats{}, err
	}

	f, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return SnapshotStats{}, kverrors.Wrap(kverrors.SnapshotFailure, op, err,
			"could not create the temporary snapshot file %s: check disk space and permissions", tmpPath)
	}

	// Write, flush, fsync, close. Any failure removes only the temp file.
	if _, err := f.Write(frame); err != nil {
		_ = f.Close()
		_ = os.Remove(tmpPath)
		return SnapshotStats{}, kverrors.Wrap(kverrors.SnapshotFailure, op, err,
			"could not write snapshot data: the previous snapshot is unchanged")
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(tmpPath)
		return SnapshotStats{}, kverrors.Wrap(kverrors.SnapshotFailure, op, err,
			"could not fsync the temporary snapshot: the previous snapshot is unchanged")
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return SnapshotStats{}, kverrors.Wrap(kverrors.SnapshotFailure, op, err,
			"could not close the temporary snapshot: the previous snapshot is unchanged")
	}

	// Rename into place. If this fails, remove the temp file and report.
	if err := os.Rename(tmpPath, finalPath); err != nil {
		_ = os.Remove(tmpPath)
		return SnapshotStats{}, kverrors.Wrap(kverrors.SnapshotFailure, op, err,
			"could not replace the snapshot file: the previous snapshot is unchanged; on Windows a locked target file can cause this")
	}

	// Best-effort directory sync so the rename is durable where supported.
	syncDir(dir) //nolint:errcheck — failure is non-fatal and already logged nowhere sensitive

	return SnapshotStats{
		Entries:  len(data),
		Bytes:    int64(len(frame)),
		Duration: time.Since(start),
		Path:     finalPath,
	}, nil
}

// syncDir fsyncs a directory where the platform supports it (POSIX). On
// Windows, directory handles cannot be opened this way and the error is
// intentionally ignored: the rename remains atomic within the filesystem
// even without the directory sync.
func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

// encodeSnapshot serializes the live data set into the snapshot format.
func encodeSnapshot(data map[string][]byte, expirations map[string]time.Time, now time.Time) ([]byte, error) {
	const op = "snapshot.Encode"

	keys := make([]string, 0, len(data))
	for k := range data {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	size := snapshotHeaderSize + snapshotCRCSize
	for _, k := range keys {
		size += 24 + len(k) + len(data[k])
	}
	buf := make([]byte, size)

	binary.LittleEndian.PutUint32(buf[0:4], SnapshotMagic)
	buf[4] = SnapshotVersion
	binary.LittleEndian.PutUint64(buf[5:13], uint64(len(keys)))
	binary.LittleEndian.PutUint64(buf[13:21], uint64(now.Unix()))

	pos := snapshotHeaderSize
	for _, k := range keys {
		v := data[k]
		var expNano uint64
		if t, ok := expirations[k]; ok && !t.IsZero() {
			expNano = uint64(t.UnixNano())
		}
		binary.LittleEndian.PutUint64(buf[pos:pos+8], uint64(len(k)))
		binary.LittleEndian.PutUint64(buf[pos+8:pos+16], uint64(len(v)))
		binary.LittleEndian.PutUint64(buf[pos+16:pos+24], expNano)
		pos += 24
		copy(buf[pos:], k)
		pos += len(k)
		copy(buf[pos:], v)
		pos += len(v)
	}

	crc := crc32.Checksum(buf[4:pos], crcTable)
	binary.LittleEndian.PutUint32(buf[pos:], crc)
	return buf, nil
}

// LoadSnapshot reads the snapshot file if it exists. A missing file is not
// an error: it returns (nil, false, nil). Any other anomaly — bad magic,
// version, truncation, CRC mismatch — is a SnapshotFailure error; a
// corrupted snapshot is never silently skipped.
func LoadSnapshot(dir string) (map[string][]byte, map[string]time.Time, bool, error) {
	const op = "snapshot.Load"

	path := filepath.Join(dir, SnapshotFileName)
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, false, nil
		}
		return nil, nil, false, kverrors.Wrap(kverrors.SnapshotFailure, op, err,
			"could not read the snapshot file: check permissions on the data directory")
	}

	data, expirations, err := decodeSnapshot(raw)
	if err != nil {
		return nil, nil, false, kverrors.Wrap(kverrors.SnapshotFailure, op, err,
			"the snapshot file %s is invalid; preserve it and see docs/recovery.md", path)
	}
	return data, expirations, true, nil
}

// decodeSnapshot parses and validates the snapshot format, including the
// final CRC over everything after the magic.
func decodeSnapshot(raw []byte) (map[string][]byte, map[string]time.Time, error) {
	if len(raw) < snapshotHeaderSize+snapshotCRCSize {
		return nil, nil, fmt.Errorf("snapshot too short: %d bytes", len(raw))
	}
	if binary.LittleEndian.Uint32(raw[0:4]) != SnapshotMagic {
		return nil, nil, fmt.Errorf("bad snapshot magic: not a MiniKV snapshot file")
	}
	if raw[4] != SnapshotVersion {
		return nil, nil, fmt.Errorf("unsupported snapshot version %d", raw[4])
	}

	count := binary.LittleEndian.Uint64(raw[5:13])

	// The trailing CRC covers bytes [4, len-crc).
	body := raw[:len(raw)-snapshotCRCSize]
	got := binary.LittleEndian.Uint32(raw[len(raw)-snapshotCRCSize:])
	want := crc32.Checksum(body[4:], crcTable)
	if got != want {
		return nil, nil, fmt.Errorf("snapshot checksum mismatch: recorded %08x, computed %08x", got, want)
	}

	data := make(map[string][]byte, count)
	expirations := make(map[string]time.Time, count)

	pos := snapshotHeaderSize
	for i := uint64(0); i < count; i++ {
		if pos+24 > len(body) {
			return nil, nil, fmt.Errorf("truncated snapshot entry header at offset %d", pos)
		}
		klen := binary.LittleEndian.Uint64(body[pos : pos+8])
		vlen := binary.LittleEndian.Uint64(body[pos+8 : pos+16])
		expNano := binary.LittleEndian.Uint64(body[pos+16 : pos+24])
		pos += 24

		if klen == 0 || pos+int(klen)+int(vlen) > len(body) {
			return nil, nil, fmt.Errorf("invalid snapshot entry lengths at offset %d", pos)
		}
		key := string(body[pos : pos+int(klen)])
		pos += int(klen)
		val := make([]byte, vlen)
		copy(val, body[pos:pos+int(vlen)])
		pos += int(vlen)

		data[key] = val
		if expNano != 0 {
			expirations[key] = time.Unix(0, int64(expNano))
		}
	}
	return data, expirations, nil
}
