package platformmcp

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/plugins"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	remotemcprepo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/toolsets"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type toolExposureFixture struct {
	principal Principal
	project   ResolvedProject
	toolsetID uuid.UUID
	// toolsetSlug is what the write's own row lock is taken by, so an
	// interleaving test can hold that lock from a probe connection.
	toolsetSlug string
	service     *MCPToolExposureService
	conn        *pgxpool.Pool
	tools       []string
	grants      []authz.Grant
	// indexed records every toolset the service asked to have reindexed. A
	// committed change must schedule one, because dynamic-mode tools/list
	// refuses a version with no search index.
	indexed *[]uuid.UUID
}

// grantMCP returns a context that also carries mcp:read and mcp:write on the
// named servers. Grants stay resource-exact here for the same reason the
// production check is: a wildcard would let a test reach a target the caller
// should not, and hide an authorization regression.
func (f toolExposureFixture) grantMCP(ctx context.Context, ids ...uuid.UUID) context.Context {
	grants := slices.Clone(f.grants)
	for _, id := range ids {
		grants = append(grants, authz.NewGrant(authz.ScopeMCPRead, id.String()), authz.NewGrant(authz.ScopeMCPWrite, id.String()))
	}
	return authz.GrantsToContext(ctx, grants)
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
	// The mutation re-checks org:admin live, so the caller has to be a real
	// administrator of a real organization, not merely hold the grant in a
	// context this test built.
	require.NoError(t, authz.SeedSystemRoleGrants(ctx, conn, principal.OrganizationID))
	seedPlatformMCPAuthorizationMember(t, ctx, conn, principal.OrganizationID, principal.UserID, authz.SystemRoleAdmin)

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
	grants := []authz.Grant{
		authz.NewGrant(authz.ScopeOrgAdmin, principal.OrganizationID),
		authz.NewGrant(authz.ScopeProjectRead, project.ID.String()),
		authz.NewGrant(authz.ScopeMCPRead, toolset.ID.String()),
		// The write is checked against the exact server, exactly as the
		// dashboard's own toolset update checks it.
		authz.NewGrant(authz.ScopeMCPWrite, toolset.ID.String()),
	}
	ctx = authz.GrantsToContext(ctx, grants)
	engine := authz.NewEngine(testenv.NewLogger(t), conn, func(context.Context, string) (bool, error) { return false, nil }, nil)
	// The live org-admin authorizer, not a stub: the mutation re-checks
	// membership and the org:admin grant against the database, so the test
	// only passes when the seeded caller really is a live administrator.
	admin := NewLiveOrgAdminAuthorizer(conn, engine)
	service, err := NewMCPToolExposureService(testenv.NewLogger(t), conn, audit.NewLogger(), engine, admin, "tool-exposure-cursor-key", plugins.PublicationRequests{}, nil, testOperationBudget(), testOperationBudget())
	require.NoError(t, err)

	// Stands in for toolsets.TriggerToolsetIndexForVersion, which needs a
	// Temporal environment this package's tests do not run.
	indexed := &[]uuid.UUID{}
	service.WithIndexing(func(ctx context.Context, indexedProject, indexedToolset uuid.UUID) error {
		require.Equal(t, project.ID, indexedProject)
		*indexed = append(*indexed, indexedToolset)
		// Answer as the real trigger does for this toolset's state: one that
		// is not MCP-enabled is reported as needing no index.
		target, err := toolsetsrepo.New(conn).GetToolsetByIDAndProject(ctx, toolsetsrepo.GetToolsetByIDAndProjectParams{ID: indexedToolset, ProjectID: indexedProject})
		require.NoError(t, err)
		if !target.McpEnabled {
			return toolsets.ErrToolsetIndexNotRequired
		}
		return nil
	})

	return ctx, toolExposureFixture{
		principal: principal, project: project, toolsetID: toolset.ID, toolsetSlug: toolset.Slug,
		service: service, conn: conn, tools: tools, grants: grants, indexed: indexed,
	}
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

	// Every committed change schedules the search-index rebuild. Without it a
	// dynamic-mode server refuses tools/list entirely until the five-minute
	// sweep catches up, so adding a tool would take a working server offline.
	require.Equal(t, []uuid.UUID{fixture.toolsetID, fixture.toolsetID}, *fixture.indexed,
		"both committed changes scheduled indexing for the toolset they wrote")

	// Asking again for a tool the server already exposes is a no-op, not a
	// second append and not a failure.
	noop, err := fixture.add(t, ctx, second.Exposure.ExposureVersion, fixture.tools[1])
	require.NoError(t, err)
	require.Equal(t, "no_op", noop.Outcome)
	require.Len(t, *fixture.indexed, 2, "a no-op created no version, so it schedules no reindex")
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
		ToolsetID: fixture.toolsetID, ProjectID: fixture.project.ID, OrganizationID: fixture.principal.OrganizationID,
		Version: 1, ToolUrns: []string{fixture.tools[2]},
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
// a server at all, must both reach the readable dashboard refusal on the
// mutation path too — not a forbidden error or a bare not-found leaking out
// of the authorization or target lookup.
func TestChangeMCPToolsRefusesAServerItDoesNotOwnTheToolsOf(t *testing.T) {
	t.Parallel()
	ctx, fixture := seedToolExposureFixture(t, t.Context(), "platform_mcp_tools_unsupported_target")

	// A server that fronts an upstream: its tools come from that upstream, so
	// there is no Gram toolset behind it to change.
	remoteID, upstreamID := uuid.New(), uuid.New()
	_, err := remotemcprepo.New(fixture.conn).CreateServer(ctx, remotemcprepo.CreateServerParams{
		ID: remoteID, ProjectID: fixture.project.ID, TransportType: "streamable-http", Url: "https://upstream.example.test/mcp",
	})
	require.NoError(t, err)
	_, err = mcpserversrepo.New(fixture.conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID: upstreamID, ProjectID: fixture.project.ID, Name: conv.ToPGText("Upstream MCP"), Slug: conv.ToPGText("upstream-mcp"),
		RemoteMcpServerID: uuid.NullUUID{UUID: remoteID, Valid: true}, Visibility: "private",
	})
	require.NoError(t, err)
	ctx = fixture.grantMCP(ctx, upstreamID)

	change := func(ctx context.Context, operation string, target uuid.UUID) error {
		input := ChangeMCPToolsInput{
			ProjectID: fixture.project.ID.String(), MCPID: target.String(),
			ToolURNs: []string{fixture.tools[0]}, ExpectedVersion: strings.Repeat("a", 64),
			IdempotencyKey: uuid.NewString(), Confirmed: true,
		}
		if operation == "remove" {
			_, err := fixture.service.RemoveTools(ctx, fixture.principal, input)
			return err
		}
		_, err := fixture.service.AddTools(ctx, fixture.principal, input)
		return err
	}

	// An authorized caller naming a server whose tools are not this project's
	// gets the readable refusal on the read and on both mutations, not a
	// forbidden error and not a bare not-found.
	_, err = fixture.service.Exposure(ctx, fixture.principal, fixture.project.ID, upstreamID)
	require.ErrorIs(t, err, ErrMCPToolExposureMissing)
	for _, operation := range []string{"add", "remove"} {
		err := change(ctx, operation, upstreamID)
		var refusal *MCPToolExposureError
		require.ErrorAs(t, err, &refusal, "%s reaches the readable refusal", operation)
		require.Equal(t, "not_found", refusal.Code, "%s", operation)
		require.Contains(t, refusal.Message, "dashboard", "%s points somewhere that can do it", operation)
	}

	// A target the caller holds no grant on answers with the permission
	// denial, identically whether or not it exists. Telling the two apart
	// would make this tool a project- and server-id oracle.
	absent := uuid.New()
	for _, target := range []uuid.UUID{absent, upstreamID} {
		err := change(authz.GrantsToContext(ctx, fixture.grants), "add", target)
		var denied *ExternalAuthorizationError
		require.ErrorAs(t, err, &denied)
		require.Equal(t, string(authz.ScopeMCPWrite), denied.RequiredScope)
	}
}

// The tool list lives on the toolset, not on the server record, so every live
// server fronting that toolset is an alias for the same list. Write access to
// the named server alone must not move the others.
func TestChangeMCPToolsAuthorizesEveryServerSharingTheToolList(t *testing.T) {
	t.Parallel()
	ctx, fixture := seedToolExposureFixture(t, t.Context(), "platform_mcp_tools_shared_toolset")

	alone, err := fixture.service.Exposure(ctx, fixture.principal, fixture.project.ID, fixture.toolsetID)
	require.NoError(t, err)
	require.Equal(t, 0, alone.SharedWithOther, "one server fronting the toolset is the normal case")

	second := uuid.New()
	_, err = mcpserversrepo.New(fixture.conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID: second, ProjectID: fixture.project.ID, Name: conv.ToPGText("Second front"), Slug: conv.ToPGText("second-front"),
		ToolsetID: uuid.NullUUID{UUID: fixture.toolsetID, Valid: true}, Visibility: "private",
	})
	require.NoError(t, err)

	shared, err := fixture.service.Exposure(ctx, fixture.principal, fixture.project.ID, fixture.toolsetID)
	require.NoError(t, err, "the read still works; it is the write that needs the wider permission")
	require.Equal(t, 1, shared.SharedWithOther, "the caller is told the change reaches another server")

	// Holding mcp:write on only the named server is no longer enough.
	_, err = fixture.add(t, ctx, shared.ExposureVersion, fixture.tools[0])
	var refusal *MCPToolExposureError
	require.ErrorAs(t, err, &refusal)
	require.Equal(t, "forbidden", refusal.Code)
	require.NotContains(t, refusal.Message, second.String(), "the other server is not named to a caller who cannot reach it")

	_, err = toolsetsrepo.New(fixture.conn).GetLatestToolsetVersion(ctx, fixture.toolsetID)
	require.ErrorIs(t, err, pgx.ErrNoRows, "nothing was written for either server")

	// With write access to both, the same change goes through.
	applied, err := fixture.add(t, fixture.grantMCP(ctx, second), shared.ExposureVersion, fixture.tools[0])
	require.NoError(t, err)
	require.Equal(t, "applied", applied.Outcome)
	require.Equal(t, []string{fixture.tools[0]}, applied.Exposure.ToolURNs)
	require.Equal(t, 1, applied.Exposure.SharedWithOther)
}

// Nothing in the schema pairs mcp_servers.toolset_id with project_id, so a
// server in a different project can front this toolset. Authorizing such an id
// from here is not possible: authz.MCPCheck injects the named project as a
// selector dimension precisely so project-scoped grants match, which means a
// project-wide mcp:write in THIS project satisfies the check for a foreign
// server id. So the foreign server must be excluded from the authorized set
// AND the change must be refused, because the write would otherwise move it
// silently.
func TestChangeMCPToolsRefusesAToolListSharedWithAnotherProject(t *testing.T) {
	t.Parallel()
	ctx, fixture := seedToolExposureFixture(t, t.Context(), "platform_mcp_tools_foreign_project")

	foreignProject, err := projectsrepo.New(fixture.conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
		Name: "Other project", Slug: "other-" + uuid.NewString()[:8], OrganizationID: fixture.principal.OrganizationID,
	})
	require.NoError(t, err)
	foreignServer := uuid.New()
	_, err = mcpserversrepo.New(fixture.conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID: foreignServer, ProjectID: foreignProject.ID, Name: conv.ToPGText("Foreign front"),
		Slug: conv.ToPGText("foreign-front"), ToolsetID: uuid.NullUUID{UUID: fixture.toolsetID, Valid: true},
		Visibility: "private",
	})
	require.NoError(t, err)

	shared, err := fixture.service.Exposure(ctx, fixture.principal, fixture.project.ID, fixture.toolsetID)
	require.NoError(t, err)
	require.Equal(t, 0, shared.SharedWithOther,
		"the foreign server is not counted among the servers this project's permissions cover")

	// Even holding mcp:write on the foreign server id — which a project-wide
	// grant in this project would supply for free — the change is refused.
	_, err = fixture.add(t, fixture.grantMCP(ctx, foreignServer), shared.ExposureVersion, fixture.tools[0])
	var refusal *MCPToolExposureError
	require.ErrorAs(t, err, &refusal)
	require.Equal(t, "shared_outside_project", refusal.Code)
	require.ErrorIs(t, err, ErrMCPToolExposureShared,
		"a structural refusal, not a conflict the caller should re-read and retry")
	require.NotErrorIs(t, err, ErrMCPToolExposureConflict)
	require.Contains(t, refusal.Message, "dashboard")
	require.NotContains(t, refusal.Message, foreignServer.String(), "the foreign server is not named")
	require.NotContains(t, refusal.Message, foreignProject.ID.String(), "nor is its project")

	_, err = toolsetsrepo.New(fixture.conn).GetLatestToolsetVersion(ctx, fixture.toolsetID)
	require.ErrorIs(t, err, pgx.ErrNoRows, "nothing was written for either project's server")

	// Once the foreign server is gone the same change goes through, which
	// proves the refusal was about the sharing and not about the request.
	_, err = mcpserversrepo.New(fixture.conn).DeleteMCPServer(ctx, mcpserversrepo.DeleteMCPServerParams{
		ID: foreignServer, ProjectID: foreignProject.ID,
	})
	require.NoError(t, err)
	applied, err := fixture.add(t, ctx, shared.ExposureVersion, fixture.tools[0])
	require.NoError(t, err)
	require.Equal(t, "applied", applied.Outcome)
	require.Equal(t, []string{fixture.tools[0]}, applied.Exposure.ToolURNs)
}

// The exposure read takes no lock of its own, and the toolset row lock the
// write takes is both later and on a different table, so without a lock on the
// server row the whole decision — which toolset to write, which servers that
// write moves — is made against an unpinned snapshot of mcp_servers.
// UpdateMCPServer assigns toolset_id, so a dashboard edit can repoint this
// server between the read and the write, and the exposure version token covers
// toolset_versions only and would not notice. The change would then land on a
// toolset the named server no longer fronts.
//
// Falsification: drop the LockPlatformMCPServerToolsetBinding call from the
// Mutate closure and the mutation never waits for the probe's row lock, so
// WaitForQueryBlockedBy below fails after its 30s deadline.
func TestChangeMCPToolsPinsTheServerToolsetBindingBeforeDeciding(t *testing.T) {
	t.Parallel()
	ctx, fixture := seedToolExposureFixture(t, t.Context(), "platform_mcp_tools_binding_lock")

	stale, err := fixture.service.Exposure(ctx, fixture.principal, fixture.project.ID, fixture.toolsetID)
	require.NoError(t, err)

	// A second toolset in the same project for the server to be moved onto.
	other, err := toolsetsrepo.New(fixture.conn).CreateToolset(ctx, toolsetsrepo.CreateToolsetParams{
		OrganizationID: fixture.principal.OrganizationID, ProjectID: fixture.project.ID,
		Name: "Moved target", Slug: "moved-" + uuid.NewString()[:8], McpEnabled: true,
	})
	require.NoError(t, err)

	// Hold exactly the row lock a dashboard edit of this server would take.
	probe := testenv.BeginTx(t, ctx, fixture.conn)
	_, err = testrepo.New(probe).LockMCPServerRowFixture(ctx, testrepo.LockMCPServerRowFixtureParams{
		ID: fixture.toolsetID, ProjectID: fixture.project.ID,
	})
	require.NoError(t, err)

	result := make(chan error, 1)
	go func() {
		_, addErr := fixture.add(t, ctx, stale.ExposureVersion, fixture.tools[0])
		result <- addErr
	}()

	// The mutation must not be able to reach its decision while that row is
	// pinned. This is the assertion the fix exists for.
	testenv.WaitForQueryBlockedBy(t, ctx, fixture.conn, testenv.BackendPID(probe), "%LockPlatformMCPServerToolsetBinding%")

	// Now commit the repoint, which is the race: the server the caller named
	// no longer fronts the toolset its exposure version was read from.
	_, err = testrepo.New(probe).RepointMCPServerToolsetFixture(ctx, testrepo.RepointMCPServerToolsetFixtureParams{
		ID: fixture.toolsetID, ProjectID: fixture.project.ID, ToolsetID: other.ID,
		OrganizationID: fixture.principal.OrganizationID,
	})
	require.NoError(t, err)
	require.NoError(t, probe.Commit(ctx))

	// Having waited, the mutation reads the binding as it now stands and
	// refuses, instead of writing the tool it was given onto the toolset the
	// server was detached from.
	var refusal *MCPToolExposureError
	require.ErrorAs(t, <-result, &refusal)
	require.Equal(t, "conflict", refusal.Code)

	_, err = toolsetsrepo.New(fixture.conn).GetLatestToolsetVersion(ctx, fixture.toolsetID)
	require.ErrorIs(t, err, pgx.ErrNoRows, "the detached toolset was not written")
	_, err = toolsetsrepo.New(fixture.conn).GetLatestToolsetVersion(ctx, other.ID)
	require.ErrorIs(t, err, pgx.ErrNoRows, "and neither was the one it moved to")
}

// Why a server cannot join the toolset while a change is in flight, which is
// the guarantee the lock ordering buys and the reason the post-write re-read is
// only a backstop.
//
// Attaching a server writes mcp_servers.toolset_id, and PostgreSQL enforces
// that foreign key with FOR KEY SHARE on the referenced toolsets row. The
// exposure change holds FOR UPDATE on that row from before its authorized read
// until commit, and the two conflict — so the attach either committed before
// the lock, and is in the authorized set, or it waits until after the change.
//
// Falsification: relax the probe to FOR NO KEY UPDATE, which does not conflict
// with FOR KEY SHARE, and the attach succeeds instead of timing out.
func TestToolsetRowLockBlocksAttachingAnotherServerToIt(t *testing.T) {
	t.Parallel()
	ctx, fixture := seedToolExposureFixture(t, t.Context(), "platform_mcp_tools_attach_blocked")

	// Exactly the lock the exposure change holds across its decision.
	holder := testenv.BeginTx(t, ctx, fixture.conn)
	_, err := testrepo.New(holder).LockToolsetNowaitFixture(ctx, testrepo.LockToolsetNowaitFixtureParams{
		ProjectID: fixture.project.ID, Slug: fixture.toolsetSlug,
	})
	require.NoError(t, err)

	attach := testenv.BeginTx(t, ctx, fixture.conn)
	testenv.SetLockTimeout(t, ctx, attach, 2*time.Second)
	_, err = mcpserversrepo.New(attach).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID: uuid.New(), ProjectID: fixture.project.ID, Name: conv.ToPGText("Would join"),
		Slug:      conv.ToPGText("would-join-" + uuid.NewString()[:8]),
		ToolsetID: uuid.NullUUID{UUID: fixture.toolsetID, Valid: true}, Visibility: "private",
	})
	testenv.RequireLockNotAvailable(t, err)
	require.NoError(t, attach.Rollback(ctx))
	require.NoError(t, holder.Rollback(ctx))

	// With nobody holding the toolset row the same attach goes through, so the
	// refusal above was the lock and not the row being invalid.
	_, err = mcpserversrepo.New(fixture.conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID: uuid.New(), ProjectID: fixture.project.ID, Name: conv.ToPGText("Joins freely"),
		Slug:      conv.ToPGText("joins-" + uuid.NewString()[:8]),
		ToolsetID: uuid.NullUUID{UUID: fixture.toolsetID, Valid: true}, Visibility: "private",
	})
	require.NoError(t, err)
}

// Lock ORDER, not lock presence. toolsets.UpdateToolset holds the toolset row
// (GetToolsetForUpdate) and then, inside hostedmcp.Sync, locks the hosted
// mcp_servers row FOR UPDATE (LockMCPServerByIDAndProjectID) before writing
// it. For a hosted server both ids are the toolset id, so that is the same
// pair of rows this path
// touches. Taking them servers-first here would be an ABBA cycle that
// PostgreSQL breaks by aborting one side with deadlock_detected, turning a
// concurrent dashboard edit and tool-exposure change into a failed request.
//
// So this asserts the exposure path waits on the TOOLSET row while holding no
// lock on the server row.
//
// Falsification: lock the server row before the toolset in the Mutate closure
// and the NOWAIT probe below fails with lock_not_available, because the
// mutation is then parked on the toolset while holding the server row — which
// is exactly the cycle.
func TestChangeMCPToolsTakesToolsetAndServerLocksInDashboardOrder(t *testing.T) {
	t.Parallel()
	ctx, fixture := seedToolExposureFixture(t, t.Context(), "platform_mcp_tools_lock_order")

	current, err := fixture.service.Exposure(ctx, fixture.principal, fixture.project.ID, fixture.toolsetID)
	require.NoError(t, err)

	// Hold the toolset row the way UpdateToolset holds it first.
	probe := testenv.BeginTx(t, ctx, fixture.conn)
	_, err = testrepo.New(probe).LockToolsetNoKeyUpdateFixture(ctx, testrepo.LockToolsetNoKeyUpdateFixtureParams{
		ProjectID: fixture.project.ID, Slug: fixture.toolsetSlug,
	})
	require.NoError(t, err)

	result := make(chan error, 1)
	go func() {
		_, addErr := fixture.add(t, ctx, current.ExposureVersion, fixture.tools[0])
		result <- addErr
	}()

	// It parks on the toolset lock, which is the first of the two.
	testenv.WaitForQueryBlockedBy(t, ctx, fixture.conn, testenv.BackendPID(probe), "%LockPlatformMCPToolsetForToolExposure%")

	// And while parked there it holds nothing on the server row, so a writer
	// coming the other way — toolset first, then server — never waits on this
	// transaction for a row it already has.
	serverProbe := testenv.BeginTx(t, ctx, fixture.conn)
	_, err = testrepo.New(serverProbe).LockMCPServerRowNowaitFixture(ctx, testrepo.LockMCPServerRowNowaitFixtureParams{
		ID: fixture.toolsetID, ProjectID: fixture.project.ID,
	})
	require.NoError(t, err, "the exposure path must not hold the server row while waiting for the toolset row")
	require.NoError(t, serverProbe.Rollback(ctx))

	// Released, the change goes through — the wait was ordering, not a refusal.
	require.NoError(t, probe.Commit(ctx))
	require.NoError(t, <-result)
}

// The audit write is inside the mutation's transaction precisely so it cannot
// drift from the change, and nothing else in this suite would notice if it
// were dropped: every other assertion reads the committed tool list, which a
// change with no audit record satisfies just as well.
func TestChangeMCPToolsRecordsTheToolsetUpdateAudit(t *testing.T) {
	t.Parallel()
	ctx, fixture := seedToolExposureFixture(t, t.Context(), "platform_mcp_tools_audit")

	current, err := fixture.service.Exposure(ctx, fixture.principal, fixture.project.ID, fixture.toolsetID)
	require.NoError(t, err)
	before, err := audittest.AuditLogCountByAction(ctx, fixture.conn, audit.ActionToolsetUpdate)
	require.NoError(t, err)

	applied, err := fixture.add(t, ctx, current.ExposureVersion, fixture.tools[0])
	require.NoError(t, err)
	require.Equal(t, "applied", applied.Outcome)

	after, err := audittest.AuditLogCountByAction(ctx, fixture.conn, audit.ActionToolsetUpdate)
	require.NoError(t, err)
	require.Equal(t, before+1, after, "an applied change records exactly one toolset update")

	record, err := audittest.LatestAuditLogByAction(ctx, fixture.conn, audit.ActionToolsetUpdate)
	require.NoError(t, err)
	require.Equal(t, fixture.principal.OrganizationID, record.OrganizationID)
	require.Equal(t, uuid.NullUUID{UUID: fixture.project.ID, Valid: true}, record.ProjectID)
	require.Contains(t, record.ActorID, fixture.principal.UserID, "the real caller is the actor, not the platform")
	require.Equal(t, fixture.toolsetID.String(), record.SubjectID, "the toolset the change wrote is the audited subject")

	// Both snapshots are present and the metadata carries the version the
	// change produced, so the record says what changed rather than only that
	// something did.
	require.NotEmpty(t, record.BeforeSnapshot)
	require.NotEmpty(t, record.AfterSnapshot)
	metadata, err := audittest.DecodeAuditData(record.Metadata)
	require.NoError(t, err)
	require.Equal(t, 1, applied.Exposure.ToolCount)
	require.Contains(t, metadata, "toolset_version_after")

	// A no-op writes no audit record: there was no change to describe.
	noop, err := fixture.add(t, ctx, applied.Exposure.ExposureVersion, fixture.tools[0])
	require.NoError(t, err)
	require.Equal(t, "no_op", noop.Outcome)
	unchanged, err := audittest.AuditLogCountByAction(ctx, fixture.conn, audit.ActionToolsetUpdate)
	require.NoError(t, err)
	require.Equal(t, after, unchanged, "a no-op records nothing")
}

// Removal refuses a name it cannot act on for the same reason adding does: a
// silent "unchanged" for a mistyped URN reads exactly like "that tool was
// already gone". A tool the server really exposes stays removable even once
// the project stopped producing it.
func TestRemoveToolsFromMCPRefusesAnUnknownToolButNotAnOrphanedOne(t *testing.T) {
	t.Parallel()
	ctx, fixture := seedToolExposureFixture(t, t.Context(), "platform_mcp_remove_tools_unknown")

	// A tool the server exposes whose definition this project never produced:
	// the source it came from has since been removed from the deployment.
	const orphan = "tools:function:retired:archive_order"
	require.NoError(t, testrepo.New(fixture.conn).InsertToolsetVersionFixture(ctx, testrepo.InsertToolsetVersionFixtureParams{
		ToolsetID: fixture.toolsetID, ProjectID: fixture.project.ID, OrganizationID: fixture.principal.OrganizationID,
		Version: 1, ToolUrns: []string{fixture.tools[0], orphan},
	}))
	current, err := fixture.service.Exposure(ctx, fixture.principal, fixture.project.ID, fixture.toolsetID)
	require.NoError(t, err)

	remove := func(version string, urns ...string) (MCPToolExposureMutationOutput, error) {
		return fixture.service.RemoveTools(ctx, fixture.principal, ChangeMCPToolsInput{
			ProjectID: fixture.project.ID.String(), MCPID: fixture.toolsetID.String(), ToolURNs: urns,
			ExpectedVersion: version, IdempotencyKey: uuid.NewString(), Confirmed: true,
		})
	}

	_, err = remove(current.ExposureVersion, "tools:function:orders:refund_order")
	var refusal *MCPToolExposureError
	require.ErrorAs(t, err, &refusal)
	require.Equal(t, "invalid_request", refusal.Code)
	require.Equal(t, []string{"tools:function:orders:refund_order"}, refusal.UnknownTools)

	unchanged, err := fixture.service.Exposure(ctx, fixture.principal, fixture.project.ID, fixture.toolsetID)
	require.NoError(t, err)
	require.Equal(t, current.ExposureVersion, unchanged.ExposureVersion, "a refused removal changes nothing")

	applied, err := remove(current.ExposureVersion, orphan)
	require.NoError(t, err)
	require.Equal(t, "applied", applied.Outcome)
	require.Equal(t, []string{orphan}, applied.Applied)
	require.Equal(t, []string{fixture.tools[0]}, applied.Exposure.ToolURNs)
}

// Idempotency has to survive the target going away, or a client that never
// saw the first response has no safe way to find out whether it landed.
func TestAddToolsToMCPReplaysAfterTheTargetIsUnlinked(t *testing.T) {
	t.Parallel()
	ctx, fixture := seedToolExposureFixture(t, t.Context(), "platform_mcp_add_tools_replay_unlinked")

	current, err := fixture.service.Exposure(ctx, fixture.principal, fixture.project.ID, fixture.toolsetID)
	require.NoError(t, err)
	input := ChangeMCPToolsInput{
		ProjectID: fixture.project.ID.String(), MCPID: fixture.toolsetID.String(),
		ToolURNs: []string{fixture.tools[0]}, ExpectedVersion: current.ExposureVersion,
		IdempotencyKey: uuid.NewString(), Confirmed: true,
	}
	first, err := fixture.service.AddTools(ctx, fixture.principal, input)
	require.NoError(t, err)
	require.Equal(t, "applied", first.Outcome)

	_, err = mcpserversrepo.New(fixture.conn).DeleteMCPServer(ctx, mcpserversrepo.DeleteMCPServerParams{
		ID: fixture.toolsetID, ProjectID: fixture.project.ID,
	})
	require.NoError(t, err)

	replay, err := fixture.service.AddTools(ctx, fixture.principal, input)
	require.NoError(t, err, "the stored result is returned even though the target is gone")
	require.True(t, replay.Receipt.Replayed)
	require.Equal(t, first.Receipt.ID, replay.Receipt.ID)
	require.Equal(t, "applied", replay.Outcome)
	require.Equal(t, []string{fixture.tools[0]}, replay.Applied)
	require.Equal(t, "verification_unavailable", replay.SnapshotScope, "the post-commit read honestly reports it could not confirm")
}

// The unavailable registration must advertise exactly what a composed
// deployment advertises, so a tool never appears and disappears as a rollout
// flips. The live side is a real service, or the two paths would install the
// same handler and the comparison would prove nothing.
func TestToolExposureUnavailableRegistrationMatchesLiveManifest(t *testing.T) {
	t.Parallel()
	_, fixture := seedToolExposureFixture(t, t.Context(), "platform_mcp_tool_exposure_manifest")
	require.True(t, fixture.service.valid(), "the live side must be a composed service")

	reader := NewPostgresReader(testenv.NewLogger(t), fixture.conn)
	describe := func(service *MCPToolExposureService) map[string]Descriptor {
		registrar := newRegistrar(mcp.NewServer(&mcp.Implementation{Name: "tool-exposure-manifest", Version: "0.0.1"}, nil))
		registerToolExposureTools(registrar, service, reader)
		byName := map[string]Descriptor{}
		for _, descriptor := range registrar.Descriptors() {
			byName[descriptor.Name] = descriptor
		}
		return byName
	}

	live := describe(fixture.service)
	unavailable := describe(nil)
	require.Len(t, unavailable, 3)
	require.Len(t, live, len(unavailable))
	for name, descriptor := range unavailable {
		other, ok := live[name]
		require.True(t, ok, "tool %q is registered on both paths", name)
		require.Equal(t, other.Title, descriptor.Title)
		require.Equal(t, other.Description, descriptor.Description)
		require.Equal(t, other.Meta, descriptor.Meta)
		require.Equal(t, other.Annotations, descriptor.Annotations)
		require.Equal(t, other.InputSchema, descriptor.InputSchema)
	}

	require.Equal(t, bothAudiences, unavailable[listProjectToolsToolName].Meta.Audiences)
	require.Equal(t, ExternalAuthorizationMember, unavailable[listProjectToolsToolName].Meta.Authorization)
	require.True(t, unavailable[listProjectToolsToolName].Annotations.ReadOnlyHint)
	for _, name := range []string{addToolsToMCPToolName, removeToolsFromMCPToolName} {
		require.Equal(t, externalOnly, unavailable[name].Meta.Audiences, "%s", name)
		require.Equal(t, ExternalAuthorizationOrgAdmin, unavailable[name].Meta.Authorization, "%s", name)
		require.Equal(t, ProjectScopeExplicit, unavailable[name].Meta.ProjectScope, "%s", name)
		require.Contains(t, unavailable[name].Description, "republishes every plugin that carries the server",
			"%s must state the blast radius before it is called", name)
		require.Contains(t, unavailable[name].Description, "confirmed: true", "%s", name)
	}

	// The refusals themselves must differ: a caller that only asked to list a
	// project's tools must not be told it cannot change a server.
	listRefusal := invokeUnavailable(t, unavailable[listProjectToolsToolName], map[string]any{
		"project_id": fixture.project.ID.String(),
	})
	addRefusal := invokeUnavailable(t, unavailable[addToolsToMCPToolName], map[string]any{
		"project_id": fixture.project.ID.String(), "mcp_id": fixture.toolsetID.String(),
		"tool_urns": []string{fixture.tools[0]}, "expected_version": strings.Repeat("a", 64),
		"idempotency_key": uuid.NewString(), "confirmed": true,
	})
	require.Contains(t, listRefusal, "Listing a project's tools")
	require.Contains(t, addRefusal, "Changing which tools an MCP server exposes")
	require.NotEqual(t, listRefusal, addRefusal)
}

func invokeUnavailable(t *testing.T, descriptor Descriptor, arguments map[string]any) string {
	t.Helper()
	encoded, err := json.Marshal(arguments)
	require.NoError(t, err)
	// Through the exported accessor, so the nil guard it exists for is part of
	// what this exercises rather than bypassed.
	_, err = descriptor.Invoke(t.Context(), encoded)
	require.Error(t, err)
	var refusal *ToolRefusalError
	require.ErrorAs(t, err, &refusal)
	return refusal.Payload
}
