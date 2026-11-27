package platformmcp

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

// A disabled organization must lose Platform MCP access on the next request,
// including through connections and tokens issued while it was enabled.
func TestLiveOrgAdminAuthorizerDeniesDisabledOrganization(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_disabled_organization")
	require.NoError(t, err)

	principal, _ := seedRegistrationLifecycle(t, ctx, conn)
	require.NoError(t, authz.SeedSystemRoleGrants(ctx, conn, principal.OrganizationID))
	seedPlatformMCPAuthorizationMember(t, ctx, conn, principal.OrganizationID, principal.UserID, authz.SystemRoleAdmin)
	engine := authz.NewEngine(testenv.NewLogger(t), conn, func(context.Context, string) (bool, error) { return false, nil }, workos.NewStubClient())
	authorizer := NewLiveOrgAdminAuthorizer(conn, engine)
	selector := NewLiveOrganizationSelector(conn, authorizer)

	require.NoError(t, authorizer.RequireLiveMembership(ctx, principal))
	_, err = authorizer.PrepareExternalContext(ctx, principal)
	require.NoError(t, err)
	options, err := selector.EligibleOrganizations(ctx, principal.UserID)
	require.NoError(t, err)
	require.Len(t, options, 1)

	require.NoError(t, testrepo.New(conn).DisableDelegationOrganizationFixture(ctx, principal.OrganizationID))

	require.ErrorIs(t, authorizer.RequireLiveMembership(ctx, principal), ErrForbidden)
	_, err = authorizer.PrepareExternalContext(ctx, principal)
	require.ErrorIs(t, err, ErrForbidden)
	require.True(t, isAuthorizationDenied(err))
	options, err = selector.EligibleOrganizations(ctx, principal.UserID)
	require.NoError(t, err)
	require.Empty(t, options)
}
