package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRunVersionExitsZero(t *testing.T) {
	for _, arg := range []string{"version", "--version", "-v"} {
		var out, errOut bytes.Buffer
		if code := run([]string{arg}, &out, &errOut); code != 0 {
			t.Errorf("run(%q) = %d, want 0", arg, code)
		}
		if !strings.Contains(out.String(), "minikv ") {
			t.Errorf("version output %q lacks version string", out.String())
		}
	}
}

func TestRunHelpExitsZero(t *testing.T) {
	for _, arg := range []string{"help", "--help", "-h"} {
		var out, errOut bytes.Buffer
		if code := run([]string{arg}, &out, &errOut); code != 0 {
			t.Errorf("run(%q) = %d, want 0", arg, code)
		}
		if !strings.Contains(out.String(), "Usage:") {
			t.Errorf("help output %q lacks usage", out.String())
		}
	}
}

func TestRunNoArgsExitsTwo(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run([]string{}, &out, &errOut); code != 2 {
		t.Errorf("run(no args) = %d, want 2", code)
	}
}

func TestRunUnknownCommandExitsTwo(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run([]string{"definitely-not-a-command"}, &out, &errOut); code != 2 {
		t.Errorf("run(unknown) = %d, want 2", code)
	}
}

func TestClientCommandUsageErrorsExitTwo(t *testing.T) {
	cases := [][]string{
		{"set"},             // missing key and value
		{"set", "onlykey"},  // missing value
		{"get"},             // missing key
		{"delete"},          // missing key
		{"keys", "extra"},   // too many args
		{"status", "extra"}, // too many args
		{"snapshot", "x"},   // too many args
	}
	for _, args := range cases {
		var out, errOut bytes.Buffer
		if code := run(args, &out, &errOut); code != 2 {
			t.Errorf("run(%v) = %d, want 2 (usage error); stderr: %q", args, code, errOut.String())
		}
	}
}

func TestClientCommandUnreachableServerExitsOne(t *testing.T) {
	// Port 1 is not listening; the client must fail with exit code 1.
	var out, errOut bytes.Buffer
	if code := run([]string{"get", "k", "--addr", "http://127.0.0.1:1"}, &out, &errOut); code != 1 {
		t.Errorf("run(get unreachable) = %d, want 1; stderr: %q", code, errOut.String())
	}
}
