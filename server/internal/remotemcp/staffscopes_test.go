package remotemcp_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotemcp"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	unproxiedrepo "github.com/speakeasy-api/gram/server/internal/unproxiedmcp/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestStaffScopes_DescribeResolvesAsIfDiscoveryWereOn(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	enableDiscovery(t, ctx, ti, false)
	srv := seedScopeServer(t, ctx, ti, "https://staff-describe.example.com/mcp")
	issuer := seedScopeIssuer(t, ctx, ti, []string{"iss:read"}, nil)
	owner := seedScopeClient(t, ctx, ti, issuer, nil, srv.userSessionIssuerID)
	seedPin(t, ctx, ti, srv.url, "read")
	authCtx, _ := contextvalues.GetAuthContext(ctx)

	staff := remotemcp.NewStaffScopes(testenv.NewLogger(t), ti.conn, audit.NewLogger())
	got, err := staff.Describe(ctx, remotemcp.StaffScopeTarget{OrganizationID: authCtx.ActiveOrganizationID, ProjectID: *authCtx.ProjectID, McpServerID: uuid.MustParse(srv.mcpServerID)})
	require.NoError(t, err)
	require.Equal(t, srv.url, got.ResourceURL)
	require.Equal(t, []string{"read"}, got.PinnedScopes)
	c := clientEntry(t, got, owner)
	require.Equal(t, "resource_pin", c.ScopeSource, "the staff view ignores the organization's rollout flag")
	require.True(t, c.PinWouldDecide)
}

func TestStaffScopes_DescribeReportsServerWithoutRemoteBackend(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	backend, err := unproxiedrepo.New(ti.conn).CreateServer(ctx, unproxiedrepo.CreateServerParams{
		ID:          uuid.Must(uuid.NewV7()),
		ProjectID:   *authCtx.ProjectID,
		Name:        conv.ToPGText("Unproxied"),
		Slug:        conv.ToPGText("unproxied-" + uuid.NewString()[:8]),
		Url:         "https://staff-unproxied.example.com/mcp",
		Description: pgtype.Text{},
	})
	require.NoError(t, err)
	server, err := mcpserversrepo.New(ti.conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID:                   uuid.Must(uuid.NewV7()),
		ProjectID:            *authCtx.ProjectID,
		Name:                 conv.ToPGText("Unproxied"),
		Slug:                 conv.ToPGText("unproxied-" + uuid.NewString()[:8]),
		UnproxiedMcpServerID: conv.ToNullUUID(backend.ID),
		Visibility:           "private",
	})
	require.NoError(t, err)

	staff := remotemcp.NewStaffScopes(testenv.NewLogger(t), ti.conn, audit.NewLogger())
	_, err = staff.Describe(ctx, remotemcp.StaffScopeTarget{OrganizationID: authCtx.ActiveOrganizationID, ProjectID: *authCtx.ProjectID, McpServerID: server.ID})
	require.ErrorIs(t, err, remotemcp.ErrNotRemoteBacked)
}

func TestStaffScopes_SetPinIgnoresFlagAndAuditsTheStaffActor(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	enableDiscovery(t, ctx, ti, false)
	srv := seedScopeServer(t, ctx, ti, "https://staff-pin.example.com/mcp")
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	target := remotemcp.StaffScopeTarget{OrganizationID: authCtx.ActiveOrganizationID, ProjectID: *authCtx.ProjectID, McpServerID: uuid.MustParse(srv.mcpServerID)}

	staff := remotemcp.NewStaffScopes(testenv.NewLogger(t), ti.conn, audit.NewLogger())
	got, err := staff.SetPin(ctx, remotemcp.StaffScopePin{
		Target:           target,
		Scopes:           []string{" read ", "read", "write"},
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, "staff-subject"),
		ActorDisplayName: new("Staff Member"),
	})
	require.NoError(t, err)
	require.Equal(t, []string{"read", "write"}, got.PinnedScopes)
	require.Equal(t, []string{"read", "write"}, storedPin(t, ctx, ti, srv.url))

	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionMcpServerScopePinUpdate)
	require.NoError(t, err)
	require.Equal(t, "staff-subject", record.ActorID)
	require.Equal(t, srv.mcpServerID, record.SubjectID)

	got, err = staff.SetPin(ctx, remotemcp.StaffScopePin{Target: target, Scopes: []string{}, Actor: urn.NewPrincipal(urn.PrincipalTypeUser, "staff-subject"), ActorDisplayName: nil})
	require.NoError(t, err)
	require.Empty(t, got.PinnedScopes)
	require.Nil(t, storedPin(t, ctx, ti, srv.url), "an empty list stores NULL")
	count, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionMcpServerScopePinUpdate)
	require.NoError(t, err)
	require.EqualValues(t, 2, count, "the clear is audited too")
}

func TestStaffScopes_SetPinRejectsInvalidScopes(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	srv := seedScopeServer(t, ctx, ti, "https://staff-invalid.example.com/mcp")
	authCtx, _ := contextvalues.GetAuthContext(ctx)

	staff := remotemcp.NewStaffScopes(testenv.NewLogger(t), ti.conn, audit.NewLogger())
	_, err := staff.SetPin(ctx, remotemcp.StaffScopePin{
		Target:           remotemcp.StaffScopeTarget{OrganizationID: authCtx.ActiveOrganizationID, ProjectID: *authCtx.ProjectID, McpServerID: uuid.MustParse(srv.mcpServerID)},
		Scopes:           []string{`bad"scope`},
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, "staff-subject"),
		ActorDisplayName: nil,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
}
