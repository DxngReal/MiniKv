package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"minikv/internal/engine"
	"minikv/internal/persistence"
)

// newTestServer builds a durable engine over a temp dir plus the API server.
// The engine is registered for Close via t.Cleanup so the WAL handle is
// released before TempDir cleanup (Windows cannot delete open files).
func newTestServer(t *testing.T) (*Server, *engine.DurableStore) {
	t.Helper()
	eng, err := engine.Open(t.TempDir(), persistence.DurabilityNever, nil)
	if err != nil {
		t.Fatalf("engine.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = eng.Close() })
	return NewServer(eng, Config{}), eng
}

// doReq performs an in-memory HTTP request against the mux.
func doReq(t *testing.T, s *Server, method, target string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	} else {
		reader = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, target, reader)
	w := httptest.NewRecorder()
	s.routes().ServeHTTP(w, req)
	return w
}

func TestSetGetDeleteEndpoints(t *testing.T) {
	s, _ := newTestServer(t)

	// SET (create) → 201
	w := doReq(t, s, http.MethodPut, "/v1/keys/greeting", []byte(`{"value":"hello"}`))
	if w.Code != http.StatusCreated {
		t.Fatalf("PUT new key = %d, want 201; body: %s", w.Code, w.Body.String())
	}

	// SET (overwrite) → 200
	w = doReq(t, s, http.MethodPut, "/v1/keys/greeting", []byte(`{"value":"again"}`))
	if w.Code != http.StatusOK {
		t.Fatalf("PUT existing key = %d, want 200", w.Code)
	}

	// GET → 200 + value
	w = doReq(t, s, http.MethodGet, "/v1/keys/greeting", nil)
	if w.Code != http.StatusOK || !bytes.Contains(w.Body.Bytes(), []byte(`"value":"again"`)) {
		t.Fatalf("GET = %d %s, want 200 with the stored value", w.Code, w.Body.String())
	}

	// DELETE → 204
	w = doReq(t, s, http.MethodDelete, "/v1/keys/greeting", nil)
	if w.Code != http.StatusNoContent {
		t.Fatalf("DELETE = %d, want 204", w.Code)
	}

	// DELETE again → 404
	w = doReq(t, s, http.MethodDelete, "/v1/keys/greeting", nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("DELETE missing = %d, want 404", w.Code)
	}

	// GET missing → 404 with typed error body
	w = doReq(t, s, http.MethodGet, "/v1/keys/greeting", nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("GET missing = %d, want 404", w.Code)
	}
	var eBody errorBody
	if err := json.Unmarshal(w.Body.Bytes(), &eBody); err != nil {
		t.Fatalf("error body is not valid JSON: %v", err)
	}
	if eBody.Error != "key_not_found" || eBody.Hint == "" {
		t.Errorf("error body = %+v, want kind key_not_found with a hint", eBody)
	}
}

func TestSetValidationErrors(t *testing.T) {
	s, _ := newTestServer(t)

	cases := []struct {
		name     string
		body     string
		wantCode int
		wantKind string
	}{
		{"missing value", `{}`, 400, "invalid_value"},
		{"malformed json", `{not json`, 400, "invalid_value"},
		{"negative ttl", `{"value":"v","ttl_ms":-5}`, 400, "invalid_ttl"},
		{"value too large", fmt.Sprintf(`{"value":"%s"}`, string(make([]byte, 1<<20+10))), 400, "invalid_value"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := doReq(t, s, http.MethodPut, "/v1/keys/k", []byte(tc.body))
			if w.Code != tc.wantCode {
				t.Errorf("code = %d, want %d; body %s", w.Code, tc.wantCode, w.Body.String())
			}
			var e errorBody
			if err := json.Unmarshal(w.Body.Bytes(), &e); err != nil {
				t.Fatalf("error body not JSON: %v", err)
			}
			if e.Error != tc.wantKind {
				t.Errorf("kind = %q, want %q", e.Error, tc.wantKind)
			}
			if e.Hint == "" {
				t.Error("hint is empty")
			}
		})
	}

	// ttl_ms: 0 means no expiration and must be accepted.
	w := doReq(t, s, http.MethodPut, "/v1/keys/k", []byte(`{"value":"v","ttl_ms":0}`))
	if w.Code != http.StatusCreated {
		t.Errorf("PUT ttl_ms=0 = %d, want 201; body %s", w.Code, w.Body.String())
	}
}

func TestTTLExpirationOverHTTP(t *testing.T) {
	s, _ := newTestServer(t)

	w := doReq(t, s, http.MethodPut, "/v1/keys/temp", []byte(`{"value":"v","ttl_ms":50}`))
	if w.Code != http.StatusCreated {
		t.Fatalf("PUT with ttl = %d, want 201", w.Code)
	}
	time.Sleep(80 * time.Millisecond)

	w = doReq(t, s, http.MethodGet, "/v1/keys/temp", nil)
	if w.Code != http.StatusNotFound {
		t.Errorf("GET expired = %d, want 404; body %s", w.Code, w.Body.String())
	}
}

func TestKeysEndpointHidesValues(t *testing.T) {
	s, _ := newTestServer(t)
	_ = doReq(t, s, http.MethodPut, "/v1/keys/a", []byte(`{"value":"secret-a"}`))
	_ = doReq(t, s, http.MethodPut, "/v1/keys/b", []byte(`{"value":"secret-b"}`))

	w := doReq(t, s, http.MethodGet, "/v1/keys", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /v1/keys = %d, want 200", w.Code)
	}
	var out struct {
		Keys []string `json:"keys"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("bad JSON: %v", err)
	}
	if len(out.Keys) != 2 {
		t.Errorf("keys = %v, want [a b]", out.Keys)
	}
	if bytes.Contains(w.Body.Bytes(), []byte("secret")) {
		t.Error("key listing must never expose values")
	}
}

func TestStatusEndpointFields(t *testing.T) {
	s, _ := newTestServer(t)

	_ = doReq(t, s, http.MethodPut, "/v1/keys/x", []byte(`{"value":"1"}`))
	w := doReq(t, s, http.MethodGet, "/v1/status", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /v1/status = %d, want 200", w.Code)
	}
	var st statusResponse
	if err := json.Unmarshal(w.Body.Bytes(), &st); err != nil {
		t.Fatalf("bad JSON: %v", err)
	}
	if st.Version == "" || st.KeyCount != 1 || st.WALBytes <= 0 {
		t.Errorf("status = %+v, want version, 1 key, positive WAL bytes", st)
	}
	if st.DurabilityMode != "never" {
		t.Errorf("DurabilityMode = %q, want never (test opens with never)", st.DurabilityMode)
	}
}

func TestSnapshotEndpointWritesFile(t *testing.T) {
	s, eng := newTestServer(t)

	_ = doReq(t, s, http.MethodPut, "/v1/keys/persist-me", []byte(`{"value":"before"}`))
	w := doReq(t, s, http.MethodPost, "/v1/snapshot", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("POST /v1/snapshot = %d, want 200; body %s", w.Code, w.Body.String())
	}

	// Restart on the same directory restores the key.
	eng.Close()
	eng2, err := engine.Open(eng.Dir(), persistence.DurabilityNever, nil)
	if err != nil {
		t.Fatalf("reopen error = %v", err)
	}
	defer eng2.Close()
	got, err := eng2.Get("persist-me")
	if err != nil || string(got) != "before" {
		t.Errorf("Get after restart = (%q, %v), want before", got, err)
	}
}

func TestGracefulShutdownClosesEngine(t *testing.T) {
	s, eng := newTestServer(t)

	_ = doReq(t, s, http.MethodPut, "/v1/keys/k", []byte(`{"value":"v"}`))

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := s.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}

	// Engine must be closed after server shutdown.
	if _, err := eng.Get("k"); err == nil {
		t.Error("engine still usable after server shutdown")
	}
}

func TestMethodNotAllowedIsRejected(t *testing.T) {
	s, _ := newTestServer(t)
	w := doReq(t, s, http.MethodPost, "/v1/keys/k", []byte(`{}`))
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST /v1/keys/{key} = %d, want 405", w.Code)
	}
}
