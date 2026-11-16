package usersessions_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	orggen "github.com/speakeasy-api/gram/server/gen/organization_user_session_issuers"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/usersessions/authserver"
)

// A pinned shared authorization server is re-pinned to the host it moves to,
// so its issuer URL follows, and moving back restores it.
func TestSetAuthenticationHostRepinsSharedAuthorizationServer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	issuerID := createOrganizationIssuer(t, ctx, ti, "pinned-move")
	serverIssuerURL := testServerURL + authserver.SharedPath(issuerID)
	authenticationHostIssuerURL := testAuthenticationHostURL + authserver.SharedPath(issuerID)
	setIssuerShared(t, ctx, ti, issuerID, serverIssuerURL)

	impact, err := ti.service.GetAuthenticationHostImpact(ctx, &orggen.GetAuthenticationHostImpactPayload{ID: issuerID.String(), UseAuthenticationHost: true})
	require.NoError(t, err)
	require.True(t, impact.Changes)
	require.Equal(t, serverIssuerURL, impact.AuthorizationServerBefore)
	require.Equal(t, authenticationHostIssuerURL, impact.AuthorizationServerAfter)

	moved, err := ti.service.SetAuthenticationHost(ctx, &orggen.SetAuthenticationHostPayload{ID: issuerID.String(), UseAuthenticationHost: true})
	require.NoError(t, err)
	require.True(t, moved.UseAuthenticationHost)
	require.True(t, moved.IssuerURLPinned)
	require.Equal(t, authenticationHostIssuerURL, *moved.IssuerURL)
	require.Equal(t, authenticationHostIssuerURL+"/token", *moved.TokenEndpoint)

	updateAudit, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionUserSessionIssuerUpdate)
	require.NoError(t, err)
	after, err := audittest.DecodeAuditData(updateAudit.AfterSnapshot)
	require.NoError(t, err)
	require.Equal(t, true, after["UseAuthenticationHost"])

	back, err := ti.service.SetAuthenticationHost(ctx, &orggen.SetAuthenticationHostPayload{ID: issuerID.String(), UseAuthenticationHost: false})
	require.NoError(t, err)
	require.False(t, back.UseAuthenticationHost)
	require.Equal(t, serverIssuerURL, *back.IssuerURL)
}

func TestSetAuthenticationHostMovesUnpinnedSharedAuthorizationServer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	issuerID := createOrganizationIssuer(t, ctx, ti, "unpinned-move")
	setIssuerShared(t, ctx, ti, issuerID, "")

	moved, err := ti.service.SetAuthenticationHost(ctx, &orggen.SetAuthenticationHostPayload{ID: issuerID.String(), UseAuthenticationHost: true})
	require.NoError(t, err)
	require.False(t, moved.IssuerURLPinned)
	require.Equal(t, testAuthenticationHostURL+authserver.SharedPath(issuerID), *moved.IssuerURL)
}

func TestGetAuthenticationHostImpactForPerServerAuthorizationServers(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	issuerID := seedIssuer(t, ctx, ti, "endpoint-move")
	attachIssuerToolset(t, ctx, ti, *authCtx.ProjectID, issuerID, "endpoint-move-toolset")

	impact, err := ti.service.GetAuthenticationHostImpact(ctx, &orggen.GetAuthenticationHostImpactPayload{ID: issuerID.String(), UseAuthenticationHost: true})
	require.NoError(t, err)
	require.Equal(t, testServerURL, impact.AuthorizationServerBefore)
	require.Equal(t, testAuthenticationHostURL, impact.AuthorizationServerAfter)
	require.True(t, impact.Changes)
	require.Equal(t, 1, impact.McpServerCount)
	require.NotEmpty(t, *impact.Issuer.ProjectName)

	moved, err := ti.service.SetAuthenticationHost(ctx, &orggen.SetAuthenticationHostPayload{ID: issuerID.String(), UseAuthenticationHost: true})
	require.NoError(t, err)
	require.True(t, moved.UseAuthenticationHost)
	require.Nil(t, moved.IssuerURL)

	impact, err = ti.service.GetAuthenticationHostImpact(ctx, &orggen.GetAuthenticationHostImpactPayload{ID: issuerID.String(), UseAuthenticationHost: true})
	require.NoError(t, err)
	require.False(t, impact.Changes, "it is already on the authentication host")
}

func TestSetAuthenticationHostRequiresAnAuthenticationHost(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestServiceWithHosts(t, authserver.Hosts{ServerURL: testServerURL, AuthenticationHostBaseURL: "", PlatformHosts: nil}, nil)
	issuerID := createOrganizationIssuer(t, ctx, ti, "no-authentication-host")

	issuer, err := ti.service.GetIssuer(ctx, &orggen.GetIssuerPayload{ID: issuerID.String()})
	require.NoError(t, err)
	require.False(t, *issuer.AuthenticationHostAvailable)

	impact, err := ti.service.GetAuthenticationHostImpact(ctx, &orggen.GetAuthenticationHostImpactPayload{ID: issuerID.String(), UseAuthenticationHost: true})
	require.NoError(t, err)
	require.False(t, impact.Changes)

	_, err = ti.service.SetAuthenticationHost(ctx, &orggen.SetAuthenticationHostPayload{ID: issuerID.String(), UseAuthenticationHost: true})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestSetAuthenticationHostRBAC(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	issuerID := createOrganizationIssuer(t, ctx, ti, "rbac-move")

	readCtx := withExactAuthzGrants(t, ctx, ti.conn, authz.NewGrant(authz.ScopeOrgRead, authCtx.ActiveOrganizationID))
	_, err := ti.service.GetAuthenticationHostImpact(readCtx, &orggen.GetAuthenticationHostImpactPayload{ID: issuerID.String(), UseAuthenticationHost: true})
	require.NoError(t, err)

	_, err = ti.service.SetAuthenticationHost(readCtx, &orggen.SetAuthenticationHostPayload{ID: issuerID.String(), UseAuthenticationHost: true})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestSetAuthenticationHostHidesOtherOrganizations(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	foreignID := seedOtherOrganizationIssuer(t, ctx, ti)

	_, err := ti.service.SetAuthenticationHost(ctx, &orggen.SetAuthenticationHostPayload{ID: foreignID.String(), UseAuthenticationHost: true})
	requireOopsCode(t, err, oops.CodeNotFound)

	_, err = ti.service.GetAuthenticationHostImpact(ctx, &orggen.GetAuthenticationHostImpactPayload{ID: foreignID.String(), UseAuthenticationHost: true})
	requireOopsCode(t, err, oops.CodeNotFound)
}
