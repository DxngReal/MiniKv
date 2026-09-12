package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// DefaultClientAddr is the server address used by client subcommands.
const DefaultClientAddr = "http://127.0.0.1:8080"

// maxResponseBytes caps client response reads (mirrors the API body cap).
const maxResponseBytes = 1 << 21

// httpClient builds the shared client with a sane timeout.
func httpClient() *http.Client {
	return &http.Client{Timeout: 10 * time.Second}
}

// splitArgs separates positionals from flags so flags may appear anywhere:
// Go's flag package stops at the first positional, which would break the
// documented form `minikv set KEY VALUE --ttl 60s`. Use `--` to treat all
// following arguments as positionals (needed for values starting with "-").
func splitArgs(args []string) (positional, flagArgs []string) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			positional = append(positional, args[i+1:]...)
			return
		}
		if strings.HasPrefix(a, "-") {
			flagArgs = append(flagArgs, a)
			if !strings.Contains(a, "=") && i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				flagArgs = append(flagArgs, args[i+1])
				i++
			}
			continue
		}
		positional = append(positional, a)
	}
	return
}

// printHTTPError prints a consistent error for a failed HTTP call. It never
// prints response bodies that could contain values — the API's error bodies
// are safe by design, so they are included.
func printHTTPError(stderr io.Writer, cmd string, resp *http.Response, body []byte) {
	msg := strings.TrimSpace(string(body))
	if msg == "" {
		msg = resp.Status
	}
	fmt.Fprintf(stderr, "minikv %s: %s\n", cmd, msg)
}

// do performs the request and returns the body regardless of status.
func do(cmd string, method, url string, body io.Reader, contentType string) (*http.Response, []byte, error) {
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, nil, err
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := httpClient().Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("could not reach the server at %s: %w; is `minikv server` running?", urlBase(url), err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, nil, fmt.Errorf("could not read the response: %w", err)
	}
	return resp, data, nil
}

func urlBase(u string) string {
	if i := strings.Index(u, "/v1/"); i >= 0 {
		return u[:i]
	}
	return u
}

// RunSet implements `minikv set KEY VALUE [--ttl duration]`.
func RunSet(args []string, stderr io.Writer) int {
	fs := flag.NewFlagSet("set", flag.ContinueOnError)
	fs.SetOutput(stderr)
	addr := fs.String("addr", DefaultClientAddr, "server base URL")
	ttl := fs.Duration("ttl", 0, "optional time-to-live, e.g. 60s; 0 means no expiration")
	positional, flagArgs := splitArgs(args)
	if err := fs.Parse(flagArgs); err != nil {
		return 2
	}
	if len(positional) != 2 {
		fmt.Fprintln(stderr, "usage: minikv set KEY VALUE [--ttl 60s] [--addr URL]")
		return 2
	}
	key, value := positional[0], positional[1]

	payload := struct {
		Value string `json:"value"`
		TTLMS int64  `json:"ttl_ms,omitempty"`
	}{Value: value}
	if ttl.Nanoseconds() > 0 {
		payload.TTLMS = ttl.Milliseconds()
	}
	body, _ := json.Marshal(payload)

	resp, data, err := do("set", http.MethodPut, *addr+"/v1/keys/"+key, strings.NewReader(string(body)), "application/json")
	if err != nil {
		fmt.Fprintln(stderr, "minikv set:", err)
		return 1
	}
	if resp.StatusCode >= 300 {
		printHTTPError(stderr, "set", resp, data)
		return 1
	}
	fmt.Fprintf(os.Stdout, "set %s\n", key)
	return 0
}

// RunGet implements `minikv get KEY`.
func RunGet(args []string, stderr io.Writer) int {
	fs := flag.NewFlagSet("get", flag.ContinueOnError)
	fs.SetOutput(stderr)
	addr := fs.String("addr", DefaultClientAddr, "server base URL")
	positional, flagArgs := splitArgs(args)
	if err := fs.Parse(flagArgs); err != nil {
		return 2
	}
	if len(positional) != 1 {
		fmt.Fprintln(stderr, "usage: minikv get KEY [--addr URL]")
		return 2
	}
	key := positional[0]

	resp, data, err := do("get", http.MethodGet, *addr+"/v1/keys/"+key, nil, "")
	if err != nil {
		fmt.Fprintln(stderr, "minikv get:", err)
		return 1
	}
	if resp.StatusCode == http.StatusNotFound {
		fmt.Fprintf(stderr, "minikv get: key not found: %s\n", key)
		return 1
	}
	if resp.StatusCode >= 300 {
		printHTTPError(stderr, "get", resp, data)
		return 1
	}
	var out struct{ Value string }
	if err := json.Unmarshal(data, &out); err != nil {
		fmt.Fprintln(stderr, "minikv get: could not parse the response:", err)
		return 1
	}
	fmt.Println(out.Value)
	return 0
}

// RunDelete implements `minikv delete KEY`.
func RunDelete(args []string, stderr io.Writer) int {
	fs := flag.NewFlagSet("delete", flag.ContinueOnError)
	fs.SetOutput(stderr)
	addr := fs.String("addr", DefaultClientAddr, "server base URL")
	positional, flagArgs := splitArgs(args)
	if err := fs.Parse(flagArgs); err != nil {
		return 2
	}
	if len(positional) != 1 {
		fmt.Fprintln(stderr, "usage: minikv delete KEY [--addr URL]")
		return 2
	}
	key := positional[0]

	resp, data, err := do("delete", http.MethodDelete, *addr+"/v1/keys/"+key, nil, "")
	if err != nil {
		fmt.Fprintln(stderr, "minikv delete:", err)
		return 1
	}
	if resp.StatusCode == http.StatusNotFound {
		fmt.Fprintf(stderr, "minikv delete: key not found: %s\n", key)
		return 1
	}
	if resp.StatusCode >= 300 {
		printHTTPError(stderr, "delete", resp, data)
		return 1
	}
	fmt.Fprintf(os.Stdout, "deleted %s\n", key)
	return 0
}

// RunKeys implements `minikv keys`.
func RunKeys(args []string, stderr io.Writer) int {
	fs := flag.NewFlagSet("keys", flag.ContinueOnError)
	fs.SetOutput(stderr)
	addr := fs.String("addr", DefaultClientAddr, "server base URL")
	positional, flagArgs := splitArgs(args)
	if err := fs.Parse(flagArgs); err != nil {
		return 2
	}
	if len(positional) != 0 {
		fmt.Fprintln(stderr, "usage: minikv keys [--addr URL]")
		return 2
	}
	resp, data, err := do("keys", http.MethodGet, *addr+"/v1/keys", nil, "")
	if err != nil {
		fmt.Fprintln(stderr, "minikv keys:", err)
		return 1
	}
	if resp.StatusCode >= 300 {
		printHTTPError(stderr, "keys", resp, data)
		return 1
	}
	var out struct {
		Keys []string `json:"keys"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		fmt.Fprintln(stderr, "minikv keys: could not parse the response:", err)
		return 1
	}
	if len(out.Keys) == 0 {
		fmt.Println("(no keys)")
		return 0
	}
	for _, k := range out.Keys {
		fmt.Println(k)
	}
	return 0
}

// RunStatus implements `minikv status`.
func RunStatus(args []string, stderr io.Writer) int {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	fs.SetOutput(stderr)
	addr := fs.String("addr", DefaultClientAddr, "server base URL")
	positional, flagArgs := splitArgs(args)
	if err := fs.Parse(flagArgs); err != nil {
		return 2
	}
	if len(positional) != 0 {
		fmt.Fprintln(stderr, "usage: minikv status [--addr URL]")
		return 2
	}
	resp, data, err := do("status", http.MethodGet, *addr+"/v1/status", nil, "")
	if err != nil {
		fmt.Fprintln(stderr, "minikv status:", err)
		return 1
	}
	if resp.StatusCode >= 300 {
		printHTTPError(stderr, "status", resp, data)
		return 1
	}
	var pretty map[string]any
	if err := json.Unmarshal(data, &pretty); err != nil {
		fmt.Fprintln(stderr, "minikv status: could not parse the response:", err)
		return 1
	}
	enc, _ := json.MarshalIndent(pretty, "", "  ")
	fmt.Println(string(enc))
	return 0
}

// RunSnapshot implements `minikv snapshot`.
func RunSnapshot(args []string, stderr io.Writer) int {
	fs := flag.NewFlagSet("snapshot", flag.ContinueOnError)
	fs.SetOutput(stderr)
	addr := fs.String("addr", DefaultClientAddr, "server base URL")
	positional, flagArgs := splitArgs(args)
	if err := fs.Parse(flagArgs); err != nil {
		return 2
	}
	if len(positional) != 0 {
		fmt.Fprintln(stderr, "usage: minikv snapshot [--addr URL]")
		return 2
	}
	resp, data, err := do("snapshot", http.MethodPost, *addr+"/v1/snapshot", nil, "")
	if err != nil {
		fmt.Fprintln(stderr, "minikv snapshot:", err)
		return 1
	}
	if resp.StatusCode >= 300 {
		printHTTPError(stderr, "snapshot", resp, data)
		return 1
	}
	var out struct {
		Entries int   `json:"entries"`
		Bytes   int64 `json:"bytes"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		fmt.Fprintln(stderr, "minikv snapshot: could not parse the response:", err)
		return 1
	}
	fmt.Fprintf(os.Stdout, "snapshot written: %d entries, %d bytes\n", out.Entries, out.Bytes)
	return 0
}
