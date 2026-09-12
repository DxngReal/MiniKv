package engine

import (
	"strconv"
	"sync"
	"testing"
	"time"
)

// Benchmarks for the in-memory store (no WAL). Run with:
//
//	go test -bench=. -benchmem ./internal/engine/
//
// Results and machine context are documented in docs/benchmarks.md.

func benchStore(b *testing.B) *Store {
	b.Helper()
	s, err := New(Options{ShardCount: 32, JanitorInterval: time.Second})
	if err != nil {
		b.Fatalf("New() error = %v", err)
	}
	b.Cleanup(func() { _ = s.Close() })
	return s
}

// BenchmarkSetNoTTL measures single-goroutine Set without expiration.
func BenchmarkSetNoTTL(b *testing.B) {
	s := benchStore(b)
	keys := make([]string, 10000)
	for i := range keys {
		keys[i] = "key-" + strconv.Itoa(i)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := s.Set(keys[i%len(keys)], []byte("value-payload-32-bytes-long!!!!"), nil); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkGetHit measures single-goroutine Get on existing keys.
func BenchmarkGetHit(b *testing.B) {
	s := benchStore(b)
	keys := make([]string, 10000)
	for i := range keys {
		keys[i] = "key-" + strconv.Itoa(i)
		if _, err := s.Set(keys[i], []byte("value-payload-32-bytes-long!!!!"), nil); err != nil {
			b.Fatal(err)
		}
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := s.Get(keys[i%len(keys)]); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkGetMiss measures Get on absent keys (validation + lookup only).
func BenchmarkGetMiss(b *testing.B) {
	s := benchStore(b)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := s.Get("missing-key"); err == nil {
			b.Fatal("expected miss")
		}
	}
}

// BenchmarkDelete measures single-goroutine Delete of pre-seeded keys.
func BenchmarkDelete(b *testing.B) {
	s := benchStore(b)
	for i := 0; i < b.N; i++ {
		if _, err := s.Set("key-"+strconv.Itoa(i), []byte("v"), nil); err != nil {
			b.Fatal(err)
		}
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := s.Delete("key-" + strconv.Itoa(i)); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkMixedConcurrent measures 8 goroutines doing 50% Get (90% hit),
// 30% Set, 20% Delete over a shared keyspace.
func BenchmarkMixedConcurrent(b *testing.B) {
	s := benchStore(b)
	for i := 0; i < 5000; i++ {
		if _, err := s.Set("key-"+strconv.Itoa(i), []byte("v"), nil); err != nil {
			b.Fatal(err)
		}
	}
	b.ResetTimer()

	var wg sync.WaitGroup
	for w := 0; w < 8; w++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < b.N/8; i++ {
				key := "key-" + strconv.Itoa((id*7919+i)%5000)
				switch i % 10 {
				case 0, 1:
					_, _ = s.Set(key, []byte("v"), nil)
				case 2:
					_, _ = s.Delete(key)
				default:
					_, _ = s.Get(key)
				}
			}
		}(w)
	}
	wg.Wait()
}

// BenchmarkSetWithTTL measures Set with an expiration on every write.
func BenchmarkSetWithTTL(b *testing.B) {
	s := benchStore(b)
	exp := time.Now().Add(time.Hour)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := s.Set("key-"+strconv.Itoa(i%10000), []byte("v"), &exp); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkSnapshot measures a full deep-copy Snapshot of 10k entries.
func BenchmarkSnapshot(b *testing.B) {
	s := benchStore(b)
	for i := 0; i < 10000; i++ {
		if _, err := s.Set("key-"+strconv.Itoa(i), []byte("value-payload-32-bytes-long!!!!"), nil); err != nil {
			b.Fatal(err)
		}
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := s.Snapshot(); err != nil {
			b.Fatal(err)
		}
	}
}
