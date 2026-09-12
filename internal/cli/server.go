// Package cli implements the minikv commands: the HTTP server and the
// client subcommands that talk to a running server.
//
// Exit-code contract: 0 = success, 1 = operation failed, 2 = usage error.
// Commands never log key or value contents.
package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"minikv/internal/api"
	"minikv/internal/config"
	"minikv/internal/engine"
	"minikv/internal/kverrors"
	"minikv/internal/logging"
	"minikv/internal/persistence"
)

// RunServer implements `minikv server`. It blocks until SIGINT/SIGTERM,
// then shuts down gracefully and returns the exit code.
func RunServer(args []string, stderr io.Writer) int {
	fs := flag.NewFlagSet("server", flag.ContinueOnError)
	fs.SetOutput(stderr)
	addr := fs.String("addr", config.DefaultAddr, "HTTP listen address host:port")
	data := fs.String("data", config.DefaultDataDir, "data directory for WAL and snapshots")
	logLevel := fs.String("log-level", config.DefaultLogLevel, "log level: debug, info, warn, error")
	durability := fs.String("durability", string(persistence.DurabilityAlways),
		`WAL durability: "always" (fsync each mutation) or "never" (testing only)`)
	if err := fs.Parse(args); err != nil {
		fmt.Fprintln(stderr, "minikv: could not parse server flags:", err)
		return 2
	}

	cfg := config.Default()
	cfg.Addr = *addr
	cfg.DataDir = *data
	cfg.LogLevel = *logLevel
	if err := cfg.Validate(); err != nil {
		fmt.Fprintln(stderr, "minikv:", err)
		return 2
	}

	level, err := logging.ParseLevel(cfg.LogLevel)
	if err != nil {
		fmt.Fprintln(stderr, "minikv:", err)
		return 2
	}
	logger := logging.New(os.Stdout, level)
	logger.Info("starting minikv server",
		"addr", cfg.Addr,
		"data_dir", cfg.DataDir,
		"durability", *durability,
		"log_level", cfg.LogLevel,
	)

	mode := persistence.DurabilityAlways
	if *durability == string(persistence.DurabilityNever) {
		mode = persistence.DurabilityNever
	}

	eng, err := engine.Open(cfg.DataDir, mode, logger)
	if err != nil {
		fmt.Fprintln(stderr, "minikv:", err)
		return 1
	}

	srv := api.NewServer(eng, api.Config{
		Addr:           cfg.Addr,
		RequestTimeout: cfg.RequestTimeout,
		ShutdownGrace:  cfg.ShutdownGrace,
		Logger:         logger,
	})

	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.ListenAndServe() }()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	select {
	case err := <-serveErr:
		if err != nil {
			fmt.Fprintln(stderr, "minikv:", err)
			return 1
		}
	case sig := <-sigCh:
		logger.Info("signal received; shutting down gracefully", "signal", sig.String())
		ctx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownGrace)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			logger.Error("graceful shutdown failed", "error", err.Error())
			return 1
		}
	}

	logger.Info("minikv server stopped cleanly")
	return 0
}

// ensure kverrors stays referenced if error paths evolve.
var _ = kverrors.IsKind
