package risk

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// White-box tests for the per-org-salted redaction helper. The integration
// tests in results_test.go can't express cross-org assertions because
// testenv.InitAuthContext pins every test instance to mockidp.MockOrgID, so
// we exercise the pure function directly here.

func TestRedactMatch_FingerprintDiffersAcrossOrgs(t *testing.T) {
	t.Parallel()

	secret := "sk-shared-across-orgs-7777"

	fpOrgA := redactMatch("gitleaks", &secret, "org-aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	fpOrgB := redactMatch("gitleaks", &secret, "org-bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb")

	require.NotEqual(t, fpOrgA, fpOrgB,
		"same secret in two different orgs must produce different fingerprints (org-salted sha256)")
	require.Regexp(t, `^<redacted len=26 sha=[0-9a-f]{8}>$`, fpOrgA)
	require.Regexp(t, `^<redacted len=26 sha=[0-9a-f]{8}>$`, fpOrgB)
}

func TestRedactMatch_FingerprintDeterministicWithinOrg(t *testing.T) {
	t.Parallel()

	secret := "sk-abc123def456"
	orgID := "org-cccccccc-cccc-cccc-cccc-cccccccccccc"

	fp1 := redactMatch("gitleaks", &secret, orgID)
	fp2 := redactMatch("gitleaks", &secret, orgID)

	require.Equal(t, fp1, fp2,
		"same secret in same org must produce identical fingerprints so agents can dedupe")
}

func TestRedactMatch_ShadowMCPIgnoresSalt(t *testing.T) {
	t.Parallel()

	const serverID = "mcp__evil-server__"
	match := serverID

	got := redactMatch("shadow_mcp", &match, "any-org-id")

	require.Equal(t, serverID, got,
		"shadow_mcp match should pass through verbatim regardless of orgID")
}

func TestRedactMatch_AccountIdentityIgnoresSalt(t *testing.T) {
	t.Parallel()

	const email = "jane@gmail.com"
	match := email

	got := redactMatch("account_identity", &match, "any-org-id")

	require.Equal(t, email, got,
		"account_identity match should pass through verbatim regardless of orgID")
}

func TestRedactMatch_EmptyMatchCollapses(t *testing.T) {
	t.Parallel()

	empty := ""
	require.Equal(t, "<redacted len=0>", redactMatch("gitleaks", nil, "org-x"))
	require.Equal(t, "<redacted len=0>", redactMatch("gitleaks", &empty, "org-x"))
}

// Guards against an "org_id || match" concatenation bug where an attacker
// could shift bytes between salt and payload. With a NUL separator,
// (orgID="ab", match="cd") and (orgID="a", match="bcd") must produce
// different fingerprints even though their concatenation is identical.
func TestRedactMatch_NULSeparatorPreventsBoundaryAmbiguity(t *testing.T) {
	t.Parallel()

	left, right := "bcd", "cd"
	fpA := redactMatch("gitleaks", &left, "a")
	fpB := redactMatch("gitleaks", &right, "ab")

	require.NotEqual(t, fpA, fpB,
		"shifting bytes from match into orgID must change the fingerprint (NUL separator boundary)")
}
