package risk_analysis

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/risk/presetlib"
)

func newTestLibrary(t *testing.T) *presetlib.Library {
	t.Helper()
	lib, err := presetlib.New()
	require.NoError(t, err)
	return lib
}

func TestIsBuiltinFalsePositive(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		source string
		ruleID string
		match  string
		want   bool
	}{
		{name: "catalog credit card", source: "presidio", ruleID: "pii.credit_card", match: "4242 4242 4242 4242", want: true},
		{name: "stripe sandbox key", source: "gitleaks", ruleID: "secret.stripe_secret_key", match: "sk_test_4eC39HqLyjWDarjtT1zdp7dc", want: true},
		{name: "stripe live key", source: "gitleaks", ruleID: "secret.stripe_secret_key", match: "sk_live_4eC39HqLyjWDarjtT1zdp7dc", want: false},
	}
	lib := newTestLibrary(t)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, IsBuiltinFalsePositive(lib, tc.source, tc.ruleID, tc.match))
		})
	}
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
