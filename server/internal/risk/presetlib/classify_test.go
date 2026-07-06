package presetlib

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// newLibrary parses the embedded catalog and fails the test on any error. Shared
// by the presetlib test files that need a live *Library.
func newLibrary(t *testing.T) *Library {
	t.Helper()
	lib, err := New()
	require.NoError(t, err, "embedded catalog must parse")
	return lib
}

// TestCatalogIntegrity guards the embedded catalog.yaml: New must parse it (every
// matcher type known, every regex compiling, unique rule ids), and it must expose
// a version. A bad edit fails here in CI rather than silently in production.
func TestCatalogIntegrity(t *testing.T) {
	t.Parallel()
	lib := newLibrary(t)
	if lib.Version() == "" {
		t.Fatal("Version() is empty")
	}
}

// TestReasonSmoke is a minimal end-to-end check across a few matcher types.
// Exhaustive per-matcher and per-entry coverage lives in catalog_test.go
// (engine) and catalog_data_test.go (catalog entries).
func TestReasonSmoke(t *testing.T) {
	t.Parallel()
	lib := newLibrary(t)
	tests := []struct {
		name    string
		m       Match
		wantHit bool
	}{
		{"test visa card on credit_card rule", Match{Source: "presidio", RuleID: "pii.credit_card", Value: "4111 1111 1111 1111"}, true},
		{"real-looking value on credit_card rule", Match{Source: "presidio", RuleID: "pii.credit_card", Value: "hello world"}, false},
		{"stripe test key from gitleaks", Match{Source: "gitleaks", RuleID: "secret.stripe_access_token", Value: "sk_test_abc123"}, true},
		{"stripe live key not suppressed", Match{Source: "gitleaks", RuleID: "secret.stripe_access_token", Value: "sk_live_abc123"}, false},
		{"wrong source does not fire", Match{Source: "custom", RuleID: "secret.stripe_access_token", Value: "sk_test_abc123"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := lib.Reason(tt.m)
			if tt.wantHit && got == "" {
				t.Errorf("Reason(%+v) = \"\", want non-empty", tt.m)
			}
			if !tt.wantHit && got != "" {
				t.Errorf("Reason(%+v) = %q, want \"\"", tt.m, got)
			}
		})
	}
}
