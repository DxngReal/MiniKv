package engine

import (
	"io"
	"log/slog"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"minikv/internal/kverrors"
)

// Sentinel values for Stats.SnapshotEntries when no snapshot exists.
const NoSnapshot = -1

// entry is a single stored value with an optional absolute expiration.
// A zero expiresAt means the entry never expires.
type entry struct {
	value     []byte
	expiresAt time.Time
}

// expired reports whether the entry's TTL has elapsed at time now.
func (e entry) expired(now time.Time) bool {
	return !e.expiresAt.IsZero() && !now.Before(e.expiresAt)
}

// shard is one lock domain of the store. Each shard owns exactly one mutex
// and one map; code must never hold two shard locks at the same time.
type shard struct {
	mu    sync.RWMutex
	items map[string]entry
}

// Store is the in-memory sharded key-value store implementing Engine.
// All methods are safe for concurrent use.
type Store struct {
	shards     []*shard
	shardMask  uint64
	maxKey     int
	maxValue   int
	janitorInt time.Duration
	logger     *slog.Logger

	// closed is checked before every operation; Close is idempotent.
	closed    atomic.Bool
	closeOnce sync.Once
	stopJan   chan struct{}
	janDone   chan struct{}

	openedAt time.Time

	// Global counters use atomics so no lock is shared across shards.
	puts    atomic.Int64
	gets    atomic.Int64
	hits    atomic.Int64
	deletes atomic.Int64
	expired atomic.Int64
}

// Compile-time proof that Store satisfies the engine contract.
var _ Engine = (*Store)(nil)

// Options configures a Store. Zero-value fields fall back to defaults.
type Options struct {
	// ShardCount is the number of lock shards; must be a power of two.
	// Default: 32.
	ShardCount int
	// MaxKeyBytes limits key length in bytes. Default: MaxKeyBytes (512).
	MaxKeyBytes int
	// MaxValueBytes limits value size in bytes. Default: MaxValueBytes (1 MiB).
	MaxValueBytes int
	// JanitorInterval is how often the background cleaner removes expired
	// entries. Default: 1s.
	JanitorInterval time.Duration
	// Logger receives janitor and lifecycle messages; nil discards logs.
	Logger *slog.Logger
}

// DefaultOptions returns the default store options.
func DefaultOptions() Options {
	return Options{
		ShardCount:      32,
		MaxKeyBytes:     MaxKeyBytes,
		MaxValueBytes:   MaxValueBytes,
		JanitorInterval: time.Second,
	}
}

// New creates a sharded in-memory store and starts its TTL janitor.
// Call Close to stop the janitor and reject further operations.
func New(opts Options) (*Store, error) {
	const op = "engine.New"

	if opts.ShardCount <= 0 {
		opts.ShardCount = 32
	}
	if opts.ShardCount&(opts.ShardCount-1) != 0 {
		return nil, kverrors.New(kverrors.ServerFailure, op,
			"shard count %d must be a power of two", opts.ShardCount)
	}
	if opts.MaxKeyBytes <= 0 {
		opts.MaxKeyBytes = MaxKeyBytes
	}
	if opts.MaxValueBytes <= 0 {
		opts.MaxValueBytes = MaxValueBytes
	}
	if opts.JanitorInterval <= 0 {
		opts.JanitorInterval = time.Second
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.New(slog.NewJSONHandler(io.Discard, nil))
	}

	s := &Store{
		shards:     make([]*shard, opts.ShardCount),
		shardMask:  uint64(opts.ShardCount - 1),
		maxKey:     opts.MaxKeyBytes,
		maxValue:   opts.MaxValueBytes,
		janitorInt: opts.JanitorInterval,
		logger:     logger,
		stopJan:    make(chan struct{}),
		janDone:    make(chan struct{}),
		openedAt:   time.Now(),
	}
	for i := range s.shards {
		s.shards[i] = &shard{items: make(map[string]entry)}
	}

	go s.runJanitor()
	return s, nil
}

// shardFor returns the shard owning key (FNV-1a, masked by the shard count).
func (s *Store) shardFor(key string) *shard {
	h := uint64(14695981039346656037)
	for i := 0; i < len(key); i++ {
		h ^= uint64(key[i])
		h *= 1099511628211
	}
	return s.shards[h&s.shardMask]
}

// errClosed builds the standard "store is closed" error for an operation.
// It unwraps to ErrClosed and carries the StoreClosed kind.
func errClosed(op string) error {
	return kverrors.Wrap(kverrors.StoreClosed, op, ErrClosed, "operation rejected: the store is closed")
}

// Get returns the value stored under key. Missing and expired keys both
// return an error of kind kverrors.KeyNotFound.
//
// The returned slice aliases internal storage; treat it as read-only.
// Set copies its input, so later mutations of the caller's buffer are safe.
func (s *Store) Get(key string) ([]byte, error) {
	const op = "engine.Get"
	if s.closed.Load() {
		return nil, errClosed(op)
	}
	if err := ValidateKey(key, s.maxKey); err != nil {
		return nil, err
	}
	s.gets.Add(1)

	now := time.Now()
	sh := s.shardFor(key)

	sh.mu.RLock()
	e, ok := sh.items[key]
	sh.mu.RUnlock()
	if !ok {
		return nil, kverrors.New(kverrors.KeyNotFound, op, "key does not exist")
	}
	if !e.expired(now) {
		s.hits.Add(1)
		return e.value, nil
	}

	// Lazy expiry: remove the dead entry so counters stay accurate. Take the
	// write lock and re-check; another goroutine may have removed it already.
	sh.mu.Lock()
	if e, ok := sh.items[key]; ok && e.expired(now) {
		delete(sh.items, key)
		s.expired.Add(1)
	}
	sh.mu.Unlock()
	return nil, kverrors.New(kverrors.KeyNotFound, op, "key expired")
}

// Set stores value under key with an optional absolute expiration
// (nil means no expiration) and reports whether a live entry existed.
//
// Set copies value, so the caller may reuse its buffer afterwards. An
// expiration time in the past is rejected as an invalid TTL; expired
// entries behave exactly like missing ones.
func (s *Store) Set(key string, value []byte, expiresAt *time.Time) (bool, error) {
	const op = "engine.Set"
	if s.closed.Load() {
		return false, errClosed(op)
	}
	if err := ValidateKey(key, s.maxKey); err != nil {
		return false, err
	}
	if err := ValidateValue(value, s.maxValue); err != nil {
		return false, err
	}
	if expiresAt != nil && !expiresAt.IsZero() && !expiresAt.After(time.Now()) {
		return false, kverrors.New(kverrors.InvalidTTL, op,
			"expiration time is in the past; expired entries behave as missing ones")
	}
	return s.applySet(key, value, expiresAt)
}

// applySet stores the entry without re-validating: it is the single write
// path shared by the public Set and the durable engine's post-append apply.
// A lapsed deadline is accepted here on purpose — the entry is then dead on
// arrival and behaves exactly like a missing one, which matches how WAL
// replay treats an expired record (skip). This is what keeps memory and log
// consistent when an fsync outlives a short TTL.
func (s *Store) applySet(key string, value []byte, expiresAt *time.Time) (bool, error) {
	exp := time.Time{}
	if expiresAt != nil && !expiresAt.IsZero() {
		exp = *expiresAt
	}

	v := make([]byte, len(value))
	copy(v, value)

	now := time.Now()
	sh := s.shardFor(key)

	sh.mu.Lock()
	defer sh.mu.Unlock()

	existing, ok := sh.items[key]
	existed := ok && !existing.expired(now)
	if ok && existing.expired(now) {
		s.expired.Add(1)
	}
	sh.items[key] = entry{value: v, expiresAt: exp}
	s.puts.Add(1)
	return existed, nil
}

// Delete removes key and reports whether a live entry was removed.
// An expired entry is garbage-collected and reported as not existing.
func (s *Store) Delete(key string) (bool, error) {
	const op = "engine.Delete"
	if s.closed.Load() {
		return false, errClosed(op)
	}
	if err := ValidateKey(key, s.maxKey); err != nil {
		return false, err
	}

	now := time.Now()
	sh := s.shardFor(key)

	sh.mu.Lock()
	defer sh.mu.Unlock()

	e, ok := sh.items[key]
	if !ok {
		return false, nil
	}
	delete(sh.items, key)
	if e.expired(now) {
		s.expired.Add(1)
		return false, nil
	}
	s.deletes.Add(1)
	return true, nil
}

// Keys returns every live (non-expired) key, sorted for deterministic
// output. It never returns expired keys.
func (s *Store) Keys() ([]string, error) {
	const op = "engine.Keys"
	if s.closed.Load() {
		return nil, errClosed(op)
	}

	now := time.Now()
	keys := make([]string, 0, 16)
	for _, sh := range s.shards {
		sh.mu.RLock()
		for k, e := range sh.items {
			if !e.expired(now) {
				keys = append(keys, k)
			}
		}
		sh.mu.RUnlock()
	}
	sort.Strings(keys)
	return keys, nil
}

// Snapshot returns a point-in-time deep copy of all live entries. The
// returned map is independent of the store: later mutations do not affect
// it, which is what the persistence layer requires to write a consistent
// snapshot file without holding shard locks.
func (s *Store) Snapshot() (map[string][]byte, error) {
	data, _, err := s.SnapshotWithExpiry()
	return data, err
}

// SnapshotWithExpiry returns a deep copy of all live entries plus their
// absolute expiration times. The two maps are independent of the store and
// safe for the persistence layer to write without holding shard locks.
func (s *Store) SnapshotWithExpiry() (map[string][]byte, map[string]time.Time, error) {
	const op = "engine.SnapshotWithExpiry"
	if s.closed.Load() {
		return nil, nil, errClosed(op)
	}

	now := time.Now()
	out := make(map[string][]byte, 16)
	expirations := make(map[string]time.Time)
	for _, sh := range s.shards {
		sh.mu.RLock()
		for k, e := range sh.items {
			if e.expired(now) {
				continue
			}
			v := make([]byte, len(e.value))
			copy(v, e.value)
			out[k] = v
			if !e.expiresAt.IsZero() {
				expirations[k] = e.expiresAt
			}
		}
		sh.mu.RUnlock()
	}
	return out, expirations, nil
}

// Stats returns current counters, the number of live keys, and the
// persistence fields. The store itself reports WALBytes and
// SnapshotEntries as "unknown" (NoSnapshot for the snapshot); the durable
// wrapper overwrites them with real values.
func (s *Store) Stats() Stats {
	now := time.Now()
	var live int64
	for _, sh := range s.shards {
		sh.mu.RLock()
		for _, e := range sh.items {
			if !e.expired(now) {
				live++
			}
		}
		sh.mu.RUnlock()
	}
	return Stats{
		KeyCount:        live,
		Puts:            s.puts.Load(),
		Gets:            s.gets.Load(),
		HitCount:        s.hits.Load(),
		DeleteCount:     s.deletes.Load(),
		ExpiredCount:    s.expired.Load(),
		ShardCount:      len(s.shards),
		Uptime:          time.Since(s.openedAt),
		WALBytes:        -1,
		SnapshotEntries: NoSnapshot,
	}
}

// Close stops the TTL janitor and rejects further operations. Close is
// idempotent; the first call wins and later calls return nil. Operations
// after Close return an error unwrapping to ErrClosed.
func (s *Store) Close() error {
	s.closeOnce.Do(func() {
		s.closed.Store(true)
		close(s.stopJan)
		<-s.janDone
	})
	return nil
}

// setFromRecovery applies a recovered entry without a WAL append (the
// record is already durable). Counters stay untouched: recovery does not
// count as user traffic.
func (s *Store) setFromRecovery(key string, value []byte, expiresAt *time.Time) error {
	exp := time.Time{}
	if expiresAt != nil {
		exp = *expiresAt
	}
	sh := s.shardFor(key)
	sh.mu.Lock()
	defer sh.mu.Unlock()
	sh.items[key] = entry{value: value, expiresAt: exp}
	return nil
}

// purge removes all expired entries from one shard and returns the count.
// The caller must not hold any other shard lock (single-lock rule).
func (s *Store) purge(sh *shard, now time.Time) int {
	sh.mu.Lock()
	var n int
	for k, e := range sh.items {
		if e.expired(now) {
			delete(sh.items, k)
			n++
		}
	}
	sh.mu.Unlock()
	return n
}
