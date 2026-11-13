package mcp

import (
	"testing"

	"github.com/google/uuid"
	authclientsrepo "github.com/speakeasy-api/gram/server/internal/authclients/repo"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/authenforcement"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	productfeaturesrepo "github.com/speakeasy-api/gram/server/internal/productfeatures/repo"
)

func TestConsentApprovalEnforcementReadsPastWarmRuntimeCache(t *testing.T) {
	t.Parallel()
	pool, err := TestInfra.CloneTestDatabase(t, "mcp_consent_enforcement_fresh")
	require.NoError(t, err)
	for _, initiallyEnabled := range []bool{false, true} {
		name := "enable after cached off"
		if initiallyEnabled {
			name = "disable after cached on"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := newAuthClientFixture(t, pool)
			ctx := t.Context()
			queries := productfeaturesrepo.New(pool)
			setting := productfeaturesrepo.EnableFeatureParams{OrganizationID: f.orgID, FeatureName: string(productfeatures.FeatureUpstreamAuthEnforced)}
			if initiallyEnabled {
				_, err := queries.EnableFeature(ctx, setting)
				require.NoError(t, err)
			}
			flags := new(feature.InMemory)
			flags.SetFlag(feature.FlagAuthClientResolver, f.orgID, true)
			f.service.features = flags
			f.service.authEnforcement = authenforcement.NewGate(pool)
			cached, err := f.service.authEnforcement.Enforced(ctx, f.orgID)
			require.NoError(t, err)
			require.Equal(t, initiallyEnabled, cached)
			if initiallyEnabled {
				_, err = queries.DeleteFeature(ctx, productfeaturesrepo.DeleteFeatureParams(setting))
			} else {
				_, err = queries.EnableFeature(ctx, setting)
			}
			require.NoError(t, err)
			current, err := f.service.consentAuthChoicesEnforced(ctx, f.orgID)
			require.NoError(t, err)
			require.Equal(t, !initiallyEnabled, current, "approval uses current staff configuration, not the runtime cache or shadow flag")
			endpoint := &ResolvedMcpEndpoint{OrganizationID: f.orgID, ProjectID: f.projectID, McpServerID: validUUID(f.serverID)}
			current, err = f.service.consentDirectAuthChoicesEnforced(ctx, endpoint)
			require.NoError(t, err)
			require.Equal(t, !initiallyEnabled, current)
			cached, err = f.service.authEnforcement.Enforced(ctx, f.orgID)
			require.NoError(t, err)
			require.Equal(t, initiallyEnabled, cached, "the test did not reset or invalidate the warm runtime gate")
		})
	}
}

func TestGitHubProbeGuardKeepsResolvedTypedChoice(t *testing.T) {
	t.Parallel()
	pool, err := TestInfra.CloneTestDatabase(t, "mcp_github_probe_typed")
	require.NoError(t, err)
	f := newAuthClientFixture(t, pool)
	clientID := f.adminClient(authClientTypeServiceAccount, uuid.NullUUID{})
	require.NoError(t, authclientsrepo.New(pool).SetGitHubAppCredential(t.Context(), authclientsrepo.SetGitHubAppCredentialParams{
		AuthClientID: clientID, OrganizationID: f.orgID, InstanceUrl: "https://git.example.com", AppID: 123, InstallationID: 456,
		PrivateKeyEncrypted: "unused-test-key", UpstreamOrigin: "https://mcp.example.com",
	}))
	members := []metaMember{
		{serverID: f.serverID, backend: metaMemberBackendProxied, authClient: &authClientResolution{clientID: clientID, outcome: authClientResolved, githubApp: true}},
		{serverID: f.serverID, backend: metaMemberBackendProxied},
	}
	require.NoError(t, f.service.rejectLegacyGitHubMembers(t.Context(), f.orgID, members))
	require.NoError(t, members[0].upstreamUnavailable, "a resolved GitHub choice remains usable")
	require.Error(t, members[1].upstreamUnavailable, "only the unchosen legacy probe is unavailable")
}
