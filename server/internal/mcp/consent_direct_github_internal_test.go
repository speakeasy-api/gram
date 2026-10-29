package mcp

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	authclientsrepo "github.com/speakeasy-api/gram/server/internal/authclients/repo"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestConsentDirectGitHubUsesTypedBindingWithoutRollout(t *testing.T) {
	t.Parallel()
	pool, err := TestInfra.CloneTestDatabase(t, "mcp_consent_direct_github")
	require.NoError(t, err)
	f := newAuthClientFixture(t, pool)
	endpoint := &ResolvedMcpEndpoint{OrganizationID: f.orgID, ProjectID: f.projectID, McpServerID: validUUID(f.serverID)}
	ctx := t.Context()

	mode, err := f.service.authClientMode(ctx, f.orgID)
	require.NoError(t, err)
	require.Equal(t, feature.AuthClientResolverOff, mode)
	enforced, err := f.service.consentDirectAuthChoicesEnforced(ctx, endpoint)
	require.NoError(t, err)
	require.False(t, enforced, "ordinary servers retain legacy consent")
	require.NoError(t, f.service.validateDirectConsentInventory(ctx, endpoint, &consentToolInventory{}, nil))

	clientID := f.adminClient(authClientTypeServiceAccount, uuid.NullUUID{})
	queries := authclientsrepo.New(pool)
	require.NoError(t, queries.SetGitHubAppCredential(ctx, authclientsrepo.SetGitHubAppCredentialParams{
		AuthClientID: clientID, OrganizationID: f.orgID, InstanceUrl: "https://github.com",
		AppID: 123, InstallationID: 456, PrivateKeyEncrypted: "test-encrypted-key", UpstreamOrigin: "https://mcp.example.com",
	}))
	enforced, err = f.service.consentDirectAuthChoicesEnforced(ctx, endpoint)
	require.NoError(t, err)
	require.True(t, enforced, "GitHub has no legacy credential transport")
	organizationEnforced, err := f.service.consentAuthChoicesEnforced(ctx, f.orgID)
	require.NoError(t, err)
	require.False(t, organizationEnforced, "the server requirement does not change the rollout")

	draft := consentToolInventory{}
	require.NoError(t, bindDirectConsentInventory(&draft, &authClientResolution{clientID: clientID, githubApp: true, outcome: authClientResolved}))
	picks := map[uuid.UUID]uuid.UUID{f.serverID: clientID}
	require.NoError(t, f.service.validateDirectConsentInventory(ctx, endpoint, &draft, picks))
	require.ErrorIs(t, f.service.validateDirectConsentInventory(ctx, endpoint, &draft, nil), errConsentConfigurationChanged)
	require.ErrorIs(t, f.service.validateDirectConsentInventory(ctx, endpoint, &consentToolInventory{}, picks), errConsentConfigurationChanged)

	// Approval must enter the same typed resolver, not silently use legacy
	// service cards when mandatory authorization is unavailable.
	subject := urn.NewAnonymousSubject("consent-test")
	_, err = f.service.directConsentAuthChoice(ctx, endpoint, AuthnChallengeState{Subject: &subject}, nil, uuid.Nil)
	require.ErrorContains(t, err, "GitHub App credentials require the authorization engine")

	_, err = queries.DetachAuthClientFromServer(ctx, authclientsrepo.DetachAuthClientFromServerParams{
		AuthClientID: clientID, McpServerID: f.serverID, OrganizationID: f.orgID,
	})
	require.NoError(t, err)
	enforced, err = f.service.consentDirectAuthChoicesEnforced(ctx, endpoint)
	require.NoError(t, err)
	require.True(t, enforced, "a historical GitHub attachment must not restore legacy credentials")
}

func TestConsentDirectOrdinaryServerKeepsEnforcedBinding(t *testing.T) {
	t.Parallel()
	pool, err := TestInfra.CloneTestDatabase(t, "mcp_consent_direct_enforced")
	require.NoError(t, err)
	f := newAuthClientFixture(t, pool)
	f.enforce()
	endpoint := &ResolvedMcpEndpoint{OrganizationID: f.orgID, ProjectID: f.projectID, McpServerID: validUUID(f.serverID)}
	enforced, err := f.service.consentDirectAuthChoicesEnforced(t.Context(), endpoint)
	require.NoError(t, err)
	require.True(t, enforced)
	draft := consentToolInventory{}
	require.NoError(t, bindDirectConsentInventory(&draft, &authClientResolution{outcome: authClientNoAuth}))
	require.NoError(t, f.service.validateDirectConsentInventory(t.Context(), endpoint, &draft, nil))
}
