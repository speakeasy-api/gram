package email

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOpenRouterCreditsThreshold_TransactionalID(t *testing.T) {
	t.Parallel()

	require.Equal(t, transactionalIDOpenRouterCreditsThreshold, OpenRouterCreditsThreshold{}.TransactionalID())
}

func TestOpenRouterCreditsThreshold_Variables_RendersExpectedKeys(t *testing.T) {
	t.Parallel()

	tmpl := OpenRouterCreditsThreshold{
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

func TestOpenRouterCreditsThreshold_Variables_ExhaustedRendersTrue(t *testing.T) {
	t.Parallel()

	vars := OpenRouterCreditsThreshold{
		OrganizationName: "Acme Inc",
		ThresholdPercent: "100",
		Exhausted:        true,
	}.Variables()

	require.Equal(t, "true", vars["exhausted"])
}

func TestOpenRouterCreditsThreshold_Variables_PassesEmptyFieldsThrough(t *testing.T) {
	t.Parallel()

	vars := OpenRouterCreditsThreshold{}.Variables()
	require.Len(t, vars, 3, "all merge keys must be present even when empty")
	require.Equal(t, "false", vars["exhausted"])
}

func TestOpenRouterCreditsThreshold_AddToAudience(t *testing.T) {
	t.Parallel()

	require.False(t, OpenRouterCreditsThreshold{}.AddToAudience(),
		"billing alerts should not add recipients to the Loops audience")
}
