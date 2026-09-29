package platformmcp

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/plugins"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type toolExposureFixture struct {
	principal Principal
	project   ResolvedProject
	toolsetID uuid.UUID
	service   *MCPToolExposureService
	conn      *pgxpool.Pool
	tools     []string
}

// seedToolExposureFixture builds the chain the workflow depends on end to end:
// a completed deployment with generated function tools, a toolset, and the
// modern server record that fronts it.
func seedToolExposureFixture(t *testing.T, ctx context.Context, name string) (context.Context, toolExposureFixture) {
	t.Helper()
	conn, err := platformMCPInfra.CloneTestDatabase(t, name)
	require.NoError(t, err)
	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	orgSlug := projectOrganizationSlug(ctx, conn, principal.OrganizationID)
	require.NotEmpty(t, orgSlug)

	toolset, err := toolsetsrepo.New(conn).CreateToolset(ctx, toolsetsrepo.CreateToolsetParams{
		OrganizationID: principal.OrganizationID, ProjectID: project.ID,
		Name: "Hosted MCP", Slug: "hosted-" + uuid.NewString()[:8],
		McpSlug: conv.ToPGText(orgSlug + "-hosted"), McpEnabled: true,
	})
	require.NoError(t, err)
	_, err = mcpserversrepo.New(conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID: toolset.ID, ProjectID: project.ID, Name: conv.ToPGText(toolset.Name), Slug: toolset.McpSlug,
		ToolsetID: uuid.NullUUID{UUID: toolset.ID, Valid: true}, Visibility: "private",
	})
	require.NoError(t, err)

	fixtures := testrepo.New(conn)
	assetID, deploymentID, functionID := uuid.New(), uuid.New(), uuid.New()
	require.NoError(t, fixtures.InsertDeploymentAssetFixture(ctx, testrepo.InsertDeploymentAssetFixtureParams{
		ID: assetID, ProjectID: uuid.NullUUID{UUID: project.ID, Valid: true}, OrganizationID: conv.ToPGText(principal.OrganizationID),
		Name: "functions.zip", Url: "memory://functions.zip", Kind: "functions", ContentType: "application/zip", Sha256: uuid.NewString(),
	}))
	require.NoError(t, fixtures.InsertCompletedDeploymentFixture(ctx, testrepo.InsertCompletedDeploymentFixtureParams{
		ID: deploymentID, UserID: principal.UserID, ProjectID: project.ID,
		OrganizationID: principal.OrganizationID, IdempotencyKey: uuid.NewString(),
	}))
	require.NoError(t, fixtures.InsertDeploymentFunctionFixture(ctx, testrepo.InsertDeploymentFunctionFixtureParams{
		ID: functionID, DeploymentID: deploymentID, AssetID: assetID, Name: "Orders", Slug: "orders", Runtime: "nodejs:22",
	}))

	tools := []string{
		"tools:function:orders:cancel_order",
		"tools:function:orders:create_order",
		"tools:function:orders:list_orders",
	}
	for _, value := range tools {
		parsed, parseErr := urn.ParseTool(value)
		require.NoError(t, parseErr)
		require.NoError(t, fixtures.InsertFunctionToolDefinitionFixture(ctx, testrepo.InsertFunctionToolDefinitionFixtureParams{
			ToolUrn: parsed, ProjectID: project.ID, DeploymentID: deploymentID, FunctionID: functionID,
			Runtime: "nodejs:22", Name: parsed.Name, Description: "A generated function tool.",
		}))
	}

	ctx = contextvalues.WithAuthenticatedActor(ctx, &contextvalues.AuthContext{ActiveOrganizationID: principal.OrganizationID, UserID: principal.UserID}, urn.NewPrincipal(urn.PrincipalTypeUser, principal.UserID))
	ctx = contextvalues.SetActingSurface(ctx, contextvalues.ActingSurfacePlatformMCP)
	ctx = authz.GrantsToContext(ctx, []authz.Grant{
		authz.NewGrant(authz.ScopeOrgAdmin, principal.OrganizationID),
		authz.NewGrant(authz.ScopeProjectRead, project.ID.String()),
		authz.NewGrant(authz.ScopeMCPRead, toolset.ID.String()),
		// The write is checked against the exact server, exactly as the
		// dashboard's own toolset update checks it.
		authz.NewGrant(authz.ScopeMCPWrite, toolset.ID.String()),
	})
	authorizer := authz.NewEngine(testenv.NewLogger(t), conn, func(context.Context, string) (bool, error) { return false, nil }, nil)
	service, err := NewMCPToolExposureService(testenv.NewLogger(t), conn, audit.NewLogger(), authorizer, "tool-exposure-cursor-key", plugins.PublicationRequests{}, nil)
	require.NoError(t, err)

	return ctx, toolExposureFixture{principal: principal, project: project, toolsetID: toolset.ID, service: service, conn: conn, tools: tools}
}

func (f toolExposureFixture) add(t *testing.T, ctx context.Context, version string, urns ...string) (MCPToolExposureMutationOutput, error) {
	t.Helper()
	return f.service.AddTools(ctx, f.principal, ChangeMCPToolsInput{
		ProjectID: f.project.ID.String(), MCPID: f.toolsetID.String(), ToolURNs: urns,
		ExpectedVersion: version, IdempotencyKey: uuid.NewString(), Confirmed: true,
	})
}

func TestListProjectToolsReportsGeneratedToolsWithTheirSource(t *testing.T) {
	t.Parallel()
	ctx, fixture := seedToolExposureFixture(t, t.Context(), "platform_mcp_list_project_tools")

	page, err := fixture.service.ListProjectTools(ctx, fixture.principal, fixture.project, ListProjectToolsInput{
		ProjectID: fixture.project.ID.String(), Limit: 2,
	})
	require.NoError(t, err)
	require.Len(t, page.Tools, 2)
	require.NotEmpty(t, page.NextCursor, "a bounded page hands back a cursor for the rest")
	require.Equal(t, fixture.tools[0], page.Tools[0].URN)
	require.Equal(t, "cancel_order", page.Tools[0].Name)
	require.Equal(t, "function", page.Tools[0].SourceKind)
	require.Equal(t, "orders", page.Tools[0].SourceSlug, "the caller can see which source produced the tool")
	require.NotEmpty(t, page.DeploymentID)

	rest, err := fixture.service.ListProjectTools(ctx, fixture.principal, fixture.project, ListProjectToolsInput{
		ProjectID: fixture.project.ID.String(), Limit: 2, Cursor: page.NextCursor,
	})
	require.NoError(t, err)
	require.Len(t, rest.Tools, 1)
	require.Equal(t, fixture.tools[2], rest.Tools[0].URN)
	require.Empty(t, rest.NextCursor)

	filtered, err := fixture.service.ListProjectTools(ctx, fixture.principal, fixture.project, ListProjectToolsInput{
		ProjectID: fixture.project.ID.String(), Query: "create", SourceKind: "function",
	})
	require.NoError(t, err)
	require.Len(t, filtered.Tools, 1)
	require.Equal(t, fixture.tools[1], filtered.Tools[0].URN)

	none, err := fixture.service.ListProjectTools(ctx, fixture.principal, fixture.project, ListProjectToolsInput{
		ProjectID: fixture.project.ID.String(), SourceKind: "openapi",
	})
	require.NoError(t, err)
	require.Empty(t, none.Tools)
}

func TestAddToolsToMCPAppendsWithoutReplacingTheList(t *testing.T) {
	t.Parallel()
	ctx, fixture := seedToolExposureFixture(t, t.Context(), "platform_mcp_add_tools")

	empty, err := fixture.service.Exposure(ctx, fixture.principal, fixture.project.ID, fixture.toolsetID)
	require.NoError(t, err)
	require.Equal(t, 0, empty.ToolCount)
	require.Equal(t, fixture.toolsetID.String(), empty.ToolsetID)

	first, err := fixture.add(t, ctx, empty.ExposureVersion, fixture.tools[0])
	require.NoError(t, err)
	require.Equal(t, "applied", first.Outcome)
	require.Equal(t, []string{fixture.tools[0]}, first.Applied)
	require.Empty(t, first.Unchanged)
	require.Equal(t, "fresh_read_after_commit", first.SnapshotScope)
	require.NotNil(t, first.Exposure)
	require.Equal(t, []string{fixture.tools[0]}, first.Exposure.ToolURNs)
	require.False(t, first.Receipt.Replayed)

	// Adding a second tool must leave the first in place: this is the whole
	// difference from the dashboard's replace-the-array write.
	second, err := fixture.add(t, ctx, first.Exposure.ExposureVersion, fixture.tools[1])
	require.NoError(t, err)
	require.Equal(t, "applied", second.Outcome)
	require.ElementsMatch(t, []string{fixture.tools[0], fixture.tools[1]}, second.Exposure.ToolURNs)

	// Asking again for a tool the server already exposes is a no-op, not a
	// second append and not a failure.
	noop, err := fixture.add(t, ctx, second.Exposure.ExposureVersion, fixture.tools[1])
	require.NoError(t, err)
	require.Equal(t, "no_op", noop.Outcome)
	require.Empty(t, noop.Applied)
	require.Equal(t, []string{fixture.tools[1]}, noop.Unchanged)
	require.Equal(t, 2, noop.Exposure.ToolCount)

	// A partial batch reports both halves rather than pretending it all landed.
	partial, err := fixture.add(t, ctx, noop.Exposure.ExposureVersion, fixture.tools[1], fixture.tools[2])
	require.NoError(t, err)
	require.Equal(t, "applied", partial.Outcome)
	require.Equal(t, []string{fixture.tools[2]}, partial.Applied)
	require.Equal(t, []string{fixture.tools[1]}, partial.Unchanged)
	require.Equal(t, 3, partial.Exposure.ToolCount)

	removed, err := fixture.service.RemoveTools(ctx, fixture.principal, ChangeMCPToolsInput{
		ProjectID: fixture.project.ID.String(), MCPID: fixture.toolsetID.String(),
		ToolURNs: []string{fixture.tools[0]}, ExpectedVersion: partial.Exposure.ExposureVersion,
		IdempotencyKey: uuid.NewString(), Confirmed: true,
	})
	require.NoError(t, err)
	require.Equal(t, "applied", removed.Outcome)
	require.ElementsMatch(t, []string{fixture.tools[1], fixture.tools[2]}, removed.Exposure.ToolURNs)
}

// The hazard this change exists to avoid: a caller acting on a stale read must
// not be able to overwrite a list someone else moved on.
func TestAddToolsToMCPRefusesAStaleReadAndKeepsTheConcurrentWrite(t *testing.T) {
	t.Parallel()
	ctx, fixture := seedToolExposureFixture(t, t.Context(), "platform_mcp_add_tools_stale")

	stale, err := fixture.service.Exposure(ctx, fixture.principal, fixture.project.ID, fixture.toolsetID)
	require.NoError(t, err)

	// Somebody else adds a tool between the caller's read and its write.
	require.NoError(t, testrepo.New(fixture.conn).InsertToolsetVersionFixture(ctx, testrepo.InsertToolsetVersionFixtureParams{
		ToolsetID: fixture.toolsetID, Version: 1, ToolUrns: []string{fixture.tools[2]},
	}))

	_, err = fixture.add(t, ctx, stale.ExposureVersion, fixture.tools[0])
	var refusal *MCPToolExposureError
	require.ErrorAs(t, err, &refusal)
	require.Equal(t, "conflict", refusal.Code)

	current, err := fixture.service.Exposure(ctx, fixture.principal, fixture.project.ID, fixture.toolsetID)
	require.NoError(t, err)
	require.Equal(t, []string{fixture.tools[2]}, current.ToolURNs, "the concurrent write survives the refusal")

	// Repeating against the list as it stands now appends to it.
	applied, err := fixture.add(t, ctx, current.ExposureVersion, fixture.tools[0])
	require.NoError(t, err)
	require.ElementsMatch(t, []string{fixture.tools[0], fixture.tools[2]}, applied.Exposure.ToolURNs)
}

func TestAddToolsToMCPRefusesTheWholeBatchAndNamesUnknownTools(t *testing.T) {
	t.Parallel()
	ctx, fixture := seedToolExposureFixture(t, t.Context(), "platform_mcp_add_tools_unknown")

	current, err := fixture.service.Exposure(ctx, fixture.principal, fixture.project.ID, fixture.toolsetID)
	require.NoError(t, err)
	_, err = fixture.add(t, ctx, current.ExposureVersion, fixture.tools[0], "tools:function:orders:refund_order")
	var refusal *MCPToolExposureError
	require.ErrorAs(t, err, &refusal)
	require.Equal(t, "invalid_request", refusal.Code)
	require.Equal(t, []string{"tools:function:orders:refund_order"}, refusal.UnknownTools)

	after, err := fixture.service.Exposure(ctx, fixture.principal, fixture.project.ID, fixture.toolsetID)
	require.NoError(t, err)
	require.Empty(t, after.ToolURNs, "a refused batch changes nothing, not even the tools it could have added")
}

func TestAddToolsToMCPReplaysOneIdempotencyKeyWithoutAppendingTwice(t *testing.T) {
	t.Parallel()
	ctx, fixture := seedToolExposureFixture(t, t.Context(), "platform_mcp_add_tools_replay")

	current, err := fixture.service.Exposure(ctx, fixture.principal, fixture.project.ID, fixture.toolsetID)
	require.NoError(t, err)
	input := ChangeMCPToolsInput{
		ProjectID: fixture.project.ID.String(), MCPID: fixture.toolsetID.String(),
		ToolURNs: []string{fixture.tools[0]}, ExpectedVersion: current.ExposureVersion,
		IdempotencyKey: uuid.NewString(), Confirmed: true,
	}
	first, err := fixture.service.AddTools(ctx, fixture.principal, input)
	require.NoError(t, err)
	require.False(t, first.Receipt.Replayed)

	replay, err := fixture.service.AddTools(ctx, fixture.principal, input)
	require.NoError(t, err)
	require.True(t, replay.Receipt.Replayed)
	require.Equal(t, first.Receipt.ID, replay.Receipt.ID)
	require.Equal(t, []string{fixture.tools[0]}, replay.Exposure.ToolURNs)
}

// A server that fronts an upstream, and a caller naming something that is not
// a server at all, get the same readable refusal that points at the dashboard.
func TestChangeMCPToolsRefusesAServerItDoesNotOwnTheToolsOf(t *testing.T) {
	t.Parallel()
	ctx, fixture := seedToolExposureFixture(t, t.Context(), "platform_mcp_tools_unsupported_target")

	_, err := fixture.service.Exposure(ctx, fixture.principal, fixture.project.ID, uuid.New())
	require.ErrorIs(t, err, ErrMCPToolExposureMissing)
}
