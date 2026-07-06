package risk_analysis

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDropBuiltinFalsePositives_DropsCatalogCreditCard(t *testing.T) {
	t.Parallel()

	findings := []Finding{
		{RuleID: "pii.credit_card", Source: "presidio", Match: "4242 4242 4242 4242"},
	}

	got := dropBuiltinFalsePositives(findings)

	require.Empty(t, got, "known test credit card should be suppressed as a catalog false positive")
}

func TestDropBuiltinFalsePositives_DropsStripeTestKey(t *testing.T) {
	t.Parallel()

	findings := []Finding{
		{RuleID: "secret.stripe_secret_key", Source: "gitleaks", Match: "sk_test_4eC39HqLyjWDarjtT1zdp7dc"},
	}

	got := dropBuiltinFalsePositives(findings)

	require.Empty(t, got, "stripe sandbox key should be suppressed as a catalog false positive")
}

func TestDropBuiltinFalsePositives_RetainsRealFinding(t *testing.T) {
	t.Parallel()

	realFinding := Finding{RuleID: "secret.stripe_secret_key", Source: "gitleaks", Match: "sk_live_4eC39HqLyjWDarjtT1zdp7dc"}
	findings := []Finding{realFinding}

	got := dropBuiltinFalsePositives(findings)

	require.Len(t, got, 1)
	require.Equal(t, realFinding, got[0])
}

func TestDropBuiltinFalsePositives_MixedBatch(t *testing.T) {
	t.Parallel()

	realFinding := Finding{RuleID: "secret.stripe_secret_key", Source: "gitleaks", Match: "sk_live_realsecretvalue"}
	findings := []Finding{
		{RuleID: "pii.credit_card", Source: "presidio", Match: "4242424242424242"},
		realFinding,
		{RuleID: "secret.stripe_secret_key", Source: "gitleaks", Match: "sk_test_4eC39HqLyjWDarjtT1zdp7dc"},
	}

	got := dropBuiltinFalsePositives(findings)

	require.Len(t, got, 1)
	require.Equal(t, realFinding, got[0])
}

func TestBuiltinPresetsEnabledFromConfig_DefaultsOnForEmpty(t *testing.T) {
	t.Parallel()

	require.True(t, BuiltinPresetsEnabledFromConfig(nil))
	require.True(t, BuiltinPresetsEnabledFromConfig([]byte{}))
}

func TestBuiltinPresetsEnabledFromConfig_DefaultsOnForEmptyObject(t *testing.T) {
	t.Parallel()

	require.True(t, BuiltinPresetsEnabledFromConfig([]byte(`{}`)))
	require.True(t, BuiltinPresetsEnabledFromConfig([]byte(`{"builtin_presets":{}}`)))
}

func TestBuiltinPresetsEnabledFromConfig_DisabledWhenExplicitlyFalse(t *testing.T) {
	t.Parallel()

	require.False(t, BuiltinPresetsEnabledFromConfig([]byte(`{"builtin_presets":{"enabled":false}}`)))
}

func TestBuiltinPresetsEnabledFromConfig_EnabledWhenExplicitlyTrue(t *testing.T) {
	t.Parallel()

	require.True(t, BuiltinPresetsEnabledFromConfig([]byte(`{"builtin_presets":{"enabled":true}}`)))
}
