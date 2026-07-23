package gitleaks_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/scanners"
	"github.com/speakeasy-api/gram/server/internal/scanners/gitleaks"
)

// Synthetic, non-real credential values. They avoid sequential runs
// (0123456789) and dictionary words so gitleaks' default stopword allowlist
// does not treat them as placeholders.
const (
	fakeAccessKeyID = "ASIAZ2XY3WNBQR5TUVWX" // ASIA prefix + 16 base32 chars
	fakeSecret      = "wJalrXUtnFEMIbKp7MDoRZfiCYqTvHgNsQ8xLcWd"
	fakeToken       = "9x34WiYVTCAnxboiPBmqNrCFKD/3tyqFMmVo8mc0cNlyFFd93N5Z/o8gIIyX0hOWxvWRp/dO9Smp" +
		"CVXShsKReY3ef4sMrpkDz2IbZR9eAQyIioVhzt6O9p894tYWerLw9pGMezSLJLyNJ4DQO/A2CU2e8QsY1Buamkhl"
	// sha1Empty is the SHA-1 of the empty string: 40 lowercase hex chars. The
	// canonical false positive a bare 40-char rule would flag.
	sha1Empty = "da39a3ee5e6b4b0d3255bfef95601890afd80709"
)

func ruleSet(t *testing.T, content string) map[string]bool {
	t.Helper()
	findings, err := gitleaks.NewScanner().Scan(context.Background(), content)
	require.NoError(t, err)
	out := map[string]bool{}
	for _, f := range findings {
		out[f.RuleID] = true
	}
	return out
}

// The extended config must detect all three AWS credential flavors — the id
// (built-in) plus the secret access key and session token (our added rules).
func TestExtendedConfig_DetectsAllThreeFlavors(t *testing.T) {
	t.Parallel()

	content := `{
  "AccessKeyId": "` + fakeAccessKeyID + `",
  "SecretAccessKey": "` + fakeSecret + `",
  "SessionToken": "` + fakeToken + `"
}`

	got := ruleSet(t, content)
	require.True(t, got[gitleaks.AccessKeyIDRuleID], "access key id (built-in) should be detected")
	require.True(t, got[gitleaks.SecretAccessKeyRuleID], "secret access key should be detected")
	require.True(t, got[gitleaks.SessionTokenRuleID], "session token should be detected")
}

// The secret access key must also be caught with no label, purely by proximity
// to the access key id — the native composite (RequiredRules) path.
func TestExtendedConfig_SecretAnchoredToIDWithoutLabel(t *testing.T) {
	t.Parallel()

	// Three space-separated values, as in `aws sts` table output: only the id
	// carries a recognizable shape; the secret has no label of its own.
	content := "creds  " + fakeAccessKeyID + "  " + fakeSecret + "\n"

	got := ruleSet(t, content)
	require.True(t, got[gitleaks.AccessKeyIDRuleID])
	require.True(t, got[gitleaks.SecretAccessKeyRuleID],
		"a bare secret within WithinLines of the id should be reported via the composite rule")
}

// A bare 40-char secret with NO anchor nearby must not be reported — the
// composite rule requires the access key id.
func TestExtendedConfig_BareSecretWithoutAnchorNotDetected(t *testing.T) {
	t.Parallel()

	got := ruleSet(t, "value = "+fakeSecret+"\n")
	require.False(t, got[gitleaks.SecretAccessKeyRuleID],
		"an unanchored, unlabeled base64 blob must not be flagged as a secret access key")
}

// A 40-char lowercase hex hash sitting next to a real access key id must not be
// reported: the composite rule's entropy floor rejects it.
func TestExtendedConfig_HashNextToIDNotDetected(t *testing.T) {
	t.Parallel()

	content := fakeAccessKeyID + " " + sha1Empty + "\n"
	got := ruleSet(t, content)
	require.False(t, got[gitleaks.SecretAccessKeyRuleID],
		"a lowercase-hex hash must fall below the entropy floor")
}

// The extended detector must construct without error — newDetector validates
// each AWS rule, so a malformed rule (bad SecretGroup, missing regex) surfaces
// here rather than as a rule that silently never matches.
func TestExtendedConfig_ConstructsCleanly(t *testing.T) {
	t.Parallel()
	require.NotPanics(t, func() {
		require.NoError(t, gitleaks.NewScanner().Prime())
	})
}

// The AWS rule ids the rest of the system keys on must be canonical.
func TestExtendedConfig_RuleIDsAreCanonical(t *testing.T) {
	t.Parallel()
	for _, id := range []string{
		gitleaks.AccessKeyIDRuleID,
		gitleaks.SecretAccessKeyRuleID,
		gitleaks.SessionTokenRuleID,
	} {
		require.NoError(t, scanners.ValidateRuleID(id), id)
	}
	// The id our added rules produce must match what CanonicalRuleID derives, so
	// the canonicalization and our constants can't drift.
	require.Equal(t, gitleaks.SecretAccessKeyRuleID, gitleaks.CanonicalRuleID("aws-secret-access-key"))
	require.Equal(t, gitleaks.SessionTokenRuleID, gitleaks.CanonicalRuleID("aws-session-token"))
	require.Equal(t, gitleaks.AccessKeyIDRuleID, gitleaks.CanonicalRuleID("aws-access-token"))
}
