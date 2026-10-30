package platformmcp

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	mcpserversgen "github.com/speakeasy-api/gram/server/gen/mcp_servers"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/networkaccess"
	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/toolsets"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type deleteMCPFixture struct {
	conn      *pgxpool.Pool
	principal Principal
	project   ResolvedProject
	service   *LifecycleDeleteService
	deletes   int
	failNext  bool
	// failAfter lets the next delete commit, then reports a failure, as when
	// the response is lost before the receipt is completed.
	failAfter bool
	// grants are what an administrator role carries: org:admin plus mcp:write
	// on each seeded server, which the dashboard delete checks itself.
	grants []authz.Grant
}

// newDeleteMCPFixture composes delete_mcp over the real dashboard delete, as
// server startup does, with a hosted server writer so toolset-hosted servers
// take the dashboard's turn-off path.
func newDeleteMCPFixture(t *testing.T, name string) (context.Context, *deleteMCPFixture) {
	t.Helper()
	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, name)
	require.NoError(t, err)
	principal, project := seedRegistrationLifecycle(t, ctx, conn)

	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)
	redisClient, err := platformMCPInfra.NewRedisClient(t, 0)
	require.NoError(t, err)
	sessionManager := testenv.NewTestManager(t, logger, tracerProvider, conn, redisClient, cache.Suffix("gram-local"), billing.NewStubClient(logger, tracerProvider))
	engine := authz.NewEngine(logger, conn, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient())
	toolsetService := toolsets.NewService(logger, tracerProvider, nil, conn, sessionManager, nil, engine, audit.NewLogger(), nil, false)
	serverService := mcpservers.NewService(logger, tracerProvider, conn, sessionManager, engine, audit.NewLogger(), nil, nil, false, nil, nil, networkaccess.DenyAllChecker{}).WithHostedServerWriter(toolsetService)

	fixture := &deleteMCPFixture{conn: conn, principal: principal, project: project, service: nil, deletes: 0, failNext: false, failAfter: false, grants: []authz.Grant{authz.NewGrant(authz.ScopeOrgAdmin, principal.OrganizationID)}}
	service, err := NewLifecycleDeleteService(conn, engine, func(ctx context.Context, serverID uuid.UUID) error {
		fixture.deletes++
		if fixture.failNext {
			fixture.failNext = false
			return errors.New("interrupted before the delete committed")
		}
		err := serverService.DeleteMcpServer(ctx, &mcpserversgen.DeleteMcpServerPayload{ID: serverID.String(), SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
		if err != nil {
			return fmt.Errorf("dashboard delete: %w", err)
		}
		if fixture.failAfter {
			fixture.failAfter = false
			return errors.New("interrupted after the delete committed")
		}
		return nil
	}, "platform-mcp-delete-test-key")
	require.NoError(t, err)
	fixture.service = service
	return ctx, fixture
}

// asAdmin is an organization administrator holding the grants seeded so far.
func (f *deleteMCPFixture) asAdmin(ctx context.Context) context.Context {
	return f.callerContext(ctx, f.grants...)
}

func (f *deleteMCPFixture) callerContext(ctx context.Context, grants ...authz.Grant) context.Context {
	ctx = contextvalues.WithAuthenticatedActor(ctx, &contextvalues.AuthContext{ActiveOrganizationID: f.principal.OrganizationID, UserID: f.principal.UserID}, urn.NewPrincipal(urn.PrincipalTypeUser, f.principal.UserID))
	ctx = contextvalues.SetActingSurface(ctx, contextvalues.ActingSurfacePlatformMCP)
	return authz.GrantsToContext(ctx, grants)
}

// seedServer creates a server in front of a toolset that is not the toolset's
// own hosted server, which the dashboard delete tombstones. No registration
// exists, so no other lifecycle tool could touch it.
func (f *deleteMCPFixture) seedServer(t *testing.T, ctx context.Context) mcpserversrepo.McpServer {
	t.Helper()
	toolset := f.seedToolset(t, ctx)
	server, err := mcpserversrepo.New(f.conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID: uuid.New(), ProjectID: f.project.ID, Name: conv.ToPGText("Duplicate Slack"), Slug: conv.ToPGText("dup-" + uuid.NewString()[:8]),
		ToolsetID: uuid.NullUUID{UUID: toolset.ID, Valid: true}, Visibility: "private",
	})
	require.NoError(t, err)
	return server
}

// seedHostedServer creates a toolset's own hosted server (id = toolset id),
// which the dashboard delete only turns off.
func (f *deleteMCPFixture) seedHostedServer(t *testing.T, ctx context.Context) mcpserversrepo.McpServer {
	t.Helper()
	toolset := f.seedToolset(t, ctx)
	orgSlug := projectOrganizationSlug(ctx, f.conn, f.principal.OrganizationID)
	require.NotEmpty(t, orgSlug)
	slug := orgSlug + "-" + toolset.Slug
	server, err := mcpserversrepo.New(f.conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID: toolset.ID, ProjectID: f.project.ID, Name: conv.ToPGText(toolset.Name), Slug: conv.ToPGText(slug),
		ToolsetID: uuid.NullUUID{UUID: toolset.ID, Valid: true}, Visibility: "private",
	})
	require.NoError(t, err)
	// The hosted server owns its address, as one created in the dashboard does.
	_, err = mcpendpointsrepo.New(f.conn).CreateMCPEndpoint(ctx, mcpendpointsrepo.CreateMCPEndpointParams{
		ProjectID: f.project.ID, McpServerID: uuid.NullUUID{UUID: toolset.ID, Valid: true}, Slug: slug,
	})
	require.NoError(t, err)
	return server
}

func (f *deleteMCPFixture) seedToolset(t *testing.T, ctx context.Context) toolsetsrepo.Toolset {
	t.Helper()
	suffix := uuid.NewString()[:8]
	toolset, err := toolsetsrepo.New(f.conn).CreateToolset(ctx, toolsetsrepo.CreateToolsetParams{
		OrganizationID: f.principal.OrganizationID, ProjectID: f.project.ID,
		Name: "Hosted " + suffix, Slug: "hosted-" + suffix,
	})
	require.NoError(t, err)
	// A toolset-backed server's mcp:write resource is its toolset.
	f.grants = append(f.grants, authz.NewGrant(authz.ScopeMCPWrite, toolset.ID.String()))
	return toolset
}

func (f *deleteMCPFixture) input(server mcpserversrepo.McpServer) DeleteMCPInput {
	return DeleteMCPInput{ProjectID: f.project.ID.String(), MCPID: server.ID.String(), ExpectedVersion: f.service.version(server), IdempotencyKey: uuid.NewString(), Confirmed: true}
}

func (f *deleteMCPFixture) receipt(t *testing.T, ctx context.Context, key string) platformrepo.PlatformMcpOperationReceipt {
	t.Helper()
	receipt, err := platformrepo.New(f.conn).GetPlatformMCPOperationReceipt(ctx, platformrepo.GetPlatformMCPOperationReceiptParams{OrganizationID: f.principal.OrganizationID, UserID: conv.ToPGText(f.principal.UserID), SubjectUrn: userSubjectURN(f.principal.UserID), ProjectID: f.project.ID, Operation: operationDeleteMCP, IdempotencyKey: key})
	require.NoError(t, err)
	return receipt
}

func TestDeleteMCPRemovesAServerTheCallerNeverRegistered(t *testing.T) {
	t.Parallel()
	ctx, fixture := newDeleteMCPFixture(t, "platform_mcp_delete_tombstone")
	server := fixture.seedServer(t, ctx)
	input := fixture.input(server)
	before, err := audittest.AuditLogCountByAction(ctx, fixture.conn, audit.ActionMcpServerDelete)
	require.NoError(t, err)

	output, err := fixture.service.Delete(fixture.asAdmin(ctx), fixture.principal, input)
	require.NoError(t, err)
	require.Equal(t, deleteMCPOutcomeDeleted, output.Outcome)
	require.Equal(t, deleteMCPStateGone, output.CurrentState)
	require.Equal(t, "Duplicate Slack", output.Name)
	require.False(t, output.Receipt.Replayed)

	_, err = mcpserversrepo.New(fixture.conn).GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{ID: server.ID, ProjectID: fixture.project.ID})
	require.Error(t, err, "the dashboard delete tombstones the server")
	after, err := audittest.AuditLogCountByAction(ctx, fixture.conn, audit.ActionMcpServerDelete)
	require.NoError(t, err)
	require.Equal(t, before+1, after, "the delete is audited through the dashboard path")
	require.Equal(t, receiptStatusSucceeded, fixture.receipt(t, ctx, input.IdempotencyKey).Status)

	replay, err := fixture.service.Delete(fixture.asAdmin(ctx), fixture.principal, input)
	require.NoError(t, err)
	require.True(t, replay.Receipt.Replayed)
	require.Equal(t, output.Receipt.ID, replay.Receipt.ID)
	require.Equal(t, deleteMCPOutcomeDeleted, replay.Outcome)
	require.Equal(t, "Duplicate Slack", replay.Name, "a replay answers from the receipt after the row is gone")
	require.Equal(t, 1, fixture.deletes, "a replay never deletes again")
}

func TestDeleteMCPTurnsOffAHostedToolsetServer(t *testing.T) {
	t.Parallel()
	ctx, fixture := newDeleteMCPFixture(t, "platform_mcp_delete_hosted")
	server := fixture.seedHostedServer(t, ctx)
	serverUpdates, err := audittest.AuditLogCountByAction(ctx, fixture.conn, audit.ActionMcpServerUpdate)
	require.NoError(t, err)
	serverDeletes, err := audittest.AuditLogCountByAction(ctx, fixture.conn, audit.ActionMcpServerDelete)
	require.NoError(t, err)

	output, err := fixture.service.Delete(fixture.asAdmin(ctx), fixture.principal, fixture.input(server))
	require.NoError(t, err)
	require.Equal(t, deleteMCPOutcomeTurnedOff, output.Outcome)
	require.Equal(t, deleteMCPStateDisabled, output.CurrentState)

	stored, err := mcpserversrepo.New(fixture.conn).GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{ID: server.ID, ProjectID: fixture.project.ID})
	require.NoError(t, err)
	require.Equal(t, "disabled", stored.Visibility)
	// The server row holds the hosting state; the toolset itself stays.
	_, err = toolsetsrepo.New(fixture.conn).GetToolsetByIDAndProject(ctx, toolsetsrepo.GetToolsetByIDAndProjectParams{ID: server.ID, ProjectID: fixture.project.ID})
	require.NoError(t, err, "turning a hosted server off keeps its toolset")

	// Audited exactly as the dashboard audits it: the server is turned off, not
	// deleted, so it is an update to disabled.
	afterUpdates, err := audittest.AuditLogCountByAction(ctx, fixture.conn, audit.ActionMcpServerUpdate)
	require.NoError(t, err)
	require.Equal(t, serverUpdates+1, afterUpdates)
	afterDeletes, err := audittest.AuditLogCountByAction(ctx, fixture.conn, audit.ActionMcpServerDelete)
	require.NoError(t, err)
	require.Equal(t, serverDeletes, afterDeletes)
}

func TestDeleteMCPRefusesAStaleVersion(t *testing.T) {
	t.Parallel()
	ctx, fixture := newDeleteMCPFixture(t, "platform_mcp_delete_stale")
	server := fixture.seedServer(t, ctx)
	input := fixture.input(server)
	_, err := mcpserversrepo.New(fixture.conn).UpdateMCPServer(ctx, mcpserversrepo.UpdateMCPServerParams{
		ID: server.ID, ProjectID: fixture.project.ID, Name: conv.ToPGText("Renamed"), Visibility: server.Visibility,
		EnvironmentID: server.EnvironmentID, UserSessionIssuerID: server.UserSessionIssuerID, RemoteMcpServerID: server.RemoteMcpServerID,
		TunneledMcpServerID: server.TunneledMcpServerID, ToolsetID: server.ToolsetID, UnproxiedMcpServerID: server.UnproxiedMcpServerID,
		ToolVariationsGroupID: server.ToolVariationsGroupID,
	})
	require.NoError(t, err)

	_, err = fixture.service.Delete(fixture.asAdmin(ctx), fixture.principal, input)
	require.ErrorIs(t, err, ErrLifecycleDeleteConflict)
	require.Zero(t, fixture.deletes, "nothing is deleted on a stale read")
	_, err = mcpserversrepo.New(fixture.conn).GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{ID: server.ID, ProjectID: fixture.project.ID})
	require.NoError(t, err)
}

func TestDeleteMCPRequiresOrganizationAdministration(t *testing.T) {
	t.Parallel()
	ctx, fixture := newDeleteMCPFixture(t, "platform_mcp_delete_forbidden")
	server := fixture.seedServer(t, ctx)
	writer := fixture.callerContext(ctx, fixture.grants[1:]...)

	_, err := fixture.service.Delete(writer, fixture.principal, fixture.input(server))
	var denied *ExternalAuthorizationError
	require.ErrorAs(t, err, &denied)
	require.Equal(t, string(authz.ScopeOrgAdmin), denied.RequiredScope)
	require.Zero(t, fixture.deletes)
}

func TestDeleteMCPResumesAnInterruptedDeleteWithTheSameKey(t *testing.T) {
	t.Parallel()
	ctx, fixture := newDeleteMCPFixture(t, "platform_mcp_delete_resume")
	server := fixture.seedServer(t, ctx)
	input := fixture.input(server)

	fixture.failNext = true
	_, err := fixture.service.Delete(fixture.asAdmin(ctx), fixture.principal, input)
	require.Error(t, err)
	require.Equal(t, receiptStatusPending, fixture.receipt(t, ctx, input.IdempotencyKey).Status)

	output, err := fixture.service.Delete(fixture.asAdmin(ctx), fixture.principal, input)
	require.NoError(t, err)
	require.Equal(t, deleteMCPOutcomeDeleted, output.Outcome)
	require.False(t, output.Receipt.Replayed)
	require.Equal(t, 2, fixture.deletes)
	require.Equal(t, receiptStatusSucceeded, fixture.receipt(t, ctx, input.IdempotencyKey).Status)
}

func TestDeleteMCPRefusesAKeyReusedForAnotherServer(t *testing.T) {
	t.Parallel()
	ctx, fixture := newDeleteMCPFixture(t, "platform_mcp_delete_key_reuse")
	first := fixture.input(fixture.seedServer(t, ctx))
	_, err := fixture.service.Delete(fixture.asAdmin(ctx), fixture.principal, first)
	require.NoError(t, err)

	second := fixture.input(fixture.seedServer(t, ctx))
	second.IdempotencyKey = first.IdempotencyKey
	_, err = fixture.service.Delete(fixture.asAdmin(ctx), fixture.principal, second)
	require.ErrorIs(t, err, ErrLifecycleDeleteConflict)
	require.Equal(t, 1, fixture.deletes)
}

func TestDeleteMCPReportsAMissingServer(t *testing.T) {
	t.Parallel()
	ctx, fixture := newDeleteMCPFixture(t, "platform_mcp_delete_missing")
	_, err := fixture.service.Delete(fixture.asAdmin(ctx), fixture.principal, DeleteMCPInput{ProjectID: fixture.project.ID.String(), MCPID: uuid.NewString(), ExpectedVersion: "v", IdempotencyKey: uuid.NewString(), Confirmed: true})
	require.ErrorIs(t, err, ErrLifecycleDeleteMissing)
	require.Zero(t, fixture.deletes)
}

func TestDeleteMCPRefusesToResumeOnAServerChangedSinceTheFirstAttempt(t *testing.T) {
	t.Parallel()
	ctx, fixture := newDeleteMCPFixture(t, "platform_mcp_delete_resume_stale")
	server := fixture.seedServer(t, ctx)
	input := fixture.input(server)

	fixture.failNext = true
	_, err := fixture.service.Delete(fixture.asAdmin(ctx), fixture.principal, input)
	require.Error(t, err)
	_, err = mcpserversrepo.New(fixture.conn).UpdateMCPServer(ctx, mcpserversrepo.UpdateMCPServerParams{
		ID: server.ID, ProjectID: fixture.project.ID, Name: conv.ToPGText("Renamed"), Visibility: server.Visibility,
		EnvironmentID: server.EnvironmentID, UserSessionIssuerID: server.UserSessionIssuerID, RemoteMcpServerID: server.RemoteMcpServerID,
		TunneledMcpServerID: server.TunneledMcpServerID, ToolsetID: server.ToolsetID, UnproxiedMcpServerID: server.UnproxiedMcpServerID,
		ToolVariationsGroupID: server.ToolVariationsGroupID,
	})
	require.NoError(t, err)

	_, err = fixture.service.Delete(fixture.asAdmin(ctx), fixture.principal, input)
	require.ErrorIs(t, err, ErrLifecycleDeleteConflict)
	require.Equal(t, 1, fixture.deletes, "a resume re-checks the version before deleting")
	_, err = mcpserversrepo.New(fixture.conn).GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{ID: server.ID, ProjectID: fixture.project.ID})
	require.NoError(t, err)
}

func TestDeleteMCPResumeReportsADeleteThatAlreadyCommitted(t *testing.T) {
	t.Parallel()
	ctx, fixture := newDeleteMCPFixture(t, "platform_mcp_delete_resume_applied")
	server := fixture.seedHostedServer(t, ctx)
	input := fixture.input(server)

	fixture.failAfter = true
	_, err := fixture.service.Delete(fixture.asAdmin(ctx), fixture.principal, input)
	require.Error(t, err)
	require.Equal(t, receiptStatusPending, fixture.receipt(t, ctx, input.IdempotencyKey).Status)

	output, err := fixture.service.Delete(fixture.asAdmin(ctx), fixture.principal, input)
	require.NoError(t, err)
	require.Equal(t, deleteMCPOutcomeTurnedOff, output.Outcome)
	require.Equal(t, deleteMCPStateDisabled, output.CurrentState)
	require.Equal(t, 1, fixture.deletes, "the committed turn-off is not run again")
	require.Equal(t, receiptStatusSucceeded, fixture.receipt(t, ctx, input.IdempotencyKey).Status)
}
