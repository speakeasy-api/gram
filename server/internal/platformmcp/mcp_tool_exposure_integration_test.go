package platformmcp

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/plugins"
	remotemcprepo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
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
	grants    []authz.Grant
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
	service, err := NewMCPToolExposureService(testenv.NewLogger(t), conn, audit.NewLogger(), engine, admin, "tool-exposure-cursor-key", plugins.PublicationRequests{}, nil)
	require.NoError(t, err)

	return ctx, toolExposureFixture{principal: principal, project: project, toolsetID: toolset.ID, service: service, conn: conn, tools: tools, grants: grants}
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
	_, err = descriptor.invoke(t.Context(), encoded)
	require.Error(t, err)
	var refusal *ToolRefusalError
	require.ErrorAs(t, err, &refusal)
	return refusal.Payload
}
