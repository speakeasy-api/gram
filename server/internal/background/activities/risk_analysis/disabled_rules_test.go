package risk_analysis

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDisabledRuleSet_Empty(t *testing.T) {
	t.Parallel()

	require.True(t, NewDisabledRuleSet(nil).Empty())
	require.True(t, NewDisabledRuleSet([]string{}).Empty())
	require.True(t, NewDisabledRuleSet([]string{""}).Empty())

	d := NewDisabledRuleSet([]string{"secret.aws_access_token"})
	require.False(t, d.Empty())
	require.True(t, d.Contains("secret.aws_access_token"))
	require.False(t, d.Contains("secret.github_pat"))
}

func TestDisabledRuleSet_FilterFindings(t *testing.T) {
	t.Parallel()

	in := []Finding{
		{RuleID: "secret.aws_access_token", Source: "gitleaks"},
		{RuleID: "secret.github_pat", Source: "gitleaks"},
		{RuleID: "pii.email_address", Source: "presidio"},
	}

	t.Run("empty set is passthrough", func(t *testing.T) {
		t.Parallel()
		d := NewDisabledRuleSet(nil)
		out := d.FilterFindings(in)
		require.Len(t, out, 3)
	})

	t.Run("drops disabled rules", func(t *testing.T) {
		t.Parallel()
		d := NewDisabledRuleSet([]string{"secret.aws_access_token", "pii.email_address"})
		out := d.FilterFindings(in)
		require.Len(t, out, 1)
		require.Equal(t, "secret.github_pat", out[0].RuleID)
	})

	t.Run("all disabled returns empty", func(t *testing.T) {
		t.Parallel()
		d := NewDisabledRuleSet([]string{
			"secret.aws_access_token", "secret.github_pat", "pii.email_address",
		})
		out := d.FilterFindings(in)
		require.Empty(t, out)
	})
}
