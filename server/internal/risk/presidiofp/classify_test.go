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
	assert.NotEmpty(t, Reason(EntityTypeUKNHS, "9434765919"), "NHS number outside the issued ranges")
	assert.NotEmpty(t, Reason(EntityTypeUSDriverLicense, "N1234567"), "retired recognizer")

	assert.Empty(t, Reason(EntityTypeIPAddress, "71.126.87.167"), "residential IP")
	assert.Empty(t, Reason(EntityTypeEmailAddress, "ada@speakeasy.com"), "real email")
	assert.Empty(t, Reason(EntityTypeUKNHS, "401 023 2137"), "issued NHS number, no context supplied")

	// Uncatalogued entity types never fire, even on a match another lane would
	// flag.
	assert.Empty(t, Reason("PERSON", "10.0.0.1"))
	assert.Empty(t, Reason("", "10.0.0.1"))
}

// TestReasonInContext covers the layer that needs the payload a match came
// from: an NHS-shaped run only survives when the text talks about health care,
// and supplying context never resurrects a value the value-only layer rejected.
func TestReasonInContext(t *testing.T) {
	t.Parallel()

	const nhs = "401 023 2137"

	assert.Empty(t, ReasonInContext(EntityTypeUKNHS, nhs, "Patient NHS number "+nhs),
		"issued number in health-care context")
	assert.NotEmpty(t, ReasonInContext(EntityTypeUKNHS, nhs, "invoice reference "+nhs),
		"issued number with no health-care signal")
	assert.Empty(t, ReasonInContext(EntityTypeUKNHS, nhs, ""),
		"empty text means context unknown, never suppress on it")

	// The value-only verdict still wins: context cannot rescue a run that was
	// never an NHS number.
	assert.NotEmpty(t, ReasonInContext(EntityTypeUKNHS, "9434765919", "NHS number 9434765919"),
		"unissued range, even in context")

	// Catalogs that ignore context behave identically either way.
	assert.NotEmpty(t, ReasonInContext(EntityTypeIPAddress, "10.0.0.1", "ping 10.0.0.1"))
	assert.Empty(t, ReasonInContext(EntityTypeEmailAddress, "ada@speakeasy.com", "mail ada@speakeasy.com"))
}

// TestReasonByRuleID covers the rule_id-keyed entry point used to re-evaluate
// stored findings, plus the rule_id<->entity grammar.
func TestReasonByRuleID(t *testing.T) {
	t.Parallel()

	assert.NotEmpty(t, ReasonByRuleID("pii.ip_address", "10.0.0.1"), "RFC1918 IP")
	assert.NotEmpty(t, ReasonByRuleID("pii.email_address", "noreply@example.com"), "placeholder email")
	assert.NotEmpty(t, ReasonByRuleID("pii.us_driver_license", "N1234567"), "retired recognizer")

	assert.Empty(t, ReasonByRuleID("pii.ip_address", "71.126.87.167"), "residential IP")

	// The context-keyed entry point is what a sweep uses once it has re-read the
	// message a stored finding was anchored to.
	assert.NotEmpty(t, ReasonByRuleIDInContext("pii.uk_nhs", "4010232137", "confluence page 4010232137"))
	assert.Empty(t, ReasonByRuleIDInContext("pii.uk_nhs", "4010232137", "NHS number 4010232137"))

	// Rule ids without a catalog never fire, even when the match would match
	// another lane's catalog.
	assert.Empty(t, ReasonByRuleID("pii.person", "10.0.0.1"))
	assert.Empty(t, ReasonByRuleID("secret.aws_access_key", "10.0.0.1"))
	assert.Empty(t, ReasonByRuleID("", "10.0.0.1"))

	// RuleIDs advertises exactly the catalogued rule ids, and the grammar is
	// invertible.
	assert.Equal(t, []string{
		"pii.ip_address",
		"pii.email_address",
		"pii.uk_nhs",
		"pii.us_driver_license",
	}, RuleIDs())
	assert.Equal(t, []string{"pii.uk_nhs"}, ContextRuleIDs())
	assert.Subset(t, RuleIDs(), ContextRuleIDs(), "a context rule must also be scanned for")
	assert.Equal(t, "IP_ADDRESS", entityTypeForRuleID("pii.ip_address"))
	assert.Equal(t, "EMAIL_ADDRESS", entityTypeForRuleID("pii.email_address"))
	assert.Empty(t, entityTypeForRuleID("secret.aws_access_key"))
}
