package presidiofp

import (
	"net/netip"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNonPIIIPExactKeysAreCanonical locks the invariant that every key in
// nonPIIIPExact is already in netip canonical form. The exact lookup keys off
// addr.String(), so a non-canonical key would silently never match.
func TestNonPIIIPExactKeysAreCanonical(t *testing.T) {
	t.Parallel()

	for key := range nonPIIIPExact {
		addr, err := netip.ParseAddr(key)
		require.NoErrorf(t, err, "key %q must parse as an IP", key)
		assert.Equalf(t, key, addr.String(), "key %q must be in canonical netip form", key)
	}
}

// TestReason covers the entity-keyed dispatch: reserved/placeholder matches
// return a reason, real ones return "", and only the two catalogued entity
// types fire.
func TestReason(t *testing.T) {
	t.Parallel()

	assert.NotEmpty(t, Reason(EntityTypeIPAddress, "10.0.0.1"), "RFC1918 IP")
	assert.NotEmpty(t, Reason(EntityTypeIPAddress, "  ::  "), "trimmed unspecified IP")
	assert.NotEmpty(t, Reason(EntityTypeEmailAddress, "noreply@example.com"), "placeholder email")

	assert.Empty(t, Reason(EntityTypeIPAddress, "71.126.87.167"), "residential IP")
	assert.Empty(t, Reason(EntityTypeEmailAddress, "ada@speakeasy.com"), "real email")

	// Uncatalogued entity types never fire, even on a match another lane would
	// flag.
	assert.Empty(t, Reason("PERSON", "10.0.0.1"))
	assert.Empty(t, Reason("", "10.0.0.1"))
}

// TestReasonByRuleID covers the rule_id-keyed entry point used to re-evaluate
// stored findings, plus the rule_id<->entity grammar.
func TestReasonByRuleID(t *testing.T) {
	t.Parallel()

	assert.NotEmpty(t, ReasonByRuleID("pii.ip_address", "10.0.0.1"), "RFC1918 IP")
	assert.NotEmpty(t, ReasonByRuleID("pii.email_address", "noreply@example.com"), "placeholder email")

	assert.Empty(t, ReasonByRuleID("pii.ip_address", "71.126.87.167"), "residential IP")

	// Rule ids without a catalog never fire, even when the match would match
	// another lane's catalog.
	assert.Empty(t, ReasonByRuleID("pii.person", "10.0.0.1"))
	assert.Empty(t, ReasonByRuleID("secret.aws_access_key", "10.0.0.1"))
	assert.Empty(t, ReasonByRuleID("", "10.0.0.1"))

	// RuleIDs advertises exactly the catalogued rule ids, and the grammar is
	// invertible.
	assert.Equal(t, []string{"pii.ip_address", "pii.email_address"}, RuleIDs())
	assert.Equal(t, "IP_ADDRESS", entityTypeForRuleID("pii.ip_address"))
	assert.Equal(t, "EMAIL_ADDRESS", entityTypeForRuleID("pii.email_address"))
	assert.Empty(t, entityTypeForRuleID("secret.aws_access_key"))
}
