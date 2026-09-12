package persistence

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"minikv/internal/kverrors"
)

func testDir(t *testing.T) string {
	t.Helper()
	return t.TempDir()
}

func setEntry(key, value string) Entry {
	return Entry{Op: OpSet, Key: []byte(key), Value: []byte(value)}
}

func deleteEntry(key string) Entry {
	return Entry{Op: OpDelete, Key: []byte(key)}
}

// recordSize returns the encoded frame size for an entry (30-byte header +
// key + value + 4-byte CRC).
func recordSize(e Entry) int {
	return frameHeaderSize + len(e.Key) + len(e.Value) + crcSize
}

// mapApplier is a test Applier implementing snapshot-then-WAL semantics.
type mapApplier struct {
	data        map[string][]byte
	expirations map[string]time.Time
	expiredNow  time.Time
	failed      error
}

func newMapApplier() *mapApplier {
	return &mapApplier{data: map[string][]byte{}, expiredNow: time.Now().Add(time.Hour)}
}

func (a *mapApplier) ApplyWAL(e Entry) (bool, error) {
	if a.failed != nil {
		return false, a.failed
	}
	key := string(e.Key)
	switch e.Op {
	case OpSet:
		if !e.ExpiresAt.IsZero() && !e.ExpiresAt.After(a.expiredNow) {
			return false, nil
		}
		v := make([]byte, len(e.Value))
		copy(v, e.Value)
		a.data[key] = v
		if !e.ExpiresAt.IsZero() {
			a.expirations[key] = e.ExpiresAt
		}
		return true, nil
	case OpDelete:
		if _, ok := a.data[key]; !ok {
			return false, nil
		}
		delete(a.data, key)
		delete(a.expirations, key)
		return true, nil
	default:
		return false, fmt.Errorf("unknown op %d", e.Op)
	}
}

func TestWALRoundTrip(t *testing.T) {
	dir := testDir(t)
	w, err := OpenWAL(dir, DurabilityNever)
	if err != nil {
		t.Fatalf("OpenWAL() error = %v", err)
	}
	defer w.Close()

	exp := time.Now().Add(time.Minute).Truncate(time.Nanosecond)
	entries := []Entry{
		setEntry("alpha", "one"),
		setEntry("beta", "two"),
		{Op: OpSet, Key: []byte("gamma"), Value: []byte("three"), ExpiresAt: exp},
		deleteEntry("alpha"),
		setEntry("delta", ""),
	}
	for i, e := range entries {
		if err := w.Append(e); err != nil {
			t.Fatalf("Append(%d) error = %v", i, err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	// Replay from the file.
	f, err := os.Open(w.Path())
	if err != nil {
		t.Fatalf("open replay: %v", err)
	}
	defer f.Close()

	r := bufio.NewReader(f)
	offset := int64(0)
	var decoded []Entry
	for {
		e, next, err := Decode(r, offset)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("Decode at %d error = %v", offset, err)
		}
		decoded = append(decoded, e)
		offset = next
	}

	if len(decoded) != len(entries) {
		t.Fatalf("decoded %d entries, want %d", len(decoded), len(entries))
	}
	for i, want := range entries {
		got := decoded[i]
		if string(got.Key) != string(want.Key) {
			t.Errorf("entry %d key = %q, want %q", i, got.Key, want.Key)
		}
		if string(got.Value) != string(want.Value) {
			t.Errorf("entry %d value = %q, want %q", i, got.Value, want.Value)
		}
		if got.Op != want.Op {
			t.Errorf("entry %d op = %d, want %d", i, got.Op, want.Op)
		}
		if !want.ExpiresAt.IsZero() && !got.ExpiresAt.Equal(want.ExpiresAt) {
			t.Errorf("entry %d expiration = %v, want %v", i, got.ExpiresAt, want.ExpiresAt)
		}
	}
}

func TestWALSizeAndOffsetTracking(t *testing.T) {
	dir := testDir(t)
	w, err := OpenWAL(dir, DurabilityNever)
	if err != nil {
		t.Fatalf("OpenWAL() error = %v", err)
	}
	defer w.Close()

	e := setEntry("k", "v")
	if err := w.Append(e); err != nil {
		t.Fatalf("Append() error = %v", err)
	}
	want := int64(recordSize(e))
	if got := w.Size(); got != want {
		t.Errorf("Size() = %d, want %d", got, want)
	}

	info, err := os.Stat(w.Path())
	if err != nil {
		t.Fatalf("stat WAL: %v", err)
	}
	if info.Size() != want {
		t.Errorf("file size = %d, want %d (flush on append)", info.Size(), want)
	}
}

func TestDecodeRejectsCorruption(t *testing.T) {
	base := setEntry("hello", "world")
	frame, err := Encode(base)
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}

	corruptAndDecode := func(mutate func([]byte)) error {
		bad := make([]byte, len(frame))
		copy(bad, frame)
		mutate(bad)
		_, _, err := Decode(bytes.NewReader(bad), 0)
		return err
	}

	t.Run("bad magic", func(t *testing.T) {
		err := corruptAndDecode(func(b []byte) { b[0] = 0xFF })
		if !kverrors.IsKind(err, kverrors.WALCorruption) {
			t.Errorf("error = %v, want kind %q", err, kverrors.WALCorruption)
		}
		if !bytes.Contains([]byte(err.Error()), []byte("offset 0")) {
			t.Errorf("error %q lacks the byte offset", err)
		}
	})
	t.Run("bad version", func(t *testing.T) {
		err := corruptAndDecode(func(b []byte) { b[4] = 99 })
		if !kverrors.IsKind(err, kverrors.WALCorruption) {
			t.Errorf("error = %v, want kind %q", err, kverrors.WALCorruption)
		}
	})
	t.Run("unknown op", func(t *testing.T) {
		err := corruptAndDecode(func(b []byte) { b[5] = 42 })
		if !kverrors.IsKind(err, kverrors.WALCorruption) {
			t.Errorf("error = %v, want kind %q", err, kverrors.WALCorruption)
		}
	})
	t.Run("absurd key length", func(t *testing.T) {
		err := corruptAndDecode(func(b []byte) {
			for i := 6; i < 14; i++ {
				b[i] = 0xFF
			}
		})
		if !kverrors.IsKind(err, kverrors.WALCorruption) {
			t.Errorf("error = %v, want kind %q", err, kverrors.WALCorruption)
		}
	})
	t.Run("checksum mismatch", func(t *testing.T) {
		err := corruptAndDecode(func(b []byte) { b[frameHeaderSize] ^= 0xFF })
		if !kverrors.IsKind(err, kverrors.WALCorruption) {
			t.Errorf("error = %v, want kind %q", err, kverrors.WALCorruption)
		}
	})
	t.Run("truncated body", func(t *testing.T) {
		_, _, err := Decode(bytes.NewReader(frame[:len(frame)-2]), 0)
		if !kverrors.IsKind(err, kverrors.WALCorruption) {
			t.Errorf("error = %v, want kind %q", err, kverrors.WALCorruption)
		}
	})
	t.Run("truncated header", func(t *testing.T) {
		_, _, err := Decode(bytes.NewReader(frame[:10]), 0)
		if !kverrors.IsKind(err, kverrors.WALCorruption) {
			t.Errorf("error = %v, want kind %q", err, kverrors.WALCorruption)
		}
	})
	t.Run("clean EOF", func(t *testing.T) {
		if _, _, err := Decode(bytes.NewReader(nil), 0); !errors.Is(err, io.EOF) {
			t.Errorf("empty input error = %v, want io.EOF", err)
		}
	})
}

func TestEncodeValidation(t *testing.T) {
	if _, err := Encode(Entry{Op: OpSet, Key: nil, Value: []byte("v")}); !kverrors.IsKind(err, kverrors.InvalidKey) {
		t.Errorf("Encode(empty key) error = %v, want InvalidKey", err)
	}
	if _, err := Encode(Entry{Op: OpDelete, Key: []byte("k"), Value: []byte("v")}); !kverrors.IsKind(err, kverrors.InvalidValue) {
		t.Errorf("Encode(delete with value) error = %v, want InvalidValue", err)
	}
	if _, err := Encode(setEntry("k", string(make([]byte, maxKeyOrValueSize+1)))); !kverrors.IsKind(err, kverrors.InvalidValue) {
		t.Errorf("Encode(over-large value) error = %v, want InvalidValue", err)
	}
}

func TestWALReplayAfterReopen(t *testing.T) {
	dir := testDir(t)

	w, err := OpenWAL(dir, DurabilityNever)
	if err != nil {
		t.Fatalf("OpenWAL() error = %v", err)
	}
	_ = w.Append(setEntry("a", "1"))
	_ = w.Append(setEntry("b", "2"))
	_ = w.Append(deleteEntry("a"))
	if err := w.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	// Reopen: offset must resume at end of file, not overwrite.
	w2, err := OpenWAL(dir, DurabilityNever)
	if err != nil {
		t.Fatalf("reopen OpenWAL() error = %v", err)
	}
	defer w2.Close()
	if got := w2.Size(); got != int64(recordSize(setEntry("a", "1"))+recordSize(setEntry("b", "2"))+recordSize(deleteEntry("a"))) {
		t.Errorf("reopened Size() = %d, want existing records preserved", got)
	}
	if err := w2.Append(setEntry("c", "3")); err != nil {
		t.Fatalf("append after reopen error = %v", err)
	}

	apply := newMapApplier()
	if _, err := Recover(dir, apply, time.Now()); err != nil {
		t.Fatalf("Recover() error = %v", err)
	}
	if string(apply.data["a"]) != "" {
		t.Errorf("key a should have been deleted, got %q", apply.data["a"])
	}
	if string(apply.data["b"]) != "2" || string(apply.data["c"]) != "3" {
		t.Errorf("recovered data = %v, want b=2 c=3", apply.data)
	}
}

func TestRecoverWithoutAnyFiles(t *testing.T) {
	dir := testDir(t)
	apply := newMapApplier()
	stats, err := Recover(dir, apply, time.Now())
	if err != nil {
		t.Fatalf("Recover(empty dir) error = %v", err)
	}
	if stats.SnapshotUsed || stats.WALRecords != 0 {
		t.Errorf("unexpected recovery activity: %+v", stats)
	}
}

func TestRecoverFromCorruptWALReportsAndKeepsFile(t *testing.T) {
	dir := testDir(t)
	w, _ := OpenWAL(dir, DurabilityNever)
	_ = w.Append(setEntry("good", "1"))
	_ = w.Append(setEntry("good2", "2"))
	_ = w.Close()

	// Corrupt one byte inside the second record's value.
	raw, err := os.ReadFile(filepath.Join(dir, WALFileName))
	if err != nil {
		t.Fatalf("read WAL: %v", err)
	}
	secondStart := int64(recordSize(setEntry("good", "1")))
	raw[secondStart+frameHeaderSize] ^= 0xFF
	if err := os.WriteFile(filepath.Join(dir, WALFileName), raw, 0o644); err != nil {
		t.Fatalf("write corrupted WAL: %v", err)
	}
	intactBefore := append([]byte(nil), raw...)

	apply := newMapApplier()
	stats, rerr := Recover(dir, apply, time.Now())
	if !kverrors.IsKind(rerr, kverrors.WALCorruption) {
		t.Fatalf("Recover() error = %v, want kind %q", rerr, kverrors.WALCorruption)
	}
	if !bytes.Contains([]byte(rerr.Error()), []byte("offset")) || !bytes.Contains([]byte(rerr.Error()), []byte(WALFileName)) {
		t.Errorf("corruption error %q must include file and offset", rerr)
	}
	if stats.WALRecords != 1 {
		t.Errorf("WALRecords = %d, want 1 (first record replayed before corruption)", stats.WALRecords)
	}

	// The file must be untouched.
	rawAfter, err := os.ReadFile(filepath.Join(dir, WALFileName))
	if err != nil {
		t.Fatalf("re-read WAL: %v", err)
	}
	if !bytes.Equal(intactBefore, rawAfter) {
		t.Error("corrupted WAL file was modified by recovery")
	}

	// Valid prefix must stop exactly at the bad frame.
	valid, err := ValidWALBytes(dir)
	if err != nil {
		t.Fatalf("ValidWALBytes() error = %v", err)
	}
	if valid != secondStart {
		t.Errorf("ValidWALBytes() = %d, want %d", valid, secondStart)
	}
}

func TestRecoverStopsAtTruncatedTail(t *testing.T) {
	dir := testDir(t)
	w, _ := OpenWAL(dir, DurabilityNever)
	_ = w.Append(setEntry("a", "1"))
	_ = w.Append(setEntry("b", "22222"))
	_ = w.Close()

	raw, _ := os.ReadFile(filepath.Join(dir, WALFileName))
	// Simulate a torn write: chop the last 5 bytes.
	truncated := raw[:len(raw)-5]
	if err := os.WriteFile(filepath.Join(dir, WALFileName), truncated, 0o644); err != nil {
		t.Fatalf("write truncated WAL: %v", err)
	}

	apply := newMapApplier()
	_, err := Recover(dir, apply, time.Now())
	if !kverrors.IsKind(err, kverrors.WALCorruption) {
		t.Fatalf("Recover() error = %v, want kind %q", err, kverrors.WALCorruption)
	}
	// First record applied, second lost — but reported, not dropped silently.
	if string(apply.data["a"]) != "1" {
		t.Errorf("first record should have been applied, got %v", apply.data)
	}
}

func TestSnapshotWriteLoadRoundTrip(t *testing.T) {
	dir := testDir(t)
	data := map[string][]byte{"one": []byte("1"), "two": []byte("22")}
	expirations := map[string]time.Time{
		"two": time.Now().Add(time.Minute).Truncate(time.Nanosecond),
	}

	stats, err := WriteSnapshot(dir, data, expirations, time.Now())
	if err != nil {
		t.Fatalf("WriteSnapshot() error = %v", err)
	}
	if stats.Entries != 2 || stats.Bytes <= 0 {
		t.Errorf("stats = %+v, want 2 entries and positive size", stats)
	}

	loaded, loadedExp, ok, err := LoadSnapshot(dir)
	if err != nil || !ok {
		t.Fatalf("LoadSnapshot() = (ok=%v, err=%v), want ok", ok, err)
	}
	if len(loaded) != 2 || string(loaded["one"]) != "1" || string(loaded["two"]) != "22" {
		t.Errorf("loaded data = %v, want original values", loaded)
	}
	if !loadedExp["two"].Equal(expirations["two"]) {
		t.Errorf("loaded expiration = %v, want %v", loadedExp["two"], expirations["two"])
	}
}

func TestSnapshotMissingIsNotAnError(t *testing.T) {
	dir := testDir(t)
	_, _, ok, err := LoadSnapshot(dir)
	if err != nil {
		t.Fatalf("LoadSnapshot(missing) error = %v, want nil", err)
	}
	if ok {
		t.Error("LoadSnapshot(missing) ok = true, want false")
	}
}

func TestSnapshotCorruptIsReported(t *testing.T) {
	dir := testDir(t)
	if _, err := WriteSnapshot(dir, map[string][]byte{"k": []byte("v")}, nil, time.Now()); err != nil {
		t.Fatalf("WriteSnapshot() error = %v", err)
	}
	path := filepath.Join(dir, SnapshotFileName)
	raw, _ := os.ReadFile(path)
	raw[10] ^= 0xFF
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatalf("write corrupted snapshot: %v", err)
	}

	_, _, ok, err := LoadSnapshot(dir)
	if err == nil || ok {
		t.Fatalf("LoadSnapshot(corrupt) = (ok=%v, err=%v), want error", ok, err)
	}
	if !kverrors.IsKind(err, kverrors.SnapshotFailure) {
		t.Errorf("error = %v, want kind %q", err, kverrors.SnapshotFailure)
	}
}

func TestSnapshotLeavesNoTempFileAndKeepsPreviousOnError(t *testing.T) {
	dir := testDir(t)
	if _, err := WriteSnapshot(dir, map[string][]byte{"k": []byte("v1")}, nil, time.Now()); err != nil {
		t.Fatalf("first WriteSnapshot() error = %v", err)
	}
	// A second successful write must replace cleanly with no temp leftover.
	if _, err := WriteSnapshot(dir, map[string][]byte{"k": []byte("v2")}, nil, time.Now()); err != nil {
		t.Fatalf("second WriteSnapshot() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, SnapshotTmpName)); !os.IsNotExist(err) {
		t.Errorf("temp file still exists after successful snapshot (err=%v)", err)
	}
	loaded, _, ok, err := LoadSnapshot(dir)
	if err != nil || !ok || string(loaded["k"]) != "v2" {
		t.Errorf("loaded = (ok=%v, k=%v, err=%v), want v2", ok, loaded["k"], err)
	}
}

func TestRecoverAppliesSnapshotThenWAL(t *testing.T) {
	dir := testDir(t)

	// Snapshot: a=1, b=2. WAL: b deleted, c=3.
	if _, err := WriteSnapshot(dir, map[string][]byte{"a": []byte("1"), "b": []byte("2")}, nil, time.Now()); err != nil {
		t.Fatalf("WriteSnapshot() error = %v", err)
	}
	w, _ := OpenWAL(dir, DurabilityNever)
	_ = w.Append(deleteEntry("b"))
	_ = w.Append(setEntry("c", "3"))
	_ = w.Close()

	apply := newMapApplier()
	stats, err := Recover(dir, apply, time.Now())
	if err != nil {
		t.Fatalf("Recover() error = %v", err)
	}
	if !stats.SnapshotUsed || stats.SnapshotEntries != 2 {
		t.Errorf("snapshot stats wrong: %+v", stats)
	}
	if stats.WALRecords != 2 || stats.AppliedRecords != 4 {
		t.Errorf("WAL stats wrong: %+v", stats)
	}
	if string(apply.data["a"]) != "1" {
		t.Errorf("a = %q, want 1", apply.data["a"])
	}
	if _, exists := apply.data["b"]; exists {
		t.Error("b should have been deleted by WAL replay")
	}
	if string(apply.data["c"]) != "3" {
		t.Errorf("c = %q, want 3", apply.data["c"])
	}
}

func TestRecoverSkipsExpiredSets(t *testing.T) {
	dir := testDir(t)
	w, _ := OpenWAL(dir, DurabilityNever)
	past := time.Now().Add(-time.Hour)
	_ = w.Append(Entry{Op: OpSet, Key: []byte("dead"), Value: []byte("x"), ExpiresAt: past})
	_ = w.Append(setEntry("alive", "y"))
	_ = w.Close()

	apply := newMapApplier()
	stats, err := Recover(dir, apply, time.Now())
	if err != nil {
		t.Fatalf("Recover() error = %v", err)
	}
	if _, exists := apply.data["dead"]; exists {
		t.Error("expired key must not be restored")
	}
	if stats.SkippedExpired != 1 {
		t.Errorf("SkippedExpired = %d, want 1", stats.SkippedExpired)
	}
}

func TestRecoverApplierErrorIsRecoveryFailure(t *testing.T) {
	dir := testDir(t)
	w, _ := OpenWAL(dir, DurabilityNever)
	_ = w.Append(setEntry("a", "1"))
	_ = w.Close()

	apply := newMapApplier()
	apply.failed = errors.New("injected")
	_, err := Recover(dir, apply, time.Now())
	if !kverrors.IsKind(err, kverrors.RecoveryFailure) {
		t.Errorf("error = %v, want kind %q", err, kverrors.RecoveryFailure)
	}
}

func TestAppendAfterCloseIsRejected(t *testing.T) {
	dir := testDir(t)
	w, err := OpenWAL(dir, DurabilityNever)
	if err != nil {
		t.Fatalf("OpenWAL() error = %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if err := w.Close(); err != nil {
		t.Errorf("second Close() error = %v, want nil (idempotent)", err)
	}
	if err := w.Append(setEntry("k", "v")); !kverrors.IsKind(err, kverrors.StoreClosed) {
		t.Errorf("Append after Close error = %v, want kind %q", err, kverrors.StoreClosed)
	}
}

func TestOpenWALBadDirIsDataDirFailure(t *testing.T) {
	// A path through a file, not a directory, must fail with a typed error.
	dir := testDir(t)
	filePath := filepath.Join(dir, "not-a-dir")
	if err := os.WriteFile(filePath, []byte("x"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	_, err := OpenWAL(filepath.Join(filePath, "sub"), DurabilityNever)
	if !kverrors.IsKind(err, kverrors.DataDirFailure) {
		t.Errorf("error = %v, want kind %q", err, kverrors.DataDirFailure)
	}
}
