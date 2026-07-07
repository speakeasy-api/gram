package scanners

import (
	"fmt"
	"regexp"
	"testing"
)

// enforceRuleIDFormat is true when the binary is running under `go test`
// (covers unit + integration suites) or after cmd/ wiring opts in via
// EnableRuleIDFormatEnforcement (used in local-dev). In those modes the
// Describe* builders panic on a malformed canonical rule id so writer
// drift is caught immediately. Production runs leave the value through.
var enforceRuleIDFormat = testing.Testing()

// EnableRuleIDFormatEnforcement opts the process in to strict canonical
// rule_id validation: any Describe* builder returning a malformed id will
// panic. Intended for local development; cmd/ wires this on when
// `--environment=local`. Test binaries get the same behavior automatically
// via testing.Testing().
func EnableRuleIDFormatEnforcement() {
	enforceRuleIDFormat = true
}

// ruleIDFormat is the canonical rule id grammar shared across all scanners:
// lowercase ASCII letters and digits, underscores within a segment, dots
// between segments.
var ruleIDFormat = regexp.MustCompile(`^[a-z0-9]+(_[a-z0-9]+)*(\.[a-z0-9]+(_[a-z0-9]+)*)*$`)

// ValidateRuleID returns an error when id does not conform to the canonical
// rule id grammar (lowercase snake_case segments joined by dots). It is the
// single source of truth for the Finding.RuleID invariant; every scanner that
// produces a Finding validates its rule ids against it.
func ValidateRuleID(id string) error {
	if id == "" {
		return fmt.Errorf("rule id is empty")
	}
	if !ruleIDFormat.MatchString(id) {
		return fmt.Errorf("rule id %q is not in canonical form (lowercase snake_case segments joined by dots)", id)
	}
	return nil
}

// GuardRuleID validates the rule id against the canonical grammar and panics in
// dev/test if it doesn't match. Returns the id unchanged so callers can compose
// it inline.
func GuardRuleID(ruleID string) string {
	if !enforceRuleIDFormat {
		return ruleID
	}
	if err := ValidateRuleID(ruleID); err != nil {
		panic(fmt.Sprintf("risk scanners: invalid canonical rule id: %v", err))
	}
	return ruleID
}
