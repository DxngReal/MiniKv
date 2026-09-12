// Package tests hosts cross-package integration tests that need a real
// binary or multiple layers working together.
//
// Phase 1 contains only a sanity test proving the test harness works.
// Later phases add HTTP end-to-end and crash-recovery integration tests
// here, all using temporary directories.
package tests

import "testing"

// TestHarnessWorks verifies the integration test package compiles and runs.
func TestHarnessWorks(t *testing.T) {
	if testing.Short() {
		t.Skip("sanity check skipped in short mode")
	}
}
