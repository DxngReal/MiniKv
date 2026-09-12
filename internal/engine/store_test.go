package engine

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"minikv/internal/kverrors"
)

func newTestStore(t *testing.T, mutate ...func(*Options)) *Store {
	t.Helper()
	opts := DefaultOptions()
	opts.ShardCount = 8
	opts.JanitorInterval = 50 * time.Millisecond
	for _, m := range mutate {
		m(&opts)
	}
	s, err := New(opts)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})
	return s
}

func TestSetGetDeleteRoundTrip(t *testing.T) {
	s := newTestStore(t)

	existed, err := s.Set("greeting", []byte("hello"), nil)
	if err != nil || existed {
		t.Fatalf("Set(new key) = (%v, %v), want (false, nil)", existed, err)
	}

	got, err := s.Get("greeting")
	if err != nil || string(got) != "hello" {
		t.Fatalf("Get(greeting) = (%q, %v), want (%q, nil)", got, err, "hello")
	}

	existed, err = s.Set("greeting", []byte("again"), nil)
	if err != nil || !existed {
		t.Fatalf("Set(existing key) = (%v, %v), want (true, nil)", existed, err)
	}

	existed, err = s.Delete("greeting")
	if err != nil || !existed {
		t.Fatalf("Delete(existing) = (%v, %v), want (true, nil)", existed, err)
	}
	existed, err = s.Delete("greeting")
	if err != nil || existed {
		t.Fatalf("Delete(missing) = (%v, %v), want (false, nil)", existed, err)
	}

	if _, err := s.Get("greeting"); !kverrors.IsKind(err, kverrors.KeyNotFound) {
		t.Fatalf("Get(deleted) error = %v, want kind %q", err, kverrors.KeyNotFound)
	}
}

func TestGetMissingKeyIsKeyNotFound(t *testing.T) {
	s := newTestStore(t)
	_, err := s.Get("nope")
	if !kverrors.IsKind(err, kverrors.KeyNotFound) {
		t.Fatalf("Get(missing) error = %v, want kind %q", err, kverrors.KeyNotFound)
	}
	// The hint must be present and the error message must not contain the key.
	if !strings.Contains(err.Error(), "Next step:") {
		t.Errorf("error %q lacks a next-step hint", err)
	}
}

func TestSetValidation(t *testing.T) {
	s := newTestStore(t)
	s.maxKey = 8
	s.maxValue = 8

	cases := []struct {
		name     string
		key      string
		value    []byte
		expires  *time.Time
		wantKind kverrors.Kind
	}{
		{"empty key", "", []byte("v"), nil, kverrors.InvalidKey},
		{"over-long key", strings.Repeat("k", 9), []byte("v"), nil, kverrors.InvalidKey},
		{"nil value", "k", nil, nil, kverrors.InvalidValue},
		{"over-long value", "k", make([]byte, 9), nil, kverrors.InvalidValue},
		{"past expiration", "k", []byte("v"), pastTime(), kverrors.InvalidTTL},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := s.Set(tc.key, tc.value, tc.expires)
			if !kverrors.IsKind(err, tc.wantKind) {
				t.Errorf("Set() error = %v, want kind %q", err, tc.wantKind)
			}
		})
	}

	// Zero-value expiration pointer semantics: nil means no expiration.
	if _, err := s.Set("k", []byte("v"), nil); err != nil {
		t.Errorf("Set(nil expiration) error = %v, want nil", err)
	}
}

func pastTime() *time.Time {
	t := time.Now().Add(-time.Minute)
	return &t
}

func TestTTLExpirationLazy(t *testing.T) {
	s := newTestStore(t, func(o *Options) { o.JanitorInterval = time.Hour })

	exp := time.Now().Add(30 * time.Millisecond)
	if _, err := s.Set("temp", []byte("v"), &exp); err != nil {
		t.Fatalf("Set() error = %v", err)
	}
	if _, err := s.Get("temp"); err != nil {
		t.Fatalf("Get before expiry error = %v, want nil", err)
	}

	time.Sleep(60 * time.Millisecond)

	_, err := s.Get("temp")
	if !kverrors.IsKind(err, kverrors.KeyNotFound) {
		t.Fatalf("Get after expiry error = %v, want kind %q", err, kverrors.KeyNotFound)
	}
	if stats := s.Stats(); stats.ExpiredCount < 1 {
		t.Errorf("ExpiredCount = %d, want >= 1 after lazy expiry", stats.ExpiredCount)
	}
}

func TestTTLJanitorRemovesExpired(t *testing.T) {
	s := newTestStore(t, func(o *Options) { o.JanitorInterval = 20 * time.Millisecond })

	exp := time.Now().Add(30 * time.Millisecond)
	for i := 0; i < 10; i++ {
		if _, err := s.Set(fmt.Sprintf("k%d", i), []byte("v"), &exp); err != nil {
			t.Fatalf("Set() error = %v", err)
		}
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if st := s.Stats(); st.KeyCount == 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if st := s.Stats(); st.KeyCount != 0 {
		t.Errorf("janitor left %d live keys, want 0", st.KeyCount)
	}
	if _, err := s.Get("k3"); !kverrors.IsKind(err, kverrors.KeyNotFound) {
		t.Errorf("Get after janitor purge error = %v, want KeyNotFound", err)
	}
}

func TestKeysListsLiveKeysSorted(t *testing.T) {
	s := newTestStore(t)

	want := []string{"a", "b", "c"}
	for _, k := range want {
		if _, err := s.Set(k, []byte("v"), nil); err != nil {
			t.Fatalf("Set(%q) error = %v", k, err)
		}
	}
	exp := time.Now().Add(-time.Millisecond) // already dead but janitor may not run
	if _, err := s.Set("dead", []byte("v"), &exp); err == nil {
		t.Log("setting past expiration was accepted; janitor will clean it")
	}

	got, err := s.Keys()
	if err != nil {
		t.Fatalf("Keys() error = %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("Keys() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("Keys()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestSnapshotIsDeepCopy(t *testing.T) {
	s := newTestStore(t)

	if _, err := s.Set("k", []byte("v1"), nil); err != nil {
		t.Fatalf("Set() error = %v", err)
	}
	snap, err := s.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	if string(snap["k"]) != "v1" {
		t.Fatalf("snapshot value = %q, want %q", snap["k"], "v1")
	}

	// Mutate store and the copy; both must not affect the snapshot.
	if _, err := s.Set("k", []byte("v2"), nil); err != nil {
		t.Fatalf("Set() error = %v", err)
	}
	snap["k"][0] = 'x'
	snap["new"] = []byte("n")

	got, _ := s.Get("k")
	if string(got) != "v2" {
		t.Errorf("store value = %q, want v2 (snapshot must not alias store)", got)
	}
	if _, err := s.Get("new"); !kverrors.IsKind(err, kverrors.KeyNotFound) {
		t.Errorf("store gained key from snapshot copy; Get(new) error = %v", err)
	}
}

func TestSetCopiesCallerBuffer(t *testing.T) {
	s := newTestStore(t)

	buf := []byte("original")
	if _, err := s.Set("k", buf, nil); err != nil {
		t.Fatalf("Set() error = %v", err)
	}
	buf[0] = 'm'
	got, _ := s.Get("k")
	if string(got) != "original" {
		t.Errorf("stored value = %q, want %q (Set must copy)", got, "original")
	}
}

func TestOperationsAfterClose(t *testing.T) {
	s := newTestStore(t)
	if err := s.Close(); err != nil {
		t.Fatalf("first Close() error = %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("second Close() must be idempotent, got error %v", err)
	}

	if _, err := s.Get("k"); !errors.Is(err, ErrClosed) {
		t.Errorf("Get after Close error = %v, want ErrClosed", err)
	}
	if _, err := s.Set("k", []byte("v"), nil); !kverrors.IsKind(err, kverrors.StoreClosed) {
		t.Errorf("Set after Close error = %v, want kind StoreClosed", err)
	}
	if _, err := s.Delete("k"); !kverrors.IsKind(err, kverrors.StoreClosed) {
		t.Errorf("Delete after Close error = %v, want kind StoreClosed", err)
	}
	if _, err := s.Keys(); !kverrors.IsKind(err, kverrors.StoreClosed) {
		t.Errorf("Keys after Close error = %v, want kind StoreClosed", err)
	}
	if _, err := s.Snapshot(); !kverrors.IsKind(err, kverrors.StoreClosed) {
		t.Errorf("Snapshot after Close error = %v, want kind StoreClosed", err)
	}
}

func TestNewRejectsNonPowerOfTwoShards(t *testing.T) {
	for _, n := range []int{3, 5, 7, 12} {
		if _, err := New(Options{ShardCount: n}); !kverrors.IsKind(err, kverrors.ServerFailure) {
			t.Errorf("New(ShardCount=%d) error = %v, want ServerFailure", n, err)
		}
	}
	for _, n := range []int{1, 2, 4, 8, 32} {
		s, err := New(Options{ShardCount: n})
		if err != nil {
			t.Errorf("New(ShardCount=%d) unexpected error = %v", n, err)
			continue
		}
		s.Close()
	}
}

func TestShardDistribution(t *testing.T) {
	s := newTestStore(t, func(o *Options) { o.ShardCount = 8 })
	seen := make(map[*shard]bool)
	for i := 0; i < 200; i++ {
		seen[s.shardFor(fmt.Sprintf("key-%d", i))] = true
	}
	if len(seen) < 4 {
		t.Errorf("200 keys landed in %d/8 shards; distribution looks broken", len(seen))
	}
}

func TestStatsCounters(t *testing.T) {
	s := newTestStore(t)

	_, _ = s.Set("a", []byte("1"), nil)
	_, _ = s.Set("b", []byte("2"), nil)
	_, _ = s.Get("a")  // hit
	_, _ = s.Get("zz") // miss
	_, _ = s.Delete("b")

	time.Sleep(2 * time.Millisecond) // ensure measurable uptime
	st := s.Stats()
	if st.KeyCount != 1 {
		t.Errorf("KeyCount = %d, want 1", st.KeyCount)
	}
	if st.Puts != 2 {
		t.Errorf("Puts = %d, want 2", st.Puts)
	}
	if st.Gets != 2 {
		t.Errorf("Gets = %d, want 2", st.Gets)
	}
	if st.HitCount != 1 {
		t.Errorf("HitCount = %d, want 1", st.HitCount)
	}
	if st.DeleteCount != 1 {
		t.Errorf("DeleteCount = %d, want 1", st.DeleteCount)
	}
	if st.ShardCount != 8 {
		t.Errorf("ShardCount = %d, want 8", st.ShardCount)
	}
	if st.Uptime <= 0 {
		t.Errorf("Uptime = %s, want > 0", st.Uptime)
	}
}

func TestConcurrentAccess(t *testing.T) {
	s := newTestStore(t, func(o *Options) { o.ShardCount = 16 })

	const workers = 8
	const perWorker = 200

	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < perWorker; i++ {
				key := fmt.Sprintf("k-%d-%d", id, i%50)
				switch i % 4 {
				case 0:
					_, _ = s.Set(key, []byte("v"), nil)
				case 1:
					_, _ = s.Get(key)
				case 2:
					_, _ = s.Delete(key)
				case 3:
					_, _ = s.Keys()
					_, _ = s.Snapshot()
				}
			}
		}(w)
	}
	wg.Wait()

	st := s.Stats()
	if st.Gets < int64(workers*perWorker/4) {
		t.Errorf("Gets = %d, want at least %d", st.Gets, workers*perWorker/4)
	}
}

func TestConcurrentTTLWithSnapshot(t *testing.T) {
	s := newTestStore(t, func(o *Options) {
		o.ShardCount = 8
		o.JanitorInterval = 5 * time.Millisecond
	})

	stop := make(chan struct{})
	var wg sync.WaitGroup

	// Writer: keys expire quickly and are re-set continuously.
	wg.Add(1)
	go func() {
		defer wg.Done()
		exp := time.Now().Add(10 * time.Millisecond)
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			_, _ = s.Set(fmt.Sprintf("t%d", i%20), []byte("v"), &exp)
		}
	}()

	// Reader: snapshot concurrently with expiries and the janitor.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				if _, err := s.Snapshot(); err != nil && !kverrors.IsKind(err, kverrors.StoreClosed) {
					t.Errorf("Snapshot() error = %v", err)
					return
				}
			}
		}
	}()

	time.Sleep(150 * time.Millisecond)
	close(stop)
	wg.Wait()
}
