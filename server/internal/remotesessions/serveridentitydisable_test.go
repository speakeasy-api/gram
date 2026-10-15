// Turning User Identity off is scoped to one MCP server: the provider's client
// is a shared resource on a user session issuer many servers use, so it only
// goes when the server being disabled is the last one signing in through it.

package remotesessions_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/remote_sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
)

func disablePayload(mcpServerID uuid.UUID) *gen.DisableServerIdentityConfigurationPayload {
	return &gen.DisableServerIdentityConfigurationPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		McpServerID:      mcpServerID.String(),
	}
}

func TestDisableServerIdentityUnbindsTheLastServersClient(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	projectID := projectIDFromContext(t, ctx)
	targetID, userIssuerID := createServerIdentityTarget(t, ctx, ti, "disable-last-target")
	providerID := createServerIdentityProvider(t, ctx, ti, "disable-last-provider", "", false, []string{"none"})
	clientID := bindAndStampServerIdentityClient(t, ctx, ti, "disable-last-client", providerID, userIssuerID, targetID, false)

	result, err := ti.service.DisableServerIdentityConfiguration(ctx, disablePayload(targetID))
	require.NoError(t, err)
	require.Equal(t, providerID.String(), conv.PtrValOr(result.ProviderID, ""))
	require.True(t, result.ClientUnbound, "the last server using it takes the client with it")

	require.False(t, storedIssuer(t, ctx, ti.conn, projectID, targetID).Valid)
	requireClientBound(t, ctx, ti, clientID, userIssuerID, false)
}

// AIM-577: the cleared state is the configuration, so nothing may put a
// provider back. A lone server on an issuer binding two providers used to have
// the second one derived onto it by the post-commit pass, which reported a
// successful disable and left the server signing people in to a provider
// nobody chose.
func TestDisableServerIdentityStaysDisabledWithAnotherProviderBound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	projectID := projectIDFromContext(t, ctx)
	orgID := activeOrganizationID(t, ctx)
	targetID, userIssuerID := createServerIdentityTarget(t, ctx, ti, "disable-sole-target")

	providerID := createServerIdentityProvider(t, ctx, ti, "disable-sole-provider", "", false, []string{"none"})
	bindAndStampServerIdentityClient(t, ctx, ti, "disable-sole-client", providerID, userIssuerID, targetID, false)
	// A second provider's client on the same issuer, with this server the only
	// one on it.
	otherProviderID := createServerIdentityProvider(t, ctx, ti, "disable-sole-other", "", false, []string{"none"})
	otherClientID := bindServerIdentityClient(t, ctx, ti, "disable-sole-other-client", otherProviderID, userIssuerID, false)

	result, err := ti.service.DisableServerIdentityConfiguration(ctx, disablePayload(targetID))
	require.NoError(t, err)
	require.Equal(t, providerID.String(), conv.PtrValOr(result.ProviderID, ""))
	require.False(t, storedIssuer(t, ctx, ti.conn, projectID, targetID).Valid,
		"the disabled server must not be handed the other bound provider")

	// And it stays disabled through the pass every client-binding write runs,
	// which only ever takes a provider away.
	require.NoError(t, remotesessions.ClearUnboundMCPServerRemoteSessionIssuers(ctx, ti.conn, orgID, projectID, []uuid.UUID{userIssuerID}))
	require.False(t, storedIssuer(t, ctx, ti.conn, projectID, targetID).Valid,
		"no pass may configure a disabled server from whatever else its issuer binds")
	requireClientBound(t, ctx, ti, otherClientID, userIssuerID, true)
}

func TestDisableServerIdentityKeepsAClientASiblingUses(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	projectID := projectIDFromContext(t, ctx)
	userIssuerID := seedOrganizationTierUserSessionIssuer(t, ctx, ti.conn, "disable-shared-issuer")
	targetID := createServerIdentityTargetOnIssuer(t, ctx, ti, "disable-shared-target", userIssuerID)
	siblingID := createServerIdentityTargetOnIssuer(t, ctx, ti, "disable-shared-sibling", userIssuerID)

	// Both servers sign in to one provider through its one client. Unbinding
	// it would strand the sibling, and nothing puts a shared server's provider
	// back, so the client stays while only this server is cleared.
	providerID := createServerIdentityProvider(t, ctx, ti, "disable-shared-provider", "", false, []string{"none"})
	clientID := bindAndStampServerIdentityClient(t, ctx, ti, "disable-shared-client", providerID, userIssuerID, targetID, false)
	stampIssuer(t, ctx, ti.conn, projectID, siblingID, providerID)

	result, err := ti.service.DisableServerIdentityConfiguration(ctx, disablePayload(targetID))
	require.NoError(t, err)
	require.Equal(t, providerID.String(), conv.PtrValOr(result.ProviderID, ""))
	require.False(t, result.ClientUnbound, "the sibling still signs in through it")

	require.False(t, storedIssuer(t, ctx, ti.conn, projectID, targetID).Valid)
	require.Equal(t, conv.ToNullUUID(providerID), storedIssuer(t, ctx, ti.conn, projectID, siblingID))
	requireClientBound(t, ctx, ti, clientID, userIssuerID, true)
}

func TestDisableServerIdentityLeavesOtherProvidersAlone(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	projectID := projectIDFromContext(t, ctx)
	userIssuerID := seedOrganizationTierUserSessionIssuer(t, ctx, ti.conn, "disable-other-issuer")
	targetID := createServerIdentityTargetOnIssuer(t, ctx, ti, "disable-other-target", userIssuerID)
	siblingID := createServerIdentityTargetOnIssuer(t, ctx, ti, "disable-other-sibling", userIssuerID)

	targetProviderID := createServerIdentityProvider(t, ctx, ti, "disable-other-target-provider", "", false, []string{"none"})
	targetClientID := bindAndStampServerIdentityClient(t, ctx, ti, "disable-other-target-client", targetProviderID, userIssuerID, targetID, false)
	siblingProviderID := createServerIdentityProvider(t, ctx, ti, "disable-other-sibling-provider", "", false, []string{"none"})
	siblingClientID := bindAndStampServerIdentityClient(t, ctx, ti, "disable-other-sibling-client", siblingProviderID, userIssuerID, siblingID, false)

	result, err := ti.service.DisableServerIdentityConfiguration(ctx, disablePayload(targetID))
	require.NoError(t, err)
	require.True(t, result.ClientUnbound)

	// Only this server's own provider is touched. Unbinding every client the
	// project can see would have signed the sibling out and cleared it too.
	requireClientBound(t, ctx, ti, targetClientID, userIssuerID, false)
	requireClientBound(t, ctx, ti, siblingClientID, userIssuerID, true)
	require.Equal(t, conv.ToNullUUID(siblingProviderID), storedIssuer(t, ctx, ti.conn, projectID, siblingID))
}

func TestDisableServerIdentityIsIdempotentAndScoped(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	projectID := projectIDFromContext(t, ctx)
	targetID, userIssuerID := createServerIdentityTarget(t, ctx, ti, "disable-idem-target")
	providerID := createServerIdentityProvider(t, ctx, ti, "disable-idem-provider", "", false, []string{"none"})
	bindAndStampServerIdentityClient(t, ctx, ti, "disable-idem-client", providerID, userIssuerID, targetID, false)

	_, err := ti.service.DisableServerIdentityConfiguration(ctx, disablePayload(targetID))
	require.NoError(t, err)

	// A server already signing in to nothing is left alone rather than refused.
	again, err := ti.service.DisableServerIdentityConfiguration(ctx, disablePayload(targetID))
	require.NoError(t, err)
	require.Nil(t, again.ProviderID)
	require.False(t, again.ClientUnbound)
	require.False(t, storedIssuer(t, ctx, ti.conn, projectID, targetID).Valid)

	readOnlyCtx := withExactAccessGrants(t, ctx, ti.conn, authz.NewGrant(authz.ScopeMCPRead, targetID.String()))
	_, err = ti.service.DisableServerIdentityConfiguration(readOnlyCtx, disablePayload(targetID))
	requireOopsCode(t, err, oops.CodeForbidden)

	otherProjectID := createProject(t, ctx, ti.conn, "disable-idem-other")
	foreignIssuerID := createUserSessionIssuerInProject(t, ctx, ti.conn, otherProjectID, "disable-idem-foreign")
	foreignServerID := createImpactServer(t, ctx, ti, otherProjectID, "disable-idem-foreign-server", foreignIssuerID)
	_, err = ti.service.DisableServerIdentityConfiguration(ctx, disablePayload(foreignServerID))
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestDisableServerIdentityRefusesUnbindingAnOrgClientOnAnOrgIssuer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	projectID := projectIDFromContext(t, ctx)
	userIssuerID := seedOrganizationTierUserSessionIssuer(t, ctx, ti.conn, "disable-org-issuer")
	targetID := createServerIdentityTargetOnIssuer(t, ctx, ti, "disable-org-target", userIssuerID)
	providerID := createServerIdentityProvider(t, ctx, ti, "disable-org-provider", "", false, []string{"none"})
	orgClientID := bindAndStampServerIdentityClient(t, ctx, ti, "disable-org-client", providerID, userIssuerID, targetID, true)

	// An organization-owned client on an organization-level issuer is every
	// project's binding, so it is changed from the organization's settings.
	_, err := ti.service.DisableServerIdentityConfiguration(ctx, disablePayload(targetID))
	requireOopsCode(t, err, oops.CodeConflict)
	requireClientBound(t, ctx, ti, orgClientID, userIssuerID, true)
	require.Equal(t, conv.ToNullUUID(providerID), storedIssuer(t, ctx, ti.conn, projectID, targetID),
		"a refused disable must leave the server configured")
}
