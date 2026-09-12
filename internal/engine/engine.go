// Package engine defines the storage contract for MiniKV: the Engine
// interface implemented by the key-value store and consumed by the HTTP and
// CLI layers, the size limits, and the statistics snapshot.
//
// Values are stored as bytes internally. HTTP and CLI layers may expose
// UTF-8 strings. Implementations must be safe for concurrent use and must
// treat expired entries exactly like missing entries.
package engine

import (
	"time"

	"minikv/internal/kverrors"
)

// Size limits applied when a caller does not override them.
const (
	// MaxKeyBytes is the maximum key length in bytes.
	MaxKeyBytes = 512
	// MaxValueBytes is the maximum value size in bytes (1 MiB).
	MaxValueBytes = 1 << 20
)

// ErrClosed is returned by operations after Close. It wraps
// kverrors.StoreClosed so callers can match on either.
var ErrClosed = kverrors.New(kverrors.StoreClosed, "engine", "the store is closed")

// Engine is the storage contract consumed by the HTTP and CLI layers.
// All methods must be safe for concurrent use.
type Engine interface {
	// Get returns the value stored under key. Expired or missing keys return
	// an error of kind kverrors.KeyNotFound.
	Get(key string) ([]byte, error)
	// Set stores value under key with an optional absolute expiration time
	// (nil means no expiration) and reports whether the key already existed.
	Set(key string, value []byte, expiresAt *time.Time) (existed bool, err error)
	// Delete removes key and reports whether it existed.
	Delete(key string) (existed bool, err error)
	// Keys returns every live (non-expired) key.
	Keys() ([]string, error)
	// Snapshot returns a point-in-time copy of all live entries.
	Snapshot() (map[string][]byte, error)
	// Stats returns counters for the status endpoint.
	Stats() Stats
	// Close releases resources; subsequent operations return ErrClosed.
	Close() error
}

// Stats is the statistics snapshot exposed by /v1/status.
type Stats struct {
	// KeyCount is the number of live (non-expired) keys.
	KeyCount int64
	// Puts counts successful Set operations.
	Puts int64
	// Gets counts Get calls, successful or not.
	Gets int64
	// HitCount counts Get calls that found a live value.
	HitCount int64
	// DeleteCount counts Delete calls that removed a key.
	DeleteCount int64
	// ExpiredCount counts entries dropped because their TTL elapsed.
	ExpiredCount int64
	// ShardCount is the number of lock shards backing the store.
	ShardCount int
	// Uptime is how long the store has been open.
	Uptime time.Duration
}

// HitRate returns the fraction of Get calls that hit a live value.
func (s Stats) HitRate() float64 {
	if s.Gets == 0 {
		return 0
	}
	return float64(s.HitCount) / float64(s.Gets)
}

// ValidateKey reports whether key is usable, with a kind explaining why not.
func ValidateKey(key string, maxBytes int) error {
	if key == "" {
		return kverrors.New(kverrors.InvalidKey, "engine.ValidateKey", "key must not be empty")
	}
	if len(key) > maxBytes {
		return kverrors.New(kverrors.InvalidKey, "engine.ValidateKey",
			"key length %d exceeds the maximum of %d bytes", len(key), maxBytes)
	}
	return nil
}

// ValidateValue reports whether value is usable.
func ValidateValue(value []byte, maxBytes int) error {
	if value == nil {
		return kverrors.New(kverrors.InvalidValue, "engine.ValidateValue", "value must not be nil")
	}
	if len(value) > maxBytes {
		return kverrors.New(kverrors.InvalidValue, "engine.ValidateValue",
			"value size %d exceeds the maximum of %d bytes", len(value), maxBytes)
	}
	return nil
}

// ValidateTTL reports whether a TTL is usable. Zero or positive values are
// accepted; negative values are rejected.
func ValidateTTL(ttl time.Duration) error {
	if ttl < 0 {
		return kverrors.New(kverrors.InvalidTTL, "engine.ValidateTTL",
			"TTL %s must not be negative", ttl)
	}
	return nil
}
