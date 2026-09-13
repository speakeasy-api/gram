package admission

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/feature"
)

func TestResolveRolloutRejectsDuplicateAndCaseVariantKeys(t *testing.T) {
	t.Parallel()

	for _, payload := range []string{
		`{"mode":"report","mode":"enforce"}`,
		`{"mode":"report","Mode":"enforce"}`,
		`{"Mode":"report"}`,
		`{"MODE":"report"}`,
		`{"extra":true,"mode":"report"}`,
		`{"mode":"report","extra":true}`,
		`{"mode":null}`,
		`{"mode":1}`,
		`[]`,
		`null`,
		`{"mode":"report"} {}`,
	} {
		provider := &feature.InMemory{}
		provider.SetFlag(feature.FlagPlatformMCPShadowAudienceEnforcement, testOrganizationID, true)
		provider.SetFlagPayload(feature.FlagPlatformMCPShadowAudienceEnforcement, testOrganizationID, []byte(payload))
		provider.SetFlag(feature.FlagPlatformMCPDirectRemoteDistributionDisabled, testOrganizationID, false)
		_, err := ResolveRollout(t.Context(), provider, testOrganizationID, testOrgSlug, testProjectSlug)
		var unavailable *RolloutUnavailableError
		require.ErrorAs(t, err, &unavailable, payload)
		require.Equal(t, feature.FlagPlatformMCPShadowAudienceEnforcement, unavailable.Flag, payload)
	}
}
