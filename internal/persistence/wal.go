package persistence

import (
	"bufio"
	"encoding/binary"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
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

// WAL is an append-only, checksummed write-ahead log.
//
// Durability guarantee (unchanged by batching): a completed Append means the
// record's bytes have been fsynced and will survive an abrupt process
// termination. Under DurabilityAlways, concurrent appends share fsyncs
// (group commit, leader/follower — see the group-commit state comment
// below): each Append writes its frame under wmu, joins the pending batch
// under mu, releases wmu, and waits until a flush+fsync covering its bytes
// completes; the first waiting writer performs that sync itself. N
// concurrent writes therefore pay a handful of syncs instead of N, in FIFO
// write order, and every waiter is signaled exactly once with the sync
// result.
//
// Lock ordering: wmu → mu. flushAndSync takes wmu alone; batch bookkeeping
// takes mu alone. No path acquires wmu while holding mu.
type WAL struct {
	path string

	// wmu serializes buffered writes and guards file/buf/offset. It is never
	// held across an fsync: that would serialize concurrent appends behind
	// each other's syncs instead of batching them.
	wmu    sync.Mutex
	file   *os.File
	buf    *bufio.Writer
	offset int64

	durability Durability

	// Group-commit state (active only under DurabilityAlways).
	//
	// Leader/follower scheme: the first Append to arrive with no sync in
	// flight becomes the leader — it takes the whole pending queue as its
	// batch, performs the flush+fsync itself (no extra goroutine hop, so the
	// single-writer path costs the same as an inline fsync), signals the
	// batch, and keeps draining whatever queued while it synced. Appends
	// arriving during a sync are followers: they enqueue and wait for a
	// later round, so N concurrent writers share a handful of fsyncs.
	//
	// mu guards pending and syncing. It is never held during I/O. Lock
	// ordering: wmu → mu (Append holds wmu while enqueueing under mu; no
	// path acquires wmu while holding mu).
	mu       sync.Mutex      // guards pending and syncing; never held during I/O
	pending  []*appendWaiter // FIFO queue of appends awaiting the next fsync
	syncing  bool            // true while a leader is flushing/fsyncing
	leaderWG sync.WaitGroup  // tracks the active leader (zero or one)

	// closing gates new appends. Set under mu; also read under wmu+mu in
	// Append and under mu in the leader loop.
	closing atomic.Bool
}

// appendWaiter is one Append call waiting for its record to be fsynced.
type appendWaiter struct {
	done chan struct{}
	err  error
}

// walMaxSyncBatch caps how many queued appends share one fsync. It bounds
// the extra latency an append can incur waiting for later arrivals: with
// more than 512 queued writers, the oldest 512 go first and the rest wait
// for the next round. Recovery semantics are unaffected — the file is just
// an append log.
const walMaxSyncBatch = 512

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
	wal := &WAL{
		path:       path,
		file:       f,
		buf:        bufio.NewWriter(f),
		offset:     info.Size(),
		durability: durability,
	}
	return wal, nil
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

// Append serializes entry, appends it to the log, and — under
// DurabilityAlways — waits until a shared flush+fsync has covered its
// bytes, so a completed Append survives an abrupt process termination.
// Concurrent appends share one fsync (group commit) in FIFO write order.
// On error the logical WAL position is unchanged.
func (w *WAL) Append(e Entry) error {
	const op = "wal.Append"

	frame, err := Encode(e)
	if err != nil {
		return err // already a typed kverrors error
	}

	w.wmu.Lock()
	if w.file == nil || w.closing.Load() {
		w.wmu.Unlock()
		return kverrors.New(kverrors.StoreClosed, op, "the WAL is closed")
	}

	n, err := w.buf.Write(frame)
	if err != nil {
		w.offset -= int64(n) // only bytes accepted before the failure count
		w.wmu.Unlock()
		return kverrors.Wrap(kverrors.DataDirFailure, op, err,
			"WAL append failed: the mutation was NOT made durable")
	}
	w.offset += int64(len(frame))

	if w.durability != DurabilityAlways {
		// Never mode: flush per append (buffered-only data was never visible
		// to recovery before Close); no fsync. The flush stays under wmu so
		// Close cannot interleave.
		if err := w.buf.Flush(); err != nil {
			w.wmu.Unlock()
			return kverrors.Wrap(kverrors.DataDirFailure, op, err,
				"could not flush the WAL to disk: the mutation was NOT made durable")
		}
		w.wmu.Unlock()
		return nil
	}

	// DurabilityAlways: leave the frame buffered — the covering fsync reads
	// everything written so far when it flushes — then join the pending
	// batch under mu while still holding wmu (lock order wmu → mu). The
	// first arrival with no sync in flight becomes the leader and performs
	// the batch's flush+fsync itself, exactly like the pre-batching inline
	// path; later arrivals during a sync wait for a shared round.
	w.mu.Lock()
	if w.closing.Load() {
		// Lost the close race after the frame was buffered. The frame may or
		// may not reach the log via Close's final flush; report failure so
		// the caller does not apply it in memory (recovery on the next Open
		// decides what survives).
		w.mu.Unlock()
		w.wmu.Unlock()
		return kverrors.New(kverrors.StoreClosed, op, "the WAL is closed")
	}
	waiter := &appendWaiter{done: make(chan struct{})}
	w.pending = append(w.pending, waiter)
	var batch []*appendWaiter
	if !w.syncing {
		w.syncing = true
		w.leaderWG.Add(1)
		batch = w.takeBatchLocked()
	}
	w.mu.Unlock()
	w.wmu.Unlock()

	if batch != nil {
		w.leadSync(batch)
	}

	<-waiter.done
	return waiter.err
}

// takeBatchLocked removes up to walMaxSyncBatch waiters from the pending
// queue and returns them in FIFO order. Called with mu held; the remainder
// is copied out so consumed waiters are not pinned in the backing array.
func (w *WAL) takeBatchLocked() []*appendWaiter {
	n := len(w.pending)
	if n > walMaxSyncBatch {
		n = walMaxSyncBatch
	}
	batch := make([]*appendWaiter, n)
	copy(batch, w.pending[:n])
	w.pending = append([]*appendWaiter(nil), w.pending[n:]...)
	return batch
}

// leadSync performs flush+fsync rounds on behalf of the batch's waiters and
// every append that queued while those rounds ran, then releases the leader
// role. I/O happens with neither wmu nor mu held. Every waiter it takes is
// signaled exactly once with the round's result.
func (w *WAL) leadSync(batch []*appendWaiter) {
	defer w.leaderWG.Done()

	for {
		err := w.flushAndSync()
		for _, waiter := range batch {
			waiter.err = err
			close(waiter.done)
		}

		w.mu.Lock()
		if len(w.pending) == 0 {
			w.syncing = false
			w.mu.Unlock()
			return
		}
		// Appends queued while we synced share the next round.
		batch = w.takeBatchLocked()
		w.mu.Unlock()
	}
}

// flushAndSync flushes the buffered frames and fsyncs the file. Called by
// the sync loop only; takes wmu alone, never mu.
func (w *WAL) flushAndSync() error {
	const op = "wal.flushAndSync"

	w.wmu.Lock()
	if w.file == nil {
		// Close already flushed and synced the file before tearing it down;
		// this batch's bytes were included.
		w.wmu.Unlock()
		return nil
	}
	flushErr := w.buf.Flush()
	if flushErr != nil {
		w.wmu.Unlock()
		return kverrors.Wrap(kverrors.DataDirFailure, op, flushErr,
			"could not flush the WAL to disk: the batched mutations were NOT made durable")
	}
	syncErr := w.file.Sync()
	w.wmu.Unlock()
	if syncErr != nil {
		return kverrors.Wrap(kverrors.DataDirFailure, op, syncErr,
			"could not fsync the WAL: the batched mutations may not survive an abrupt termination")
	}
	return nil
}

// Close stops new appends, waits for the group-commit sync loop to drain
// every pending batch, then flushes, fsyncs (DurabilityAlways), and closes
// the file. It is safe to call Close on an already-closed WAL.
func (w *WAL) Close() error {
	const op = "wal.Close"

	// Stop new appends, then wait for the active leader to drain every
	// queued batch. New leaders cannot emerge after closing is set (the
	// enqueue path aborts under the same mutex), so when this Wait returns
	// no sync is in flight and pending is empty.
	w.mu.Lock()
	w.closing.Store(true)
	w.mu.Unlock()
	w.leaderWG.Wait()

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
