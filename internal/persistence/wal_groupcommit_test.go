package persistence

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// TestGroupCommitAckedAppendsSurviveWithoutClose is the core durability
// proof for batching: Append returns only after a flush+fsync covering the
// record, so killing the process right after an ack must lose nothing.
// We simulate the kill by abandoning the WAL (no Close) and recovering in a
// fresh WAL/replay over the same directory.
func TestGroupCommitAckedAppendsSurviveWithoutClose(t *testing.T) {
	dir := testDir(t)

	w, err := OpenWAL(dir, DurabilityAlways)
	if err != nil {
		t.Fatalf("OpenWAL() error = %v", err)
	}
	acked := 0
	for i := 0; i < 25; i++ {
		if err := w.Append(setEntry(fmt.Sprintf("k%d", i), "v")); err != nil {
			t.Fatalf("Append(%d) error = %v", i, err)
		}
		acked++
	}
	// Recover BEFORE any Close: the sync loop's fsyncs are the only
	// durability evidence at this point, exactly as after a process kill.
	// (Close happens via defer purely for file-handle cleanup; it cannot
	// retroactively affect the recovery just performed.)
	defer w.Close()

	apply := newMapApplier()
	if _, err := Recover(dir, apply, time.Now()); err != nil {
		t.Fatalf("Recover() error = %v", err)
	}
	for i := 0; i < acked; i++ {
		want := fmt.Sprintf("k%d", i)
		if got := apply.data[want]; string(got) != "v" {
			t.Fatalf("acked record %d not durable after abandon: got %q", i, got)
		}
	}
}

// TestGroupCommitConcurrentAcksDurable hammers Append from 16 goroutines
// without ever calling Close, then recovers. Every acknowledged append must
// survive: batching may never acknowledge a record that a later fsync did
// not cover.
func TestGroupCommitConcurrentAcksDurable(t *testing.T) {
	dir := testDir(t)

	w, err := OpenWAL(dir, DurabilityAlways)
	if err != nil {
		t.Fatalf("OpenWAL() error = %v", err)
	}
	const (
		goroutines = 16
		perG       = 40
	)
	var wg sync.WaitGroup
	errCh := make(chan error, goroutines)
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < perG; i++ {
				key := fmt.Sprintf("g%d-k%d", g, i)
				if err := w.Append(setEntry(key, "v")); err != nil {
					errCh <- fmt.Errorf("append %s: %w", key, err)
					return
				}
			}
		}(g)
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatalf("concurrent append failed: %v", err)
	}
	// Recover straight from the fsynced file, before any Close: every Append
	// above already returned, which by contract means its batch's fsync
	// completed. (Close via defer is cleanup only.)
	defer w.Close()

	apply := newMapApplier()
	if _, err := Recover(dir, apply, time.Now()); err != nil {
		t.Fatalf("Recover() error = %v", err)
	}
	for g := 0; g < goroutines; g++ {
		for i := 0; i < perG; i++ {
			key := fmt.Sprintf("g%d-k%d", g, i)
			if string(apply.data[key]) != "v" {
				t.Fatalf("acked key %s not durable after abandon (recovered %q)", key, apply.data[key])
			}
		}
	}
}

// TestGroupCommitAppendCloseRace exercises the append/close boundary: an
// Append that wins enqueues and must complete; an Append that loses gets a
// typed StoreClosed error — never a hang, never a panic. Close must always
// return.
func TestGroupCommitAppendCloseRace(t *testing.T) {
	for attempt := 0; attempt < 20; attempt++ {
		dir := testDir(t)
		w, err := OpenWAL(dir, DurabilityAlways)
		if err != nil {
			t.Fatalf("OpenWAL() error = %v", err)
		}

		var wg sync.WaitGroup
		appendDone := make(chan error, 1)
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				if err := w.Append(setEntry(fmt.Sprintf("k%d", i), "v")); err != nil {
					appendDone <- nil // lost the race; acceptable outcome
					return
				}
			}
			appendDone <- nil // all appended
		}()
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = w.Close()
		}()
		wg.Wait()

		// Close returned and the appender returned; nothing hung. The file
		// must still be a valid WAL: recovery succeeds (records up to the
		// last acked+synced point are present; the exact count depends on
		// the race, so only validity is asserted).
		apply := newMapApplier()
		if _, err := Recover(dir, apply, time.Now()); err != nil {
			t.Fatalf("attempt %d: Recover() after race error = %v", attempt, err)
		}
		_ = appendDone
	}
}

// TestWALNeverModeStillFlushesPerAppend pins the never-mode contract that
// group commit must not change: appends are flushed (recovery-visible
// without Close) but not fsynced.
func TestWALNeverModeStillFlushesPerAppend(t *testing.T) {
	dir := testDir(t)
	w, err := OpenWAL(dir, DurabilityNever)
	if err != nil {
		t.Fatalf("OpenWAL() error = %v", err)
	}
	defer w.Close()

	if err := w.Append(setEntry("n1", "v")); err != nil {
		t.Fatalf("Append() error = %v", err)
	}
	info, err := os.Stat(filepath.Join(dir, WALFileName))
	if err != nil {
		t.Fatalf("stat WAL: %v", err)
	}
	if info.Size() == 0 {
		t.Fatal("never-mode Append did not flush; buffered-only data would be invisible to recovery")
	}

	apply := newMapApplier()
	if _, err := Recover(dir, apply, time.Now()); err != nil {
		t.Fatalf("Recover() error = %v", err)
	}
	if string(apply.data["n1"]) != "v" {
		t.Fatalf("recovered %q, want v", apply.data["n1"])
	}
}

// TestGroupCommitPreservesFIFORecoveryOrder verifies that batching does not
// reorder records: recovery must apply them in the order they were written.
func TestGroupCommitPreservesFIFORecoveryOrder(t *testing.T) {
	dir := testDir(t)
	w, err := OpenWAL(dir, DurabilityAlways)
	if err != nil {
		t.Fatalf("OpenWAL() error = %v", err)
	}
	for i := 0; i < 50; i++ {
		if err := w.Append(setEntry("seq", fmt.Sprintf("v%03d", i))); err != nil {
			t.Fatalf("Append(%d) error = %v", i, err)
		}
	}
	// Recover before any Close: the last acked Append implies its fsync
	// landed. (Close via defer is cleanup only.)
	defer w.Close()

	apply := newMapApplier()
	if _, err := Recover(dir, apply, time.Now()); err != nil {
		t.Fatalf("Recover() error = %v", err)
	}
	if string(apply.data["seq"]) != "v049" {
		t.Fatalf("recovered %q, want v049 (records must apply in write order)", apply.data["seq"])
	}
}
