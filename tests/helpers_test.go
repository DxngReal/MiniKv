package tests

import (
	"context"
	"net"
	"testing"
	"time"

	"minikv/internal/api"
	"minikv/internal/engine"
)

// newTestAPIServer wraps api.NewServer and exposes the bound address via
// URL(), which the real-TCP integration tests need.
type testAPIServer struct {
	*api.Server
	ln net.Listener
}

// URL returns the base URL of the listening test server.
func (s *testAPIServer) URL() string {
	return "http://" + s.ln.Addr().String()
}

// newTestAPIServer builds an api.Server bound to an ephemeral port.
func newTestAPIServer(eng engine.Engine, t *testing.T) *testAPIServer {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv := api.NewServer(eng, api.Config{ShutdownGrace: 5 * time.Second})
	// Bind the server's http.Server to this listener by starting Serve in
	// ListenAndServe; the integration helper starts it in a goroutine.
	return &testAPIServer{Server: srv, ln: ln}
}

// ListenAndServe serves on the pre-bound listener. It mirrors
// api.Server.ListenAndServe but reuses the test listener so the URL is
// known before serving starts.
func (s *testAPIServer) ListenAndServe() error {
	return s.Server.ServeOn(s.ln)
}

// contextWithTimeout is a small helper for shutdown contexts.
func contextWithTimeout(d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), d)
}
