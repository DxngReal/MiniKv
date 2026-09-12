// Package config defines MiniKV's runtime configuration: server settings,
// data directory, durability mode, and limits.
//
// Configuration comes from flags in the CLI layer (Phase 4); this package
// owns the types, the defaults, and validation so both CLI and tests share
// one source of truth.
package config

import (
	"path/filepath"
	"time"

	"minikv/internal/kverrors"
)

// Default values applied when a flag is omitted.
const (
	DefaultAddr        = "127.0.0.1:8080"
	DefaultDataDir     = "./data"
	DefaultLogLevel    = "info"
	DefaultMaxKeyBytes = 512
	DefaultMaxValueB   = 1 << 20 // 1 MiB
)

// Durability controls when WAL writes are synced to disk.
type Durability string

// Durability modes.
const (
	// DurabilityAlways fsyncs every mutation before it is acknowledged.
	// This is the default required by the specification.
	DurabilityAlways Durability = "always"
	// DurabilityNever skips fsync for testing and experiments; it may lose
	// recent writes after an abrupt termination. Never the default.
	DurabilityNever Durability = "never"
)

// Config is the full runtime configuration for the MiniKV server.
type Config struct {
	// Addr is the HTTP listen address, host:port.
	Addr string
	// DataDir is the directory holding the WAL and snapshots.
	DataDir string
	// Durability selects the WAL fsync mode.
	Durability Durability
	// LogLevel is one of: debug, info, warn, error.
	LogLevel string
	// MaxKeyBytes limits key length in bytes.
	MaxKeyBytes int
	// MaxValueBytes limits value size in bytes.
	MaxValueBytes int
	// RequestTimeout bounds a single HTTP request's handling time.
	RequestTimeout time.Duration
	// ShutdownGrace bounds the graceful-shutdown wait period.
	ShutdownGrace time.Duration
}

// Default returns the default configuration with fsync-per-mutation
// durability, as required by the specification.
func Default() Config {
	return Config{
		Addr:           DefaultAddr,
		DataDir:        DefaultDataDir,
		Durability:     DurabilityAlways,
		LogLevel:       DefaultLogLevel,
		MaxKeyBytes:    DefaultMaxKeyBytes,
		MaxValueBytes:  DefaultMaxValueB,
		RequestTimeout: 10 * time.Second,
		ShutdownGrace:  10 * time.Second,
	}
}

// DataPath joins a file name inside the data directory.
func (c Config) DataPath(name string) string {
	return filepath.Join(c.DataDir, name)
}

// Validate checks the configuration and returns a typed error explaining
// what is wrong and how to fix it. It never logs key or value contents.
func (c Config) Validate() error {
	const op = "config.Validate"

	if c.Addr == "" {
		return kverrors.New(kverrors.ServerFailure, op, "listen address is empty")
	}
	if c.DataDir == "" {
		return kverrors.New(kverrors.DataDirFailure, op, "data directory is empty")
	}
	switch c.Durability {
	case DurabilityAlways, DurabilityNever:
	default:
		return kverrors.New(kverrors.ServerFailure, op,
			"durability mode %q is not supported: use %q or %q",
			c.Durability, DurabilityAlways, DurabilityNever)
	}
	switch c.LogLevel {
	case "debug", "info", "warn", "error":
	default:
		return kverrors.New(kverrors.ServerFailure, op,
			"log level %q is not supported: use debug, info, warn, or error", c.LogLevel)
	}
	if c.MaxKeyBytes <= 0 {
		return kverrors.New(kverrors.ServerFailure, op,
			"maximum key length %d must be positive", c.MaxKeyBytes)
	}
	if c.MaxValueBytes <= 0 {
		return kverrors.New(kverrors.ServerFailure, op,
			"maximum value size %d must be positive", c.MaxValueBytes)
	}
	if c.RequestTimeout <= 0 {
		return kverrors.New(kverrors.ServerFailure, op,
			"request timeout %s must be positive", c.RequestTimeout)
	}
	if c.ShutdownGrace <= 0 {
		return kverrors.New(kverrors.ServerFailure, op,
			"shutdown grace period %s must be positive", c.ShutdownGrace)
	}
	return nil
}
