package platformmcp

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	adminrepo "github.com/speakeasy-api/gram/server/internal/admin/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	organizationsrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

// seedOrganizationWithMember creates an enabled organization with one active
// member and returns that member's Platform MCP principal.
func seedOrganizationWithMember(t *testing.T, ctx context.Context, conn *pgxpool.Pool) Principal {
	t.Helper()

	organizationID := "org_" + uuid.NewString()
	_, err := organizationsrepo.New(conn).UpsertOrganizationMetadata(ctx, organizationsrepo.UpsertOrganizationMetadataParams{
		ID:          organizationID,
		Name:        "Platform MCP disabled organization test",
		Slug:        "org-" + uuid.NewString()[:8],
		WorkosID:    pgtype.Text{},
		Whitelisted: pgtype.Bool{},
	})
	require.NoError(t, err)
	require.NoError(t, authz.SeedSystemRoleGrants(ctx, conn, organizationID))

	userID := "user_" + uuid.NewString()
	seedPlatformMCPAuthorizationMember(t, ctx, conn, organizationID, userID, authz.SystemRoleAdmin)
	return Principal{Surface: SurfacePlatformMCP, UserID: userID, OrganizationID: organizationID, ConnectionID: "connection-1", Generation: "generation-1", ClientID: "client-1"}
}

func newOrganizationDisabledTestAuthorizer(t *testing.T, conn *pgxpool.Pool) *LiveOrgAdminAuthorizer {
	t.Helper()
	engine := authz.NewEngine(testenv.NewLogger(t), conn, func(context.Context, string) (bool, error) { return false, nil }, workos.NewStubClient())
	return NewLiveOrgAdminAuthorizer(conn, engine)
}

// disableTestOrganization disables an organization through the operator path.
func disableTestOrganization(t *testing.T, ctx context.Context, conn *pgxpool.Pool, organizationID string) {
	t.Helper()
	rows, err := adminrepo.New(conn).AdminDisableOrganization(ctx, organizationID)
	require.NoError(t, err)
	require.Equal(t, int64(1), rows)
}

func TestLiveOrgAdminAuthorizerAdmitsMemberOfEnabledOrganization(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_enabled_organization_admitted")
	require.NoError(t, err)
	principal := seedOrganizationWithMember(t, ctx, conn)
	authorizer := newOrganizationDisabledTestAuthorizer(t, conn)

	require.NoError(t, authorizer.RequireLiveMembership(ctx, principal))
	_, err = authorizer.PrepareExternalContext(ctx, principal)
	require.NoError(t, err)
}

// Disabling an organization must deny its very next Platform MCP request,
// including from a member whose OAuth session is still valid.
func TestLiveOrgAdminAuthorizerDeniesMemberOfDisabledOrganization(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_disabled_organization_denied")
	require.NoError(t, err)
	principal := seedOrganizationWithMember(t, ctx, conn)
	authorizer := newOrganizationDisabledTestAuthorizer(t, conn)
	require.NoError(t, authorizer.RequireLiveMembership(ctx, principal))

	disableTestOrganization(t, ctx, conn, principal.OrganizationID)

	require.ErrorIs(t, authorizer.RequireLiveMembership(ctx, principal), ErrOrganizationDisabled)
	_, err = authorizer.PrepareExternalContext(ctx, principal)
	require.ErrorIs(t, err, ErrOrganizationDisabled)
	// Generic denial handling must still refuse the request.
	require.ErrorIs(t, err, ErrForbidden)
	require.True(t, isAuthorizationDenied(err))
	require.ErrorIs(t, authorizer.RequireLiveOrgAdmin(ctx, principal), ErrOrganizationDisabled)
	allowed, err := authorizer.HasLiveOrgAdmin(ctx, principal)
	require.ErrorIs(t, err, ErrOrganizationDisabled)
	require.False(t, allowed)
}

// Disabling is reversible: re-enabling the organization admits the same
// member again without any new authorization.
func TestLiveOrgAdminAuthorizerAdmitsMemberAfterOrganizationIsReenabled(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_reenabled_organization_admitted")
	require.NoError(t, err)
	principal := seedOrganizationWithMember(t, ctx, conn)
	authorizer := newOrganizationDisabledTestAuthorizer(t, conn)
	disableTestOrganization(t, ctx, conn, principal.OrganizationID)
	require.ErrorIs(t, authorizer.RequireLiveMembership(ctx, principal), ErrOrganizationDisabled)

	rows, err := adminrepo.New(conn).AdminEnableOrganization(ctx, principal.OrganizationID)
	require.NoError(t, err)
	require.Equal(t, int64(1), rows)

	require.NoError(t, authorizer.RequireLiveMembership(ctx, principal))
	_, err = authorizer.PrepareExternalContext(ctx, principal)
	require.NoError(t, err)
}

// A user who is not a member is denied as lost membership even when the
// organization is also disabled, so their connection still ends durably.
func TestLiveOrgAdminAuthorizerDeniesNonMemberOfDisabledOrganizationAsLostMembership(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_disabled_organization_non_member")
	require.NoError(t, err)
	principal := seedOrganizationWithMember(t, ctx, conn)
	authorizer := newOrganizationDisabledTestAuthorizer(t, conn)
	disableTestOrganization(t, ctx, conn, principal.OrganizationID)

	outsider := principal
	outsider.UserID = "user_" + uuid.NewString()
	err = authorizer.RequireLiveMembership(ctx, outsider)

	require.ErrorIs(t, err, ErrForbidden)
	require.NotErrorIs(t, err, ErrOrganizationDisabled)
}

// The organization picker offers only organizations a connection could use.
func TestLiveOrganizationSelectorOmitsDisabledOrganization(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_selector_omits_disabled_organization")
	require.NoError(t, err)
	principal := seedOrganizationWithMember(t, ctx, conn)
	selector := NewLiveOrganizationSelector(conn, newOrganizationDisabledTestAuthorizer(t, conn))

	before, err := selector.EligibleOrganizations(ctx, principal.UserID)
	require.NoError(t, err)
	require.Len(t, before, 1)
	require.Equal(t, principal.OrganizationID, before[0].ID)

	disableTestOrganization(t, ctx, conn, principal.OrganizationID)

	after, err := selector.EligibleOrganizations(ctx, principal.UserID)
	require.NoError(t, err)
	require.Empty(t, after)
}
