package main

import "testing"

func TestRunVersionExitsZero(t *testing.T) {
	for _, arg := range []string{"version", "--version", "-v"} {
		if code := run([]string{arg}); code != 0 {
			t.Errorf("run(%q) = %d, want 0", arg, code)
		}
	}
}

func TestRunHelpExitsZero(t *testing.T) {
	for _, arg := range []string{"help", "--help", "-h"} {
		if code := run([]string{arg}); code != 0 {
			t.Errorf("run(%q) = %d, want 0", arg, code)
		}
	}
}

func TestRunNoArgsExitsTwo(t *testing.T) {
	if code := run([]string{}); code != 2 {
		t.Errorf("run(no args) = %d, want 2", code)
	}
}

func TestRunUnknownCommandExitsTwo(t *testing.T) {
	if code := run([]string{"definitely-not-a-command"}); code != 2 {
		t.Errorf("run(unknown) = %d, want 2", code)
	}
}

func TestRunPlannedCommandsExitOne(t *testing.T) {
	for _, arg := range []string{"server", "set", "get", "delete", "keys", "status", "snapshot"} {
		if code := run([]string{arg}); code != 1 {
			t.Errorf("run(%q) = %d, want 1 (planned, not implemented)", arg, code)
		}
	}
}
