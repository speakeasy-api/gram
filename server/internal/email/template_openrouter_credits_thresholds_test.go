package email

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOpenRouterChatCreditsThreshold_TransactionalID(t *testing.T) {
	t.Parallel()

	require.Equal(t, transactionalIDOpenRouterChatCreditsThreshold, OpenRouterChatCreditsThreshold{}.TransactionalID())
}

func TestOpenRouterInternalCreditsThreshold_TransactionalID(t *testing.T) {
	t.Parallel()

	require.Equal(t, transactionalIDOpenRouterInternalCreditsThreshold, OpenRouterInternalCreditsThreshold{}.TransactionalID())
}

func TestOpenRouterChatCreditsThreshold_Variables_RendersExpectedKeys(t *testing.T) {
	t.Parallel()

	tmpl := OpenRouterChatCreditsThreshold{
		OrganizationName: "Acme Inc",
		ThresholdPercent: "90",
		Exhausted:        false,
	}

	require.Equal(t, map[string]string{
		"organization_name": "Acme Inc",
		"threshold_percent": "90",
		"exhausted":         "false",
	}, tmpl.Variables())
}

func TestOpenRouterInternalCreditsThreshold_Variables_RendersExpectedKeys(t *testing.T) {
	t.Parallel()

	tmpl := OpenRouterInternalCreditsThreshold{
		OrganizationName: "Acme Inc",
		ThresholdPercent: "100",
		Exhausted:        true,
	}

	require.Equal(t, map[string]string{
		"organization_name": "Acme Inc",
		"threshold_percent": "100",
		"exhausted":         "true",
	}, tmpl.Variables())
}

func TestOpenRouterCreditsThresholds_VariablesPassEmptyFieldsThrough(t *testing.T) {
	t.Parallel()

	for _, tmpl := range []Template{OpenRouterChatCreditsThreshold{}, OpenRouterInternalCreditsThreshold{}} {
		vars := tmpl.Variables()
		require.Len(t, vars, 3, "%T: all merge keys must be present even when empty", tmpl)
		require.Equal(t, "false", vars["exhausted"], "%T", tmpl)
	}
}

func TestOpenRouterCreditsThresholds_AddToAudience(t *testing.T) {
	t.Parallel()

	require.False(t, OpenRouterChatCreditsThreshold{}.AddToAudience(),
		"billing alerts should not add recipients to the Loops audience")
	require.False(t, OpenRouterInternalCreditsThreshold{}.AddToAudience(),
		"billing alerts should not add recipients to the Loops audience")
}
