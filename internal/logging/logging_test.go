package logging

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func TestParseLevel(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		want    slog.Level
		wantErr bool
	}{
		{"debug", "debug", slog.LevelDebug, false},
		{"info with spaces", "  info  ", slog.LevelInfo, false},
		{"warn", "warn", slog.LevelWarn, false},
		{"warning alias", "warning", slog.LevelWarn, false},
		{"error uppercase", "ERROR", slog.LevelError, false},
		{"unknown level", "loud", 0, true},
		{"empty level", "", 0, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseLevel(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Errorf("ParseLevel(%q) = %v, nil error; want error", tc.input, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseLevel(%q) returned error: %v", tc.input, err)
			}
			if got != tc.want {
				t.Errorf("ParseLevel(%q) = %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}

func TestNewWritesStructuredJSON(t *testing.T) {
	var buf bytes.Buffer
	log := New(&buf, slog.LevelDebug)

	log.Info("server started", "addr", "127.0.0.1:8080")

	out := buf.String()
	for _, want := range []string{`"level":"INFO"`, `"msg":"server started"`, `"addr":"127.0.0.1:8080"`} {
		if !strings.Contains(out, want) {
			t.Errorf("log output %q does not contain %q", out, want)
		}
	}
}

func TestNewRespectsLevelFilter(t *testing.T) {
	var buf bytes.Buffer
	log := New(&buf, slog.LevelWarn)

	log.Info("should be filtered")
	if buf.Len() != 0 {
		t.Errorf("Info log written at warn level: %q", buf.String())
	}

	log.Warn("should pass")
	if !strings.Contains(buf.String(), `"msg":"should pass"`) {
		t.Errorf("Warn log missing from output: %q", buf.String())
	}
}
