package config

import (
	"testing"
	"time"

	"minikv/internal/kverrors"
)

func TestDefaultIsValid(t *testing.T) {
	cfg := Default()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Default().Validate() = %v, want nil", err)
	}
	if cfg.Addr != DefaultAddr {
		t.Errorf("default Addr = %q, want %q", cfg.Addr, DefaultAddr)
	}
	if cfg.Durability != DurabilityAlways {
		t.Errorf("default Durability = %q, want %q", cfg.Durability, DurabilityAlways)
	}
	if cfg.LogLevel != DefaultLogLevel {
		t.Errorf("default LogLevel = %q, want %q", cfg.LogLevel, DefaultLogLevel)
	}
	if cfg.DataPath("wal.log") == "wal.log" || cfg.DataPath("wal.log") != "data/wal.log" && cfg.DataPath("wal.log") != `data\wal.log` {
		t.Errorf("DataPath(wal.log) = %q, want a path inside the data dir", cfg.DataPath("wal.log"))
	}
}

func TestValidateRejectsBadConfig(t *testing.T) {
	cases := []struct {
		name     string
		mutate   func(c *Config)
		wantKind kverrors.Kind
	}{
		{"empty addr", func(c *Config) { c.Addr = "" }, kverrors.ServerFailure},
		{"empty data dir", func(c *Config) { c.DataDir = "" }, kverrors.DataDirFailure},
		{"bad durability", func(c *Config) { c.Durability = "sometimes" }, kverrors.ServerFailure},
		{"bad log level", func(c *Config) { c.LogLevel = "loud" }, kverrors.ServerFailure},
		{"zero max key", func(c *Config) { c.MaxKeyBytes = 0 }, kverrors.ServerFailure},
		{"negative max value", func(c *Config) { c.MaxValueBytes = -1 }, kverrors.ServerFailure},
		{"zero request timeout", func(c *Config) { c.RequestTimeout = 0 }, kverrors.ServerFailure},
		{"zero shutdown grace", func(c *Config) { c.ShutdownGrace = 0 * time.Second }, kverrors.ServerFailure},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := Default()
			tc.mutate(&cfg)
			err := cfg.Validate()
			if !kverrors.IsKind(err, tc.wantKind) {
				t.Errorf("Validate() = %v, want kind %q", err, tc.wantKind)
			}
		})
	}
}

func TestDurabilityNeverIsNotDefault(t *testing.T) {
	cfg := Default()
	if cfg.Durability == DurabilityNever {
		t.Error("default durability must be fsync-per-mutation, never the unsafe mode")
	}
}

func TestRequestTimeoutDefaultIsSane(t *testing.T) {
	if got := Default().RequestTimeout; got != 10*time.Second {
		t.Errorf("default RequestTimeout = %s, want 10s", got)
	}
}
