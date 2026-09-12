package tests

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"minikv/internal/engine"
	"minikv/internal/kverrors"
	"minikv/internal/persistence"
)

// startServer builds a durable engine in dir and serves HTTP on 127.0.0.1:0.
// It returns the base URL and a stop function that gracefully shuts down.
func startServer(t *testing.T, dir string) (string, func()) {
	t.Helper()
	eng, err := engine.Open(dir, persistence.DurabilityNever, nil)
	if err != nil {
		t.Fatalf("engine.Open() error = %v", err)
	}
	srv := newTestAPIServer(eng, t)
	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServe() }()

	base := srv.URL()
	stop := func() {
		ctx, cancel := contextWithTimeout(5 * time.Second)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			t.Errorf("Shutdown() error = %v", err)
		}
		if err := <-errCh; err != nil {
			t.Errorf("ListenAndServe() error = %v", err)
		}
	}
	return base, stop
}

// httpReq performs a request and fails the test on transport errors.
func httpReq(t *testing.T, method, url string, body []byte) (*http.Response, []byte) {
	t.Helper()
	client := &http.Client{Timeout: 5 * time.Second}
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, url, reader)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	return resp, data
}

// TestHTTPCRUDFlow exercises the full acceptance flow over real TCP:
// SET → GET → DELETE, plus keys and status.
func TestHTTPCRUDFlow(t *testing.T) {
	base, stop := startServer(t, t.TempDir())
	defer stop()

	// SET create → 201
	resp, body := httpReq(t, http.MethodPut, base+"/v1/keys/greeting", []byte(`{"value":"hello"}`))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("SET = %d, want 201; %s", resp.StatusCode, body)
	}

	// SET overwrite → 200
	resp, _ = httpReq(t, http.MethodPut, base+"/v1/keys/greeting", []byte(`{"value":"world"}`))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("overwrite SET = %d, want 200", resp.StatusCode)
	}

	// GET → 200 "world"
	resp, body = httpReq(t, http.MethodGet, base+"/v1/keys/greeting", nil)
	if resp.StatusCode != http.StatusOK || !bytes.Contains(body, []byte("world")) {
		t.Fatalf("GET = %d %s, want 200 with world", resp.StatusCode, body)
	}

	// KEYS lists exactly one key
	resp, body = httpReq(t, http.MethodGet, base+"/v1/keys", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("KEYS = %d, want 200", resp.StatusCode)
	}
	var list struct {
		Keys []string `json:"keys"`
	}
	if err := json.Unmarshal(body, &list); err != nil || len(list.Keys) != 1 || list.Keys[0] != "greeting" {
		t.Fatalf("KEYS body = %s, want exactly [greeting]", body)
	}

	// STATUS reports 1 key and durability mode
	resp, body = httpReq(t, http.MethodGet, base+"/v1/status", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("STATUS = %d, want 200", resp.StatusCode)
	}
	var status map[string]any
	if err := json.Unmarshal(body, &status); err != nil {
		t.Fatalf("STATUS body not JSON: %v", err)
	}
	if status["key_count"].(float64) != 1 {
		t.Errorf("key_count = %v, want 1", status["key_count"])
	}
	if status["wal_bytes"].(float64) <= 0 {
		t.Errorf("wal_bytes = %v, want > 0", status["wal_bytes"])
	}

	// DELETE → 204, then GET → 404
	resp, _ = httpReq(t, http.MethodDelete, base+"/v1/keys/greeting", nil)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("DELETE = %d, want 204", resp.StatusCode)
	}
	resp, _ = httpReq(t, http.MethodGet, base+"/v1/keys/greeting", nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("GET deleted = %d, want 404", resp.StatusCode)
	}
}

// TestTTLEXpiresKeys verifies ttl_ms expiry through the HTTP surface.
func TestTTLEXpiresKeys(t *testing.T) {
	base, stop := startServer(t, t.TempDir())
	defer stop()

	resp, _ := httpReq(t, http.MethodPut, base+"/v1/keys/temp",
		[]byte(`{"value":"gone-soon","ttl_ms":60}`))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("SET with ttl = %d, want 201", resp.StatusCode)
	}

	time.Sleep(120 * time.Millisecond)

	resp, _ = httpReq(t, http.MethodGet, base+"/v1/keys/temp", nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("GET expired = %d, want 404", resp.StatusCode)
	}
}

// TestWALRecoveryAfterRestart simulates an abrupt termination: it writes
// keys, closes the engine without a snapshot (WAL-only durability), reopens
// on the same directory, and verifies all data is restored.
func TestWALRecoveryAfterRestart(t *testing.T) {
	dir := t.TempDir()

	base, stop := startServer(t, dir)
	for i := 0; i < 25; i++ {
		payload := fmt.Sprintf(`{"value":"v%d"}`, i)
		resp, body := httpReq(t, http.MethodPut, base+fmt.Sprintf("/v1/keys/key-%02d", i), []byte(payload))
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("SET %d = %d, want 201; %s", i, resp.StatusCode, body)
		}
	}
	// Delete one so recovery must also honor deletes.
	httpReq(t, http.MethodDelete, base+"/v1/keys/key-00", nil)
	stop()

	// Reopen on the same directory.
	eng2, err := engine.Open(dir, persistence.DurabilityNever, nil)
	if err != nil {
		t.Fatalf("reopen error = %v", err)
	}
	defer eng2.Close()

	for i := 1; i < 25; i++ {
		got, err := eng2.Get(fmt.Sprintf("key-%02d", i))
		if err != nil || string(got) != fmt.Sprintf("v%d", i) {
			t.Errorf("key-%02d = (%q, %v), want v%d", i, got, err, i)
		}
	}
	if _, err := eng2.Get("key-00"); !kverrors.IsKind(err, kverrors.KeyNotFound) {
		t.Errorf("deleted key survived recovery: %v", err)
	}
}

// TestSnapshotThenRecovery verifies snapshot creation over HTTP and that a
// fresh process restores from snapshot + subsequent WAL records.
func TestSnapshotThenRecovery(t *testing.T) {
	dir := t.TempDir()

	base, stop := startServer(t, dir)
	httpReq(t, http.MethodPut, base+"/v1/keys/snap-a", []byte(`{"value":"1"}`))
	resp, body := httpReq(t, http.MethodPost, base+"/v1/snapshot", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /v1/snapshot = %d, want 200; %s", resp.StatusCode, body)
	}
	// Mutations after the snapshot must come back from the WAL.
	httpReq(t, http.MethodPut, base+"/v1/keys/snap-b", []byte(`{"value":"2"}`))
	httpReq(t, http.MethodDelete, base+"/v1/keys/snap-a", nil)
	stop()

	eng2, err := engine.Open(dir, persistence.DurabilityNever, nil)
	if err != nil {
		t.Fatalf("reopen error = %v", err)
	}
	defer eng2.Close()

	if got, err := eng2.Get("snap-b"); err != nil || string(got) != "2" {
		t.Errorf("snap-b = (%q, %v), want 2", got, err)
	}
	if _, err := eng2.Get("snap-a"); !kverrors.IsKind(err, kverrors.KeyNotFound) {
		t.Errorf("snap-a should have been deleted after the snapshot: %v", err)
	}
}

// TestCorruptedWALIsReportedNotDeleted writes records, corrupts one byte in
// the middle, restarts, and verifies: recovery fails loudly with file and
// offset, and the WAL file is byte-identical (never truncated or repaired).
func TestCorruptedWALIsReportedNotDeleted(t *testing.T) {
	dir := t.TempDir()

	eng, err := engine.Open(dir, persistence.DurabilityNever, nil)
	if err != nil {
		t.Fatalf("open error = %v", err)
	}
	for i := 0; i < 5; i++ {
		if _, err := eng.Set(fmt.Sprintf("k%d", i), []byte("value"), nil); err != nil {
			t.Fatalf("Set(%d) error = %v", i, err)
		}
	}
	eng.Close()

	walPath := filepath.Join(dir, persistence.WALFileName)
	original, err := os.ReadFile(walPath)
	if err != nil {
		t.Fatalf("read WAL: %v", err)
	}

	// Corrupt one byte inside the third record (header is 30 bytes each;
	// value is 5 bytes, so record 3 starts around offset 2*(30+8+5+4)).
	thirdRecord := 2 * (30 + len("k2") + len("value") + 4)
	corrupted := append([]byte(nil), original...)
	corrupted[thirdRecord+31] ^= 0xFF
	if err := os.WriteFile(walPath, corrupted, 0o644); err != nil {
		t.Fatalf("write corrupted WAL: %v", err)
	}

	// Reopen must fail with a typed corruption error naming file and offset.
	_, err = engine.Open(dir, persistence.DurabilityNever, nil)
	if !kverrors.IsKind(err, kverrors.WALCorruption) {
		t.Fatalf("reopen error = %v, want kind %q", err, kverrors.WALCorruption)
	}
	if !bytes.Contains([]byte(err.Error()), []byte(persistence.WALFileName)) ||
		!bytes.Contains([]byte(err.Error()), []byte("offset")) {
		t.Errorf("corruption error %q must name the file and offset", err)
	}

	// The corrupted file must be untouched by the failed recovery.
	after, err := os.ReadFile(walPath)
	if err != nil {
		t.Fatalf("re-read WAL: %v", err)
	}
	if !bytes.Equal(corrupted, after) {
		t.Error("recovery modified the corrupted WAL file")
	}
}

// TestCLICommands runs the compiled binary against a live server, exercising
// set/get/delete/keys/status/snapshot and exit codes end to end.
func TestCLICommands(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping binary build in short mode")
	}
	base, stop := startServer(t, t.TempDir())
	defer stop()

	bin := filepath.Join(t.TempDir(), "minikv-test-bin.exe")
	build := exec.Command("go", "build", "-o", bin, "./cmd/minikv")
	build.Dir = ".." // repository root: the parent of tests/
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build minikv binary: %v\n%s", err, out)
	}

	// runCLI appends --addr after the subcommand and positionals, which also
	// exercises the flags-after-positionals parsing path.
	runCLI := func(args ...string) (int, string) {
		full := append(append([]string{}, args...), "--addr", base)
		cmd := exec.Command(bin, full...)
		var out bytes.Buffer
		cmd.Stdout = &out
		cmd.Stderr = &out
		err := cmd.Run()
		code := 0
		if exit, ok := err.(*exec.ExitError); ok {
			code = exit.ExitCode()
		} else if err != nil {
			t.Fatalf("run %v: %v", full, err)
		}
		return code, out.String()
	}

	if code, out := runCLI("set", "greeting", "hello"); code != 0 {
		t.Errorf("set exit = %d, want 0; out: %s", code, out)
	}
	if code, out := runCLI("get", "greeting"); code != 0 || out != "hello\n" {
		t.Errorf("get exit = %d out = %q, want 0 / hello", code, out)
	}
	if code, out := runCLI("keys"); code != 0 || out != "greeting\n" {
		t.Errorf("keys exit = %d out = %q, want 0 / greeting", code, out)
	}
	if code, _ := runCLI("status"); code != 0 {
		t.Errorf("status exit != 0")
	}
	if code, _ := runCLI("snapshot"); code != 0 {
		t.Errorf("snapshot exit != 0")
	}
	if code, _ := runCLI("delete", "greeting"); code != 0 {
		t.Errorf("delete exit != 0")
	}
	if code, _ := runCLI("get", "greeting"); code != 1 {
		t.Errorf("get missing exit = %d, want 1", code)
	}
	if code, _ := runCLI("version"); code != 0 {
		t.Errorf("version exit != 0")
	}
	if code, _ := runCLI("bogus-command"); code != 2 {
		t.Errorf("unknown command exit = %d, want 2", code)
	}
}
