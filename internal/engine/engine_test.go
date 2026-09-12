package engine

import (
	"strings"
	"testing"
	"time"

	"minikv/internal/kverrors"
)

func TestValidateKey(t *testing.T) {
	cases := []struct {
		name    string
		key     string
		max     int
		wantErr bool
	}{
		{"normal key", "hello", MaxKeyBytes, false},
		{"empty key", "", MaxKeyBytes, true},
		{"key at limit", strings.Repeat("k", MaxKeyBytes), MaxKeyBytes, false},
		{"key over limit", strings.Repeat("k", MaxKeyBytes+1), MaxKeyBytes, true},
		{"small custom limit", "hello", 3, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateKey(tc.key, tc.max)
			if (err != nil) != tc.wantErr {
				t.Errorf("ValidateKey(len=%d, max=%d) error = %v, wantErr %v",
					len(tc.key), tc.max, err, tc.wantErr)
			}
			if tc.wantErr && !kverrors.IsKind(err, kverrors.InvalidKey) {
				t.Errorf("ValidateKey error = %v, want kind %q", err, kverrors.InvalidKey)
			}
		})
	}
}

func TestValidateValue(t *testing.T) {
	if err := ValidateValue([]byte("ok"), MaxValueBytes); err != nil {
		t.Errorf("ValidateValue(ok) = %v, want nil", err)
	}
	if err := ValidateValue(nil, MaxValueBytes); !kverrors.IsKind(err, kverrors.InvalidValue) {
		t.Errorf("ValidateValue(nil) = %v, want kind %q", err, kverrors.InvalidValue)
	}
	big := make([]byte, MaxValueBytes+1)
	if err := ValidateValue(big, MaxValueBytes); !kverrors.IsKind(err, kverrors.InvalidValue) {
		t.Errorf("ValidateValue(too big) = %v, want kind %q", err, kverrors.InvalidValue)
	}
}

func TestValidateTTL(t *testing.T) {
	if err := ValidateTTL(0); err != nil {
		t.Errorf("ValidateTTL(0) = %v, want nil", err)
	}
	if err := ValidateTTL(time.Second); err != nil {
		t.Errorf("ValidateTTL(1s) = %v, want nil", err)
	}
	if err := ValidateTTL(-time.Second); !kverrors.IsKind(err, kverrors.InvalidTTL) {
		t.Errorf("ValidateTTL(-1s) = %v, want kind %q", err, kverrors.InvalidTTL)
	}
}

func TestStatsHitRate(t *testing.T) {
	s := Stats{Gets: 4, HitCount: 1}
	if got := s.HitRate(); got != 0.25 {
		t.Errorf("HitRate() = %v, want 0.25", got)
	}
	empty := Stats{}
	if got := empty.HitRate(); got != 0 {
		t.Errorf("HitRate() on empty stats = %v, want 0", got)
	}
}
