package persistence

import (
	"bufio"
	"encoding/binary"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"minikv/internal/kverrors"
)

// On-disk WAL frame layout — all integers little-endian. Documented for
// users in docs/design-decisions.md; this comment is the implementation
// source of truth.
//
//	offset  size  field
//	0       4     magic: WALMagic ("KVMK")
//	4       1     format version: WALVersion
//	5       1     operation type: OpSet=1 / OpDelete=2
//	6       8     key length (uint64)
//	14      8     value length (uint64); 0 for OpDelete
//	22      8     expiration, unix nanoseconds; 0 = no expiration
//	30      K     key bytes
//	30+K    V     value bytes (absent for OpDelete)
//	30+K+V  4     CRC32-Castagnoli of bytes [4, 30+K+V) — everything after magic
//
// The CRC covers the version and payload so a corrupted version byte or
// body is detected. A reader stops at the first bad frame and reports the
// offset; it never rewrites or truncates the file.
const (
	WALMagic   uint32 = 0x4B4D564B // "KVMK"
	WALVersion byte   = 1

	frameHeaderSize = 30
	crcSize         = 4
)

// crcTable is the shared CRC32-Castagnoli table (safe for concurrent use).
var crcTable = crc32.MakeTable(crc32.Castagnoli)

// maxKeyOrValueSize bounds a single length field so a corrupted header can
// never trigger a huge allocation. It comfortably exceeds the engine limits
// (512 B keys, 1 MiB values) with room for growth.
const maxKeyOrValueSize = 4 << 20

// Entry is one decoded WAL record ready to apply to a store.
type Entry struct {
	Op        OpType
	Key       []byte
	Value     []byte // nil for OpDelete
	ExpiresAt time.Time
}

// WAL is an append-only, checksummed write-ahead log. Appends serialize on
// wmu; the buffer is flushed (and fsynced under DurabilityAlways) before
// Append returns, so a completed Append means the record will survive an
// abrupt process termination.
type WAL struct {
	path string

	// wmu serializes appends and guards file/buf/offset below.
	wmu    sync.Mutex
	file   *os.File
	buf    *bufio.Writer
	offset int64

	durability Durability
}

// OpenWAL opens (creating if needed) dir/wal.log and positions the append
// offset at the current end of file, so replaying then appending never
// overwrites existing records.
func OpenWAL(dir string, durability Durability) (*WAL, error) {
	const op = "wal.Open"
	path := filepath.Join(dir, WALFileName)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, kverrors.Wrap(kverrors.DataDirFailure, op, err,
			"could not open the WAL file for appending: check that the data directory exists and is writable")
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, kverrors.Wrap(kverrors.DataDirFailure, op, err,
			"could not stat the WAL file")
	}
	return &WAL{
		path:       path,
		file:       f,
		buf:        bufio.NewWriter(f),
		offset:     info.Size(),
		durability: durability,
	}, nil
}

// Path returns the WAL file path.
func (w *WAL) Path() string {
	return w.path
}

// Size returns the logical WAL size in bytes, including buffered data.
func (w *WAL) Size() int64 {
	w.wmu.Lock()
	defer w.wmu.Unlock()
	return w.offset
}

// Append serializes entry, appends it to the log, flushes the buffer, and
// fsyncs under DurabilityAlways, so a completed Append survives an abrupt
// process termination. On error the logical WAL position is unchanged.
func (w *WAL) Append(e Entry) error {
	const op = "wal.Append"

	frame, err := Encode(e)
	if err != nil {
		return err // already a typed kverrors error
	}

	w.wmu.Lock()
	defer w.wmu.Unlock()

	if w.file == nil {
		return kverrors.New(kverrors.StoreClosed, op, "the WAL is closed")
	}

	n, err := w.buf.Write(frame)
	if err != nil {
		w.offset -= int64(n) // only bytes accepted before the failure count
		return kverrors.Wrap(kverrors.DataDirFailure, op, err,
			"WAL append failed: the mutation was NOT made durable")
	}
	w.offset += int64(len(frame))

	if err := w.buf.Flush(); err != nil {
		return kverrors.Wrap(kverrors.DataDirFailure, op, err,
			"could not flush the WAL to disk: the mutation was NOT made durable")
	}
	if w.durability == DurabilityAlways {
		if err := w.file.Sync(); err != nil {
			return kverrors.Wrap(kverrors.DataDirFailure, op, err,
				"could not fsync the WAL: the mutation may not survive an abrupt termination")
		}
	}
	return nil
}

// Close flushes pending buffered data, fsyncs under DurabilityAlways, and
// closes the file. It is safe to call Close on an already-closed WAL.
func (w *WAL) Close() error {
	const op = "wal.Close"

	w.wmu.Lock()
	defer w.wmu.Unlock()

	if w.file == nil {
		return nil
	}
	if err := w.buf.Flush(); err != nil {
		_ = w.file.Close()
		w.file = nil
		return kverrors.Wrap(kverrors.DataDirFailure, op, err,
			"could not flush the WAL during close: pending mutations may be lost")
	}
	var closeErr error
	if w.durability == DurabilityAlways {
		if err := w.file.Sync(); err != nil {
			closeErr = kverrors.Wrap(kverrors.DataDirFailure, op, err,
				"could not fsync the WAL during close")
		}
	}
	if err := w.file.Close(); err != nil && closeErr == nil {
		closeErr = kverrors.Wrap(kverrors.DataDirFailure, op, err,
			"could not close the WAL file")
	}
	w.file = nil
	return closeErr
}

// Encode serializes an entry into a frame with a trailing CRC32 over all
// bytes after the magic. Keys must be non-empty and lengths bounded; delete
// entries must not carry a value.
func Encode(e Entry) ([]byte, error) {
	const op = "wal.Encode"

	if len(e.Key) == 0 {
		return nil, kverrors.New(kverrors.InvalidKey, op,
			"WAL entry key must not be empty")
	}
	if len(e.Key) > maxKeyOrValueSize {
		return nil, kverrors.New(kverrors.InvalidKey, op,
			"WAL entry key length %d exceeds %d bytes", len(e.Key), maxKeyOrValueSize)
	}
	if e.Op == OpDelete && len(e.Value) > 0 {
		return nil, kverrors.New(kverrors.InvalidValue, op,
			"delete entries must not carry a value")
	}
	if len(e.Value) > maxKeyOrValueSize {
		return nil, kverrors.New(kverrors.InvalidValue, op,
			"WAL entry value size %d exceeds %d bytes", len(e.Value), maxKeyOrValueSize)
	}

	var expNano int64
	if !e.ExpiresAt.IsZero() {
		expNano = e.ExpiresAt.UnixNano()
	}

	bodySize := len(e.Key) + len(e.Value)
	frame := make([]byte, frameHeaderSize+bodySize+crcSize)
	binary.LittleEndian.PutUint32(frame[0:4], WALMagic)
	frame[4] = WALVersion
	frame[5] = byte(e.Op)
	binary.LittleEndian.PutUint64(frame[6:14], uint64(len(e.Key)))
	binary.LittleEndian.PutUint64(frame[14:22], uint64(len(e.Value)))
	binary.LittleEndian.PutUint64(frame[22:30], uint64(expNano))
	copy(frame[30:30+len(e.Key)], e.Key)
	copy(frame[30+len(e.Key):30+bodySize], e.Value)

	crc := crc32.Checksum(frame[4:30+bodySize], crcTable)
	binary.LittleEndian.PutUint32(frame[30+bodySize:], crc)
	return frame, nil
}

// Decode parses one frame from r. startOffset is the byte offset of the
// frame's first byte and appears in every error message. It returns io.EOF
// only for a clean end of file at a frame boundary; every other anomaly —
// bad magic, unknown version or op, absurd lengths, truncation, checksum
// mismatch — is a WALCorruption error. Decode never rewrites the file.
func Decode(r io.Reader, startOffset int64) (Entry, int64, error) {
	const op = "wal.Decode"

	header := make([]byte, frameHeaderSize)
	n, err := io.ReadFull(r, header)
	if err != nil {
		if err == io.EOF && n == 0 {
			return Entry{}, startOffset, io.EOF // clean end of file
		}
		if err == io.EOF || err == io.ErrUnexpectedEOF {
			return Entry{}, startOffset, kverrors.New(kverrors.WALCorruption, op,
				"truncated WAL header at offset %d: read %d of %d bytes", startOffset, n, frameHeaderSize)
		}
		return Entry{}, startOffset, kverrors.Wrap(kverrors.WALCorruption, op, err,
			"could not read the WAL header at offset %d", startOffset)
	}

	if binary.LittleEndian.Uint32(header[0:4]) != WALMagic {
		return Entry{}, startOffset, kverrors.New(kverrors.WALCorruption, op,
			"bad WAL magic at offset %d: the file may not be a MiniKV WAL", startOffset)
	}
	if header[4] != WALVersion {
		return Entry{}, startOffset, kverrors.New(kverrors.WALCorruption, op,
			"unsupported WAL format version %d at offset %d", header[4], startOffset)
	}

	opType := OpType(header[5])
	if !opType.valid() {
		return Entry{}, startOffset, kverrors.New(kverrors.WALCorruption, op,
			"unknown operation type %d at offset %d", header[5], startOffset)
	}

	klen := binary.LittleEndian.Uint64(header[6:14])
	vlen := binary.LittleEndian.Uint64(header[14:22])
	expNano := int64(binary.LittleEndian.Uint64(header[22:30]))

	if klen == 0 || klen > maxKeyOrValueSize {
		return Entry{}, startOffset, kverrors.New(kverrors.WALCorruption, op,
			"invalid key length %d at offset %d", klen, startOffset)
	}
	if vlen > maxKeyOrValueSize {
		return Entry{}, startOffset, kverrors.New(kverrors.WALCorruption, op,
			"invalid value length %d at offset %d", vlen, startOffset)
	}

	bodySize := int(klen) + int(vlen)
	tail := make([]byte, bodySize+crcSize)
	if _, err := io.ReadFull(r, tail); err != nil {
		if err == io.EOF || err == io.ErrUnexpectedEOF {
			return Entry{}, startOffset, kverrors.New(kverrors.WALCorruption, op,
				"truncated WAL frame at offset %d: header promises %d bytes", startOffset, frameHeaderSize+bodySize+crcSize)
		}
		return Entry{}, startOffset, kverrors.Wrap(kverrors.WALCorruption, op, err,
			"could not read the WAL frame body at offset %d", startOffset)
	}

	// Verify the CRC over bytes [4, 30+K+V): version, op, lengths,
	// expiration, key, and value.
	crcInput := make([]byte, frameHeaderSize-4+bodySize)
	copy(crcInput, header[4:])
	copy(crcInput[frameHeaderSize-4:], tail[:bodySize])
	got := binary.LittleEndian.Uint32(tail[bodySize:])
	want := crc32.Checksum(crcInput, crcTable)
	if got != want {
		return Entry{}, startOffset, kverrors.New(kverrors.WALCorruption, op,
			"WAL checksum mismatch at offset %d: recorded %08x, computed %08x", startOffset, got, want)
	}

	e := Entry{Op: opType, Key: tail[:klen]}
	if vlen > 0 {
		e.Value = tail[klen:bodySize]
	}
	if expNano != 0 {
		e.ExpiresAt = time.Unix(0, expNano)
	}
	return e, startOffset + int64(frameHeaderSize+bodySize+crcSize), nil
}
