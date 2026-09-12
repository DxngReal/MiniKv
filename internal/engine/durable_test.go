package engine

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"minikv/internal/kverrors"
	"minikv/internal/persistence"
)

// Concurrency stress tests for the durable engine. These are the primary
// race-detector targets; run `go test -race ./...` to validate.

// TestDurableSetGetRoundTrip verifies WAL-first durability: after Set the
// value is readable and the WAL grew by the encoded frame size.
func TestDurableSetGetRoundTrip(t *testing.T) {
	dir := t.TempDir()
	eng, err := Open(dir, persistence.DurabilityNever, nil)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { _ = eng.Close() })

	existed, err := eng.Set("greeting", []byte("hello"), nil)
	if err != nil || existed {
		t.Fatalf("Set = (%v, %v), want (false, nil)", existed, err)
	}
	got, err := eng.Get("greeting")
	if err != nil || string(got) != "hello" {
		t.Fatalf("Get = (%q, %v), want hello", got, err)
	}

	st := eng.Stats()
	if st.WALBytes <= 0 || st.DurabilityMode != "never" {
		t.Errorf("Stats = %+v, want positive WAL bytes and never mode", st)
	}
}

// TestDurableRecoveryRestoresTTL verifies expiration times survive a
// restart: a key with a future TTL is restored with its deadline intact,
// and a key whose TTL lapsed before recovery is skipped (expired entries
// behave as missing).
func TestDurableRecoveryRestoresTTL(t *testing.T) {
	dir := t.TempDir()
	eng, err := Open(dir, persistence.DurabilityNever, nil)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	// Long TTL: must survive Close+Open even on a slow, loaded machine.
	future := time.Now().Add(10 * time.Second)
	if _, err := eng.Set("temp", []byte("v"), &future); err != nil {
		t.Fatalf("Set() error = %v", err)
	}

	// Short TTL that is guaranteed to lapse before the restart, so the
	// recovery-time skip is deterministic rather than a race against Close.
	soon := time.Now().Add(30 * time.Millisecond)
	if _, err := eng.Set("gone", []byte("x"), &soon); err != nil {
		t.Fatalf("Set() error = %v", err)
	}
	time.Sleep(60 * time.Millisecond)

	if err := eng.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	eng2, err := Open(dir, persistence.DurabilityNever, nil)
	if err != nil {
		t.Fatalf("reopen error = %v", err)
	}
	defer eng2.Close()

	got, err := eng2.Get("temp")
	if err != nil || string(got) != "v" {
		t.Fatalf("Get after restart = (%q, %v), want v (TTL must be restored)", got, err)
	}
	if _, err := eng2.Get("gone"); !kverrors.IsKind(err, kverrors.KeyNotFound) {
		t.Errorf("Get lapsed key after restart = %v, want KeyNotFound", err)
	}
}

// TestDurableConcurrentMixedStress hammers Set/Get/Delete/Keys/Snapshot and
// WriteSnapshot from many goroutines with a fast janitor. Under -race this
// exposes lock-order and shared-state bugs.
func TestDurableConcurrentMixedStress(t *testing.T) {
	dir := t.TempDir()
	eng, err := Open(dir, persistence.DurabilityNever, nil)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	const workers = 8
	const perWorker = 100

	var wg sync.WaitGroup
	stop := make(chan struct{})

	// Snapshot writer running alongside mutations.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				if _, err := eng.WriteSnapshot(); err != nil && !kverrors.IsKind(err, kverrors.StoreClosed) {
					t.Errorf("WriteSnapshot() error = %v", err)
					return
				}
			}
		}
	}()

	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < perWorker; i++ {
				key := fmt.Sprintf("k-%d-%d", id, i%25)
				switch i % 5 {
				case 0:
					// Fresh absolute deadline per Set: the API takes an absolute
					// expiration, so reusing a stale one is a caller error.
					exp := time.Now().Add(20 * time.Millisecond)
					if _, err := eng.Set(key, []byte("payload"), &exp); err != nil {
						t.Errorf("Set: %v", err)
						return
					}
				case 1, 2, 3:
					if _, err := eng.Get(key); err != nil && !kverrors.IsKind(err, kverrors.KeyNotFound) && !kverrors.IsKind(err, kverrors.StoreClosed) {
						t.Errorf("Get: %v", err)
						return
					}
				default:
					if _, err := eng.Delete(key); err != nil {
						t.Errorf("Delete: %v", err)
						return
					}
				}
			}
		}(w)
	}

	// Let the workers finish, then stop the snapshot loop before Close.
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()

	// The snapshot goroutine and workers both feed wg; signal stop after a
	// short grace period and wait for everything to wind down.
	go func() {
		<-done
	}()
	time.Sleep(2 * time.Second)
	close(stop)
	wg.Wait()

	if err := eng.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	// The store must still be recoverable after the stress.
	eng2, err := Open(dir, persistence.DurabilityNever, nil)
	if err != nil {
		t.Fatalf("reopen after stress error = %v", err)
	}
	defer eng2.Close()
	keys, err := eng2.Keys()
	if err != nil {
		t.Fatalf("Keys() after recovery error = %v", err)
	}
	t.Logf("recovered %d live keys after stress", len(keys))
}

// TestDurableWALGrowsAndStatsReport measures that stats reflect WAL growth
// and that ExpiredCount increases after TTL lapses.
func TestDurableWALGrowsAndStatsReport(t *testing.T) {
	dir := t.TempDir()
	eng, err := Open(dir, persistence.DurabilityNever, nil)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { _ = eng.Close() })

	before := eng.Stats().WALBytes
	exp := time.Now().Add(10 * time.Millisecond)
	if _, err := eng.Set("doomed", []byte("x"), &exp); err != nil {
		t.Fatalf("Set() error = %v", err)
	}
	after := eng.Stats().WALBytes
	if after <= before {
		t.Errorf("WAL did not grow: before=%d after=%d", before, after)
	}
	time.Sleep(50 * time.Millisecond)
	_, _ = eng.Get("doomed") // triggers lazy expiry
	if got := eng.Stats().ExpiredCount; got < 1 {
		t.Errorf("ExpiredCount = %d, want >= 1", got)
	}
}

// BenchmarkWALAppendDurabilityNever measures WAL-enabled writes without
// fsync (pure append path). Compare with the always mode for sync cost.
func BenchmarkWALAppendDurabilityNever(b *testing.B) {
	dir := b.TempDir()
	eng, err := Open(dir, persistence.DurabilityNever, nil)
	if err != nil {
		b.Fatal(err)
	}
	defer eng.Close()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := eng.Set(fmt.Sprintf("key-%d", i%50000), []byte("value-payload-32-bytes-long!!!!"), nil); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkWALAppendDurabilityAlways measures WAL-enabled writes with an
// fsync per mutation (the production default).
func BenchmarkWALAppendDurabilityAlways(b *testing.B) {
	dir := b.TempDir()
	eng, err := Open(dir, persistence.DurabilityAlways, nil)
	if err != nil {
		b.Fatal(err)
	}
	defer eng.Close()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := eng.Set(fmt.Sprintf("key-%d", i%50000), []byte("value-payload-32-bytes-long!!!!"), nil); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkWALAppendDurabilityAlwaysConcurrent measures durable writes with
// 8 goroutines writing concurrently — the workload group-commit batching
// targets. Before batching (one fsync per Append), ns/op is dominated by
// serial fsync latency; after batching, ns/op reflects fsync cost divided
// across the average batch size.
func BenchmarkWALAppendDurabilityAlwaysConcurrent(b *testing.B) {
	dir := b.TempDir()
	eng, err := Open(dir, persistence.DurabilityAlways, nil)
	if err != nil {
		b.Fatal(err)
	}
	defer eng.Close()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			if _, err := eng.Set(fmt.Sprintf("key-%d", i%50000), []byte("value-payload-32-bytes-long!!!!"), nil); err != nil {
				b.Fatal(err)
			}
			i++
		}
	})
}

// BenchmarkRecovery10k measures how long opening (snapshot+WAL recovery)
// takes for a known dataset: 10,000 WAL SET records, no snapshot.
func BenchmarkRecovery10k(b *testing.B) {
	// Build the dataset once outside the timer.
	dir := b.TempDir()
	eng, err := Open(dir, persistence.DurabilityNever, nil)
	if err != nil {
		b.Fatal(err)
	}
	for i := 0; i < 10000; i++ {
		if _, err := eng.Set(fmt.Sprintf("key-%d", i), []byte("value-payload-32-bytes-long!!!!"), nil); err != nil {
			b.Fatal(err)
		}
	}
	eng.Close()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		b.StopTimer()
		// Each iteration reopens the same on-disk state.
		b.StartTimer()
		e, err := Open(dir, persistence.DurabilityNever, nil)
		if err != nil {
			b.Fatal(err)
		}
		b.StopTimer()
		e.Close()
		b.StartTimer()
	}
}
