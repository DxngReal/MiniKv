package kverrors

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestErrorMessageContainsAllParts(t *testing.T) {
	inner := errors.New("disk full")
	err := Wrap(SnapshotFailure, "snapshot.Write", inner, "snapshot could not be written")

	got := err.Error()
	for _, want := range []string{
		"minikv:",
		"snapshot.Write",
		string(SnapshotFailure),
		"snapshot could not be written",
		"disk full",
		"Next step:",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("Error() = %q, want it to contain %q", got, want)
		}
	}
}

func TestNewWithoutCauseOmitsCause(t *testing.T) {
	err := New(InvalidKey, "engine.Set", "key exceeds maximum length")
	got := err.Error()
	if strings.Contains(got, ": .") {
		t.Errorf("Error() = %q, want no dangling empty cause", got)
	}
	if err.Unwrap() != nil {
		t.Errorf("Unwrap() = %v, want nil", err.Unwrap())
	}
}

func TestIsKindUnwrapsWrappedErrors(t *testing.T) {
	base := New(KeyNotFound, "engine.Get", "key does not exist")
	wrapped := fmt.Errorf("handle request: %w", base)

	if !IsKind(wrapped, KeyNotFound) {
		t.Error("IsKind(wrapped, KeyNotFound) = false, want true")
	}
	if IsKind(wrapped, InvalidKey) {
		t.Error("IsKind(wrapped, InvalidKey) = true, want false")
	}
	if IsKind(errors.New("other"), KeyNotFound) {
		t.Error("IsKind(plain error, KeyNotFound) = true, want false")
	}
}

func TestKindOf(t *testing.T) {
	base := New(InvalidTTL, "engine.Set", "ttl must not be negative")
	kind, ok := KindOf(fmt.Errorf("outer: %w", base))
	if !ok || kind != InvalidTTL {
		t.Errorf("KindOf(wrapped) = (%q, %v), want (%q, true)", kind, ok, InvalidTTL)
	}

	if _, ok := KindOf(errors.New("other")); ok {
		t.Error("KindOf(plain error) returned ok, want false")
	}
}

func TestHintsPresentForAllKinds(t *testing.T) {
	kinds := []Kind{
		KeyNotFound, InvalidKey, InvalidValue, InvalidTTL,
		WALCorruption, SnapshotFailure, RecoveryFailure,
		DataDirFailure, ServerFailure, StoreClosed,
	}
	for _, k := range kinds {
		if k.Hint() == "" {
			t.Errorf("Kind %q has an empty hint", k)
		}
	}
}
