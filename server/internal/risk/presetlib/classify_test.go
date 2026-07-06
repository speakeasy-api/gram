package presetlib

import "testing"

// TestCatalogIntegrity guards the embedded catalog.yaml: it must parse, every
// matcher type must be known, every regex must compile, and rule ids must be
// unique. A bad edit fails here in CI rather than silently in production.
func TestCatalogIntegrity(t *testing.T) {
	t.Parallel()
	if err := Validate(); err != nil {
		t.Fatalf("embedded catalog is invalid: %v", err)
	}
	if Version() == "" {
		t.Fatal("Version() is empty")
	}
}

// TestReasonSmoke is a minimal end-to-end check across a few matcher types.
// Exhaustive per-matcher and per-entry coverage lives in catalog_test.go
// (engine) and catalog_data_test.go (catalog entries).
func TestReasonSmoke(t *testing.T) {
	t.Parallel()
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
			got := Reason(tt.m)
			if tt.wantHit && got == "" {
				t.Errorf("Reason(%+v) = \"\", want non-empty", tt.m)
			}
			if !tt.wantHit && got != "" {
				t.Errorf("Reason(%+v) = %q, want \"\"", tt.m, got)
			}
		})
	}
}
