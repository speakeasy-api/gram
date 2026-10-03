package plugins

import (
	"fmt"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// eligibilityService builds a service carrying only the dependencies
// hooksRolloutEligible reads.
func eligibilityService(t *testing.T, features feature.Provider) *Service {
	t.Helper()
	return &Service{logger: testenv.NewLogger(t), features: features}
}

func TestHooksRolloutEligible_CanaryBypassesProvider(t *testing.T) {
	t.Parallel()

	// An empty provider proves the canary decision never consults PostHog — a
	// canary org is eligible even during a provider outage.
	svc := eligibilityService(t, &feature.InMemory{})
	for slug := range canaryHooksOrgSlugs {
		require.True(t, svc.hooksRolloutEligible(t.Context(), "org-any", slug), "canary slug %q must be eligible", slug)
	}
}

func TestHooksRolloutEligible_PinAtOrAboveCurrentIsEligible(t *testing.T) {
	t.Parallel()

	current, err := strconv.Atoi(hooksGeneratorVersion)
	require.NoError(t, err)

	for _, pin := range []int{current, current + 1, current + 100} {
		features := &feature.InMemory{}
		features.SetFlagPayload(feature.FlagHooksRollout, "org-1", fmt.Appendf(nil, `{"version": %d}`, pin))
		svc := eligibilityService(t, features)
		require.True(t, svc.hooksRolloutEligible(t.Context(), "org-1", "not-canary"), "pin %d >= current %d must be eligible", pin, current)
	}
}

func TestHooksRolloutEligible_PinBelowCurrentIsNotEligible(t *testing.T) {
	t.Parallel()

	current, err := strconv.Atoi(hooksGeneratorVersion)
	require.NoError(t, err)

	features := &feature.InMemory{}
	features.SetFlagPayload(feature.FlagHooksRollout, "org-1", fmt.Appendf(nil, `{"version": %d}`, current-1))
	svc := eligibilityService(t, features)
	require.False(t, svc.hooksRolloutEligible(t.Context(), "org-1", "not-canary"))
}

func TestHooksRolloutEligible_MissingOrMalformedPayloadFailsClosed(t *testing.T) {
	t.Parallel()

	// No payload set for this org at all.
	svc := eligibilityService(t, &feature.InMemory{})
	require.False(t, svc.hooksRolloutEligible(t.Context(), "org-nopayload", "not-canary"))

	// Payload present but not the expected shape.
	malformed := &feature.InMemory{}
	malformed.SetFlagPayload(feature.FlagHooksRollout, "org-bad", []byte(`not json`))
	svc = eligibilityService(t, malformed)
	require.False(t, svc.hooksRolloutEligible(t.Context(), "org-bad", "not-canary"))
}
