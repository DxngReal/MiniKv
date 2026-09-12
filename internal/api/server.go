package api

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"time"

	"minikv/internal/engine"
	"minikv/internal/kverrors"
	"minikv/internal/version"
)

// Server serves the MiniKV REST API.
type Server struct {
	eng     engine.Engine
	logger  *slog.Logger
	http    *http.Server
	started time.Time
}

// Config holds the HTTP server settings.
type Config struct {
	// Addr is the listen address, host:port.
	Addr string
	// RequestTimeout bounds a single request's handling time.
	RequestTimeout time.Duration
	// ShutdownGrace bounds the graceful-shutdown wait.
	ShutdownGrace time.Duration
	// Logger receives lifecycle messages; nil discards logs.
	Logger *slog.Logger
}

// NewServer builds a server around an engine.
func NewServer(eng engine.Engine, cfg Config) *Server {
	if cfg.RequestTimeout <= 0 {
		cfg.RequestTimeout = 10 * time.Second
	}
	if cfg.ShutdownGrace <= 0 {
		cfg.ShutdownGrace = 10 * time.Second
	}
	logger := cfg.Logger
	if logger == nil {
		logger = defaultLogger
	}
	s := &Server{
		eng:     eng,
		logger:  logger,
		started: time.Now(),
	}
	s.http = &http.Server{
		Addr:              cfg.Addr,
		Handler:           s.routes(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       cfg.RequestTimeout,
		WriteTimeout:      cfg.RequestTimeout,
		IdleTimeout:       60 * time.Second,
	}
	return s
}

// ListenAndServe runs the HTTP server until Shutdown is called.
func (s *Server) ListenAndServe() error {
	ln, err := net.Listen("tcp", s.http.Addr)
	if err != nil {
		return kverrors.Wrap(kverrors.ServerFailure, "api.Listen", err,
			"could not bind the listen address: check that the port is free")
	}
	return s.ServeOn(ln)
}

// ServeOn serves on an existing listener until Shutdown is called.
func (s *Server) ServeOn(ln net.Listener) error {
	s.logger.Info("http server listening", "addr", ln.Addr().String(), "version", version.Version)
	if err := s.http.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return kverrors.Wrap(kverrors.ServerFailure, "api.Serve", err,
			"the HTTP server failed while serving")
	}
	return nil
}

// Shutdown gracefully drains connections, then closes the engine. The
// engine close happens exactly once; ListenAndServe returning normally
// (after Shutdown) leaves the engine closed.
func (s *Server) Shutdown(ctx context.Context) error {
	s.logger.Info("http server shutting down")
	err := s.http.Shutdown(ctx)
	if engErr := s.eng.Close(); engErr != nil {
		s.logger.Error("engine close failed during shutdown", "error", engErr.Error())
		if err == nil {
			err = engErr
		}
	}
	return err
}
