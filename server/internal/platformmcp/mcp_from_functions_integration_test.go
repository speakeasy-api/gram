package platformmcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	mcpserversgen "github.com/speakeasy-api/gram/server/gen/mcp_servers"
	toolsetsgen "github.com/speakeasy-api/gram/server/gen/toolsets"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/networkaccess"
	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
	"github.com/speakeasy-api/gram/server/internal/plugins"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
	"github.com/speakeasy-api/gram/server/internal/ratelimit"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/toolsets"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

// createFromFunctionsContext adds the grant the dashboard's own create checks:
// mcp:write keyed on the project, since the server does not exist yet.
func (f toolExposureFixture) createFromFunctionsContext(ctx context.Context) context.Context {
	return f.grantMCP(ctx, f.project.ID)
}

func (f toolExposureFixture) createInput(name string, urns ...string) CreateMCPFromFunctionsInput {
	return CreateMCPFromFunctionsInput{
		ProjectID: f.project.ID.String(), Name: name, ToolURNs: urns,
		IdempotencyKey: uuid.NewString(), Confirmed: true,
	}
}

// projectServerCounts reports how many live toolsets and server records the
// project holds, so a refusal can be shown to have created nothing at all.
func (f toolExposureFixture) projectServerCounts(t *testing.T, ctx context.Context) (int, int) {
	t.Helper()
	toolsetRows, err := toolsetsrepo.New(f.conn).ListToolsetsByProject(ctx, f.project.ID)
	require.NoError(t, err)
	serverRows, err := mcpserversrepo.New(f.conn).ListMCPServersByOrganizationID(ctx, f.principal.OrganizationID)
	require.NoError(t, err)
	serverCount := 0
	for _, server := range serverRows {
		if server.ProjectID == f.project.ID {
			serverCount++
		}
	}
	return len(toolsetRows), serverCount
}

func TestCreateMCPFromFunctionsCreatesAServerExposingExactlyTheRequestedTools(t *testing.T) {
	t.Parallel()
	ctx, fixture := seedToolExposureFixture(t, t.Context(), "platform_mcp_create_from_functions")
	ctx = fixture.createFromFunctionsContext(ctx)

	toolsetsBefore, err := audittest.AuditLogCountByAction(ctx, fixture.conn, audit.ActionToolsetCreate)
	require.NoError(t, err)
	serversBefore, err := audittest.AuditLogCountByAction(ctx, fixture.conn, audit.ActionMcpServerCreate)
	require.NoError(t, err)

	created, err := fixture.service.CreateMCPFromFunctions(ctx, fixture.principal, fixture.createInput("Order Desk", fixture.tools[0], fixture.tools[2]))
	require.NoError(t, err)
	require.Equal(t, "created", created.Outcome)
	require.Equal(t, "Order Desk", created.MCPName)
	require.Equal(t, mcpservers.VisibilityPrivate, created.Visibility, "a new server reaches nobody until a plugin carries it")
	require.Regexp(t, `^order-desk-[0-9a-f]+$`, created.MCPSlug, "the slug follows the management API's rule for a server name")
	require.False(t, created.AddedToDefaultPlugin, "the organization already has a server, so this one joins no plugin")
	require.False(t, created.PublicationRequested)
	require.False(t, created.Receipt.Replayed)
	require.Equal(t, "fresh_read_after_commit", created.SnapshotScope)
	require.NotNil(t, created.Exposure)
	require.ElementsMatch(t, []string{fixture.tools[0], fixture.tools[2]}, created.Exposure.ToolURNs)

	// Listable afterwards through the same read get_mcp uses, with exactly the
	// requested tools — and the existing tools take over from here.
	mcpID := uuid.MustParse(created.MCPID)
	read, err := fixture.service.Exposure(ctx, fixture.principal, fixture.project.ID, mcpID)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{fixture.tools[0], fixture.tools[2]}, read.ToolURNs)
	require.Equal(t, 1, read.SharedWithOther, "the new toolset's canonical hosted wrapper offers the same tools")

	// The server and the toolset behind it are in the same project.
	server, err := mcpserversrepo.New(fixture.conn).GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{ID: mcpID, ProjectID: fixture.project.ID})
	require.NoError(t, err)
	toolsetID := uuid.MustParse(read.ToolsetID)
	require.Equal(t, uuid.NullUUID{UUID: toolsetID, Valid: true}, server.ToolsetID)
	toolset, err := toolsetsrepo.New(fixture.conn).GetToolsetByIDAndProject(ctx, toolsetsrepo.GetToolsetByIDAndProjectParams{ID: toolsetID, ProjectID: fixture.project.ID})
	require.NoError(t, err)
	require.Equal(t, fixture.project.ID, toolset.ProjectID)

	// The index trigger ran for the toolset this creation wrote. This
	// organization already has a server, so the new toolset is not
	// MCP-enabled, but the server record this creation wrote serves it
	// regardless, so dynamic mode needs the index and it is requested.
	require.Equal(t, "requested", created.IndexSignal)
	require.Equal(t, []uuid.UUID{toolsetID}, *fixture.indexed)

	// The audit trail is the dashboard's: one toolset create, plus the
	// toolset's canonical wrapper and the new server, attributed to the caller.
	toolsetsAfter, err := audittest.AuditLogCountByAction(ctx, fixture.conn, audit.ActionToolsetCreate)
	require.NoError(t, err)
	serversAfter, err := audittest.AuditLogCountByAction(ctx, fixture.conn, audit.ActionMcpServerCreate)
	require.NoError(t, err)
	require.Equal(t, toolsetsBefore+1, toolsetsAfter)
	require.Equal(t, serversBefore+2, serversAfter)
	record, err := audittest.LatestAuditLogByAction(ctx, fixture.conn, audit.ActionMcpServerCreate)
	require.NoError(t, err)
	require.Equal(t, created.MCPID, record.SubjectID)
	require.Contains(t, record.ActorID, fixture.principal.UserID)
}

func TestCreateMCPFromFunctionsReportsTheIndexOutcome(t *testing.T) {
	t.Parallel()
	ctx, fixture := seedToolExposureFixture(t, t.Context(), "platform_mcp_create_from_functions_index")
	ctx = fixture.createFromFunctionsContext(ctx)

	for _, tc := range []struct {
		name   string
		index  ToolExposureIndexer
		signal string
	}{
		{name: "Not Required", index: func(context.Context, uuid.UUID, uuid.UUID) error { return toolsets.ErrToolsetIndexNotRequired }, signal: "not_required"},
		{name: "Unschedulable", index: func(context.Context, uuid.UUID, uuid.UUID) error { return toolsets.ErrToolsetIndexUnavailable }, signal: "unavailable"},
		{name: "Failed", index: func(context.Context, uuid.UUID, uuid.UUID) error { return errors.New("temporal down") }, signal: "request_failed"},
		{name: "Absent", index: nil, signal: "unavailable"},
	} {
		fixture.service.WithIndexing(tc.index)
		created, err := fixture.service.CreateMCPFromFunctions(ctx, fixture.principal, fixture.createInput(tc.name, fixture.tools[1]))
		require.NoError(t, err, tc.name)
		require.Equal(t, tc.signal, created.IndexSignal, tc.name)
		require.Equal(t, "created", created.Outcome, "a rebuild that did not start does not undo the creation (%s)", tc.name)
	}
}

func TestCreateMCPFromFunctionsRefusesUndeployedToolsByNameAndCreatesNothing(t *testing.T) {
	t.Parallel()
	ctx, fixture := seedToolExposureFixture(t, t.Context(), "platform_mcp_create_from_functions_unknown")
	ctx = fixture.createFromFunctionsContext(ctx)
	toolsetsBefore, serversBefore := fixture.projectServerCounts(t, ctx)

	pushedLater := "tools:function:orders:refund_order"
	_, err := fixture.service.CreateMCPFromFunctions(ctx, fixture.principal, fixture.createInput("Order Desk", fixture.tools[0], pushedLater))
	var refusal *MCPToolExposureError
	require.ErrorAs(t, err, &refusal)
	require.Equal(t, "invalid_request", refusal.Code)
	require.Equal(t, []string{pushedLater}, refusal.UnknownTools, "only the tool the deployment does not produce is named")
	require.Contains(t, refusal.Message, "a tool pushed after that deployment finished is not available until the new one completes")

	// A tool generated from an API document is not a function tool.
	fromDocument := "tools:http:petstore:list_pets"
	_, err = fixture.service.CreateMCPFromFunctions(ctx, fixture.principal, fixture.createInput("Order Desk", fixture.tools[0], fromDocument))
	require.ErrorAs(t, err, &refusal)
	require.Equal(t, []string{fromDocument}, refusal.UnknownTools)
	require.Contains(t, refusal.Message, "API document", "the refusal says why, rather than claiming the tool is undeployed")

	toolsetsAfter, serversAfter := fixture.projectServerCounts(t, ctx)
	require.Equal(t, toolsetsBefore, toolsetsAfter, "a refused creation leaves no toolset behind")
	require.Equal(t, serversBefore, serversAfter, "a refused creation leaves no server behind")
	require.Empty(t, *fixture.indexed)
}

func TestCreateMCPFromFunctionsReplaysOneIdempotencyKeyWithoutASecondServer(t *testing.T) {
	t.Parallel()
	ctx, fixture := seedToolExposureFixture(t, t.Context(), "platform_mcp_create_from_functions_replay")
	ctx = fixture.createFromFunctionsContext(ctx)
	toolsetsBefore, serversBefore := fixture.projectServerCounts(t, ctx)

	input := fixture.createInput("Order Desk", fixture.tools[0])
	first, err := fixture.service.CreateMCPFromFunctions(ctx, fixture.principal, input)
	require.NoError(t, err)
	replay, err := fixture.service.CreateMCPFromFunctions(ctx, fixture.principal, input)
	require.NoError(t, err)
	require.True(t, replay.Receipt.Replayed)
	require.Equal(t, first.Receipt.ID, replay.Receipt.ID)
	require.Equal(t, first.MCPID, replay.MCPID, "the replay reports the server already created")

	toolsetsAfter, serversAfter := fixture.projectServerCounts(t, ctx)
	require.Equal(t, toolsetsBefore+1, toolsetsAfter, "one toolset, not two")
	require.Equal(t, serversBefore+2, serversAfter, "one server plus the toolset's canonical wrapper, not a second pair")

	// Once the write allowance is spent, a retry of the creation that already
	// committed still returns its stored result: the charge is only taken when
	// the receipt lookup misses. A new creation is throttled.
	fixture.service.changes = OperationBudget{Connection: denyOperationLimiter{}, Organization: denyOperationLimiter{}}
	throttledReplay, err := fixture.service.CreateMCPFromFunctions(ctx, fixture.principal, input)
	require.NoError(t, err, "a replay never spends the allowance")
	require.True(t, throttledReplay.Receipt.Replayed)
	require.Equal(t, first.MCPID, throttledReplay.MCPID)
	_, err = fixture.service.CreateMCPFromFunctions(ctx, fixture.principal, fixture.createInput("Second Desk", fixture.tools[1]))
	var throttled *MCPToolExposureError
	require.ErrorAs(t, err, &throttled)
	require.Equal(t, "rate_limited", throttled.Code)
	fixture.service.changes = testOperationBudget()

	// The same key with a different request is a conflict, never a second
	// server under an old receipt.
	changed := input
	changed.Name = "Another Desk"
	_, err = fixture.service.CreateMCPFromFunctions(ctx, fixture.principal, changed)
	var refusal *MCPToolExposureError
	require.ErrorAs(t, err, &refusal)
	require.Equal(t, "conflict", refusal.Code)
}

func TestCreateMCPFromFunctionsRefusesWithoutConfirmationOrAdmin(t *testing.T) {
	t.Parallel()
	ctx, fixture := seedToolExposureFixture(t, t.Context(), "platform_mcp_create_from_functions_refusals")
	ctx = fixture.createFromFunctionsContext(ctx)
	toolsetsBefore, serversBefore := fixture.projectServerCounts(t, ctx)

	// Unconfirmed is a preview: it creates nothing and charges nothing, so it
	// answers even with the write allowance spent.
	fixture.service.changes = OperationBudget{Connection: denyOperationLimiter{}, Organization: denyOperationLimiter{}}
	unconfirmed := fixture.createInput("Order Desk", fixture.tools[0])
	unconfirmed.Confirmed = false
	_, err := fixture.service.CreateMCPFromFunctions(ctx, fixture.principal, unconfirmed)
	var refusal *MCPToolExposureError
	require.ErrorAs(t, err, &refusal)
	require.Equal(t, "confirmation_required", refusal.Code)
	require.NotNil(t, refusal.Preview)
	require.Equal(t, "order-desk", refusal.Preview.ToolsetSlug)
	fixture.service.changes = testOperationBudget()

	// A live member who is not an organization administrator is refused with
	// the org:admin challenge, even holding mcp:write on the project.
	member := fixture.principal
	member.UserID = "member-" + uuid.NewString()[:8]
	seedPlatformMCPAuthorizationMember(t, ctx, fixture.conn, member.OrganizationID, member.UserID, authz.SystemRoleMember)
	_, err = fixture.service.CreateMCPFromFunctions(ctx, member, fixture.createInput("Order Desk", fixture.tools[0]))
	var denied *ExternalAuthorizationError
	require.ErrorAs(t, err, &denied)
	require.Equal(t, string(authz.ScopeOrgAdmin), denied.RequiredScope)

	// An administrator without mcp:write on the project gets the same denial
	// for a real project and an invented one.
	withoutWrite := authz.GrantsToContext(ctx, fixture.grants)
	for _, projectID := range []uuid.UUID{fixture.project.ID, uuid.New()} {
		input := fixture.createInput("Order Desk", fixture.tools[0])
		input.ProjectID = projectID.String()
		_, err = fixture.service.CreateMCPFromFunctions(withoutWrite, fixture.principal, input)
		require.ErrorAs(t, err, &denied)
		require.Equal(t, string(authz.ScopeMCPWrite), denied.RequiredScope)
	}

	toolsetsAfter, serversAfter := fixture.projectServerCounts(t, ctx)
	require.Equal(t, toolsetsBefore, toolsetsAfter)
	require.Equal(t, serversBefore, serversAfter)
}

// The preview is computed by the same functions the creation uses, so the slug
// a user confirms is the slug that gets created, however awkward the name.
func TestCreateMCPFromFunctionsPreviewMatchesTheCreatedSlugs(t *testing.T) {
	t.Parallel()
	ctx, fixture := seedToolExposureFixture(t, t.Context(), "platform_mcp_create_from_functions_preview")
	ctx = fixture.createFromFunctionsContext(ctx)

	for _, name := range []string{"Café Team", "  --Weird__Name!!  ", "Ünïcödé 123", "a   b", strings.Repeat("x", maxMCPNameLength)} {
		preview := fixture.createInput(name, fixture.tools[0])
		preview.Confirmed = false
		_, err := fixture.service.CreateMCPFromFunctions(ctx, fixture.principal, preview)
		var refusal *MCPToolExposureError
		require.ErrorAs(t, err, &refusal, name)
		require.Equal(t, "confirmation_required", refusal.Code, name)
		require.NotNil(t, refusal.Preview, name)

		created, err := fixture.service.CreateMCPFromFunctions(ctx, fixture.principal, fixture.createInput(name, fixture.tools[0]))
		require.NoError(t, err, name)
		require.NotNil(t, created.Exposure, name)
		require.Equal(t, refusal.Preview.ToolsetSlug, created.Exposure.ToolsetSlug, "%q: the previewed tool list slug is the created one", name)
		require.True(t, strings.HasPrefix(created.MCPSlug, refusal.Preview.MCPSlugPrefix+"-"), "%q: server slug %q starts with the previewed %q", name, created.MCPSlug, refusal.Preview.MCPSlugPrefix)
		require.Equal(t, refusal.Preview.Name, created.MCPName, "%q: the name is kept whole", name)
	}
}

// An organization's first server joins the project's Default plugin on
// creation, as it does from the dashboard, so the preview has to warn about
// it and the result has to say it happened.
func TestCreateMCPFromFunctionsReportsTheFirstServerJoiningTheDefaultPlugin(t *testing.T) {
	t.Parallel()
	ctx, fixture := seedToolExposureFixture(t, t.Context(), "platform_mcp_create_from_functions_first_server")
	ctx = fixture.createFromFunctionsContext(ctx)
	// Leave the organization with no enabled server, so the next is its first.
	require.NoError(t, toolsetsrepo.New(fixture.conn).SetToolsetMCPEnabledByID(ctx, toolsetsrepo.SetToolsetMCPEnabledByIDParams{
		McpEnabled: false, ID: fixture.toolsetID, ProjectID: fixture.project.ID,
	}))

	input := fixture.createInput("First Desk", fixture.tools[0])
	input.Confirmed = false
	_, err := fixture.service.CreateMCPFromFunctions(ctx, fixture.principal, input)
	var refusal *MCPToolExposureError
	require.ErrorAs(t, err, &refusal)
	require.Equal(t, "confirmation_required", refusal.Code)
	require.True(t, refusal.Preview.WouldJoinDefaultPlugin, "the preview warns before the user confirms")

	input.Confirmed = true
	created, err := fixture.service.CreateMCPFromFunctions(ctx, fixture.principal, input)
	require.NoError(t, err)
	require.True(t, created.AddedToDefaultPlugin, "the result reports the Default plugin membership the attach made")
	// An enabled toolset is one the real trigger indexes.
	require.Equal(t, "requested", created.IndexSignal)
	// Publication requests are disabled in this fixture and no publisher is
	// composed, so the refresh could not be asked for, and the result says so
	// rather than claiming it was.
	require.False(t, created.PublicationRequested)
	require.Equal(t, "unavailable", created.PublishSignal)

	defaultPlugin, err := pluginsrepo.New(fixture.conn).GetDefaultPlugin(ctx, pluginsrepo.GetDefaultPluginParams{
		OrganizationID: fixture.principal.OrganizationID, ProjectID: fixture.project.ID,
	})
	require.NoError(t, err)
	members, err := pluginsrepo.New(fixture.conn).ListPluginServers(ctx, defaultPlugin.ID)
	require.NoError(t, err)
	toolsetID := uuid.MustParse(created.Exposure.ToolsetID)
	joined := false
	for _, member := range members {
		joined = joined || member.ToolsetID == uuid.NullUUID{UUID: toolsetID, Valid: true}
	}
	require.True(t, joined, "the new tool list is in the Default plugin")

	// A second server is not the first, so it joins nothing.
	second, err := fixture.service.CreateMCPFromFunctions(ctx, fixture.principal, fixture.createInput("Second Desk", fixture.tools[1]))
	require.NoError(t, err)
	require.False(t, second.AddedToDefaultPlugin)
	require.False(t, second.PublicationRequested)
	require.Equal(t, "not_requested", second.PublicationRequest)
}

type recordingPublishSignaler struct{ projects []uuid.UUID }

func (r *recordingPublishSignaler) SignalPluginPublish(_ context.Context, projectID uuid.UUID, _ string) error {
	r.projects = append(r.projects, projectID)
	return nil
}

// With publication enabled but no marketplace connection yet, the request is
// recorded as not_configured. The publish can still create that first
// repository, so the first server must request it promptly, as the dashboard
// does, rather than wait for the periodic sweep.
func TestCreateMCPFromFunctionsRequestsTheFirstPublishWithoutAMarketplaceConnection(t *testing.T) {
	t.Parallel()
	ctx, fixture := seedToolExposureFixture(t, t.Context(), "platform_mcp_create_from_functions_first_publish")
	ctx = fixture.createFromFunctionsContext(ctx)
	require.NoError(t, toolsetsrepo.New(fixture.conn).SetToolsetMCPEnabledByID(ctx, toolsetsrepo.SetToolsetMCPEnabledByIDParams{
		McpEnabled: false, ID: fixture.toolsetID, ProjectID: fixture.project.ID,
	}))
	signaler := &recordingPublishSignaler{}
	fixture.service.publication = plugins.PublicationRequests{Enabled: true}
	fixture.service.publisher = signaler

	created, err := fixture.service.CreateMCPFromFunctions(ctx, fixture.principal, fixture.createInput("First Desk", fixture.tools[0]))
	require.NoError(t, err)
	require.True(t, created.AddedToDefaultPlugin)
	require.Equal(t, string(plugins.ProjectPublicationNotConfigured), created.PublicationRequest, "the project has no marketplace connection yet")
	require.Equal(t, []uuid.UUID{fixture.project.ID}, signaler.projects, "the initial publish was requested for the project")
	require.Equal(t, "best_effort_requested", created.PublishSignal)
	require.True(t, created.PublicationRequested)
}

// Nothing downstream rewrites an unrecognised tool error, so a database
// failure on any path must reach the caller as the generic unavailable
// refusal, with the real cause kept in the server log.
func TestCreateMCPFromFunctionsNeverReturnsDatabaseErrorText(t *testing.T) {
	t.Parallel()
	ctx, fixture := seedToolExposureFixture(t, t.Context(), "platform_mcp_create_from_functions_db_failure")
	ctx = fixture.createFromFunctionsContext(ctx)

	broken, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_create_from_functions_db_failure_closed")
	require.NoError(t, err)
	var logged bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logged, nil))
	// The authorization engine reads the live database, so the call clears
	// authorization and fails on the service's own first query instead.
	engine := authz.NewEngine(testenv.NewLogger(t), fixture.conn, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient())
	service, err := NewMCPToolExposureService(logger, broken, audit.NewLogger(), engine, stubAuthorizer{err: nil}, "tool-exposure-cursor-key", plugins.PublicationRequests{}, nil, testOperationBudget(), testOperationBudget())
	require.NoError(t, err)
	// Every query on a closed pool fails with a driver error, which is what a
	// database outage looks like from here.
	broken.Close()

	registrar := newRegistrar(mcp.NewServer(&mcp.Implementation{Name: "create-from-functions-db-failure", Version: "0.0.1"}, nil))
	registerCreateMCPFromFunctionsTool(registrar, service)
	descriptor := registrar.Descriptors()[0]
	ctx = ContextWithPrincipal(ctx, fixture.principal)

	for _, confirmed := range []bool{true, false} {
		arguments, err := json.Marshal(map[string]any{
			"project_id": fixture.project.ID.String(), "name": "Order Desk", "tool_urns": []string{fixture.tools[0]},
			"idempotency_key": uuid.NewString(), "confirmed": confirmed,
		})
		require.NoError(t, err)
		_, err = descriptor.Invoke(ctx, arguments)
		var refusal *ToolRefusalError
		require.ErrorAs(t, err, &refusal, "confirmed=%t reaches a refusal, not a raw error", confirmed)
		text := strings.ToLower(refusal.Payload)
		require.Contains(t, text, unavailableCode, "confirmed=%t", confirmed)
		for _, leaked := range []string{"pool", "closed", "sql", "pgx", "postgres", "relation", "constraint"} {
			require.NotContains(t, text, leaked, "confirmed=%t: the refusal must not carry database error text", confirmed)
		}
	}
	require.Contains(t, logged.String(), "closed", "the server log keeps the underlying cause")
}

// One idempotency key spans a creation: the preview records nothing under it,
// so the confirmed call with that same key creates exactly one server.
func TestCreateMCPFromFunctionsPreviewAndConfirmShareOneKey(t *testing.T) {
	t.Parallel()
	ctx, fixture := seedToolExposureFixture(t, t.Context(), "platform_mcp_create_from_functions_one_key")
	ctx = fixture.createFromFunctionsContext(ctx)
	toolsetsBefore, serversBefore := fixture.projectServerCounts(t, ctx)

	input := fixture.createInput("Order Desk", fixture.tools[0])
	input.Confirmed = false
	_, err := fixture.service.CreateMCPFromFunctions(ctx, fixture.principal, input)
	var refusal *MCPToolExposureError
	require.ErrorAs(t, err, &refusal)
	require.Equal(t, "confirmation_required", refusal.Code)
	_, err = fixture.service.queries.GetPlatformMCPOperationReceipt(ctx, platformrepo.GetPlatformMCPOperationReceiptParams{
		OrganizationID: fixture.principal.OrganizationID, ProjectID: fixture.project.ID, Operation: operationCreateMCPFromFunctions,
		IdempotencyKey: input.IdempotencyKey, UserID: conv.ToPGText(fixture.principal.UserID), SubjectUrn: userSubjectURN(fixture.principal.UserID),
	})
	require.ErrorIs(t, err, pgx.ErrNoRows, "the preview stores nothing under the key")

	input.Confirmed = true
	created, err := fixture.service.CreateMCPFromFunctions(ctx, fixture.principal, input)
	require.NoError(t, err)
	require.False(t, created.Receipt.Replayed, "the confirmed call creates rather than replaying the preview")
	require.NotNil(t, created.Exposure)
	require.Equal(t, []string{fixture.tools[0]}, created.Exposure.ToolURNs)

	toolsetsAfter, serversAfter := fixture.projectServerCounts(t, ctx)
	require.Equal(t, toolsetsBefore+1, toolsetsAfter, "exactly one toolset")
	require.Equal(t, serversBefore+2, serversAfter, "exactly one server plus the toolset's canonical wrapper")
}

// poolCheckingLimiter records how many connections the service's pool had
// checked out at the moment of each charge. Zero means the charge ran with no
// transaction open and no receipt lock held.
type poolCheckingLimiter struct {
	pool     interface{ Stat() *pgxpool.Stat }
	acquired *[]int32
}

func (l poolCheckingLimiter) Allow(context.Context, string) (ratelimit.Result, error) {
	*l.acquired = append(*l.acquired, l.pool.Stat().AcquiredConns())
	return ratelimit.Result{Allowed: true}, nil
}

func (l poolCheckingLimiter) AllowN(ctx context.Context, key string, _ int) (ratelimit.Result, error) {
	return l.Allow(ctx, key)
}

func TestCreateMCPFromFunctionsChargesOutsideAnyTransaction(t *testing.T) {
	t.Parallel()
	ctx, fixture := seedToolExposureFixture(t, t.Context(), "platform_mcp_create_from_functions_charge")
	ctx = fixture.createFromFunctionsContext(ctx)

	acquired := &[]int32{}
	limiter := poolCheckingLimiter{pool: fixture.conn, acquired: acquired}
	fixture.service.changes = OperationBudget{Connection: limiter, Organization: limiter}

	input := fixture.createInput("Order Desk", fixture.tools[0])
	_, err := fixture.service.CreateMCPFromFunctions(ctx, fixture.principal, input)
	require.NoError(t, err)
	require.NotEmpty(t, *acquired, "the creation was charged")
	for _, held := range *acquired {
		require.Zero(t, held, "the limiter is never consulted while a connection, transaction, or receipt lock is held")
	}

	charges := len(*acquired)
	_, err = fixture.service.CreateMCPFromFunctions(ctx, fixture.principal, input)
	require.NoError(t, err)
	// The replay itself is free, but re-sending its publish and index signals
	// is charged once (see chargeRerun), and that charge is outside any
	// transaction too.
	require.Len(t, *acquired, 2*charges, "a replay is charged only for the signals it re-sends")
	for _, held := range *acquired {
		require.Zero(t, held, "the replay's charge is never consulted while a connection, transaction, or receipt lock is held")
	}
}

// The tool is a second caller of the dashboard's authoring path, so what it
// leaves behind must be indistinguishable from what the dashboard's own
// sequence — create the toolset, seed its tools, create a private server in
// front of it — leaves behind through the real management handlers.
func TestCreateMCPFromFunctionsMatchesTheDashboardSequence(t *testing.T) {
	t.Parallel()
	ctx, fixture := seedToolExposureFixture(t, t.Context(), "platform_mcp_create_from_functions_parity")
	ctx = fixture.createFromFunctionsContext(ctx)

	viaTool, err := fixture.service.CreateMCPFromFunctions(ctx, fixture.principal, fixture.createInput("Tool Desk", fixture.tools[0], fixture.tools[1]))
	require.NoError(t, err)

	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)
	redisClient, err := platformMCPInfra.NewRedisClient(t, 0)
	require.NoError(t, err)
	sessionManager := testenv.NewTestManager(t, logger, tracerProvider, fixture.conn, redisClient, cache.Suffix("gram-local"), billing.NewStubClient(logger, tracerProvider))
	engine := authz.NewEngine(logger, fixture.conn, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient())
	toolsetService := toolsets.NewService(logger, tracerProvider, nil, fixture.conn, sessionManager, nil, engine, audit.NewLogger(), nil, false)
	serverService := mcpservers.NewService(logger, tracerProvider, fixture.conn, sessionManager, engine, audit.NewLogger(), nil, nil, false, nil, nil, networkaccess.DenyAllChecker{})
	orgSlug := projectOrganizationSlug(ctx, fixture.conn, fixture.principal.OrganizationID)
	dashboardCtx := contextvalues.SetAuthContext(ctx, &contextvalues.AuthContext{
		ActiveOrganizationID: fixture.principal.OrganizationID, OrganizationSlug: orgSlug,
		UserID: fixture.principal.UserID, ProjectID: &fixture.project.ID, ProjectSlug: &fixture.project.Slug,
	})

	created, err := toolsetService.CreateToolset(dashboardCtx, &toolsetsgen.CreateToolsetPayload{Name: "Dashboard Desk"})
	require.NoError(t, err)
	dashboardToolsetID := uuid.MustParse(created.ID)
	dashboardCtx = fixture.grantMCP(dashboardCtx, fixture.project.ID, dashboardToolsetID)
	_, err = toolsetService.UpdateToolset(dashboardCtx, &toolsetsgen.UpdateToolsetPayload{
		Slug: created.Slug, ToolUrns: []string{fixture.tools[0], fixture.tools[1]},
	})
	require.NoError(t, err)
	server, err := serverService.CreateMcpServer(dashboardCtx, &mcpserversgen.CreateMcpServerPayload{
		Name: created.Name, ToolsetID: &created.ID, Visibility: types.McpServerVisibility(mcpservers.VisibilityPrivate),
	})
	require.NoError(t, err)

	servers := mcpserversrepo.New(fixture.conn)
	toolRow, err := servers.GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{ID: uuid.MustParse(viaTool.MCPID), ProjectID: fixture.project.ID})
	require.NoError(t, err)
	dashboardRow, err := servers.GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{ID: uuid.MustParse(server.ID), ProjectID: fixture.project.ID})
	require.NoError(t, err)
	require.Equal(t, dashboardRow.Visibility, toolRow.Visibility)
	require.Equal(t, dashboardRow.NetworkAccessMode, toolRow.NetworkAccessMode)
	require.Equal(t, dashboardRow.EnvironmentID, toolRow.EnvironmentID)
	require.Equal(t, dashboardRow.UserSessionIssuerID, toolRow.UserSessionIssuerID)
	require.Equal(t, dashboardRow.ToolVariationsGroupID, toolRow.ToolVariationsGroupID)
	require.Equal(t, dashboardRow.RemoteMcpServerID, toolRow.RemoteMcpServerID)
	require.True(t, toolRow.ToolsetID.Valid)
	require.Equal(t, "Tool Desk", toolRow.Name.String, "the server is named after its toolset, as the dashboard names it")

	toolsetRows := toolsetsrepo.New(fixture.conn)
	toolToolset, err := toolsetRows.GetToolsetByIDAndProject(ctx, toolsetsrepo.GetToolsetByIDAndProjectParams{ID: toolRow.ToolsetID.UUID, ProjectID: fixture.project.ID})
	require.NoError(t, err)
	dashboardToolset, err := toolsetRows.GetToolsetByIDAndProject(ctx, toolsetsrepo.GetToolsetByIDAndProjectParams{ID: dashboardToolsetID, ProjectID: fixture.project.ID})
	require.NoError(t, err)
	require.Equal(t, dashboardToolset.McpEnabled, toolToolset.McpEnabled)
	require.Equal(t, dashboardToolset.DefaultEnvironmentSlug, toolToolset.DefaultEnvironmentSlug)
	require.Equal(t, dashboardToolset.Description, toolToolset.Description)
	require.Equal(t, dashboardToolset.ToolSelectionMode, toolToolset.ToolSelectionMode)
	require.Regexp(t, "^"+orgSlug+"-", toolToolset.McpSlug.String)

	toolExposure, err := fixture.service.Exposure(ctx, fixture.principal, fixture.project.ID, toolRow.ID)
	require.NoError(t, err)
	dashboardExposure, err := fixture.service.Exposure(ctx, fixture.principal, fixture.project.ID, dashboardRow.ID)
	require.NoError(t, err)
	require.ElementsMatch(t, dashboardExposure.ToolURNs, toolExposure.ToolURNs)
	require.Equal(t, dashboardExposure.ToolCount, toolExposure.ToolCount)
	toolVersion, err := toolsetRows.GetLatestToolsetVersion(ctx, toolToolset.ID)
	require.NoError(t, err)
	dashboardVersion, err := toolsetRows.GetLatestToolsetVersion(ctx, dashboardToolset.ID)
	require.NoError(t, err)
	require.Equal(t, dashboardVersion.Version, toolVersion.Version)
}

// The unavailable registration must advertise exactly what a composed
// deployment advertises.
func TestCreateMCPFromFunctionsUnavailableRegistrationMatchesLiveManifest(t *testing.T) {
	t.Parallel()
	_, fixture := seedToolExposureFixture(t, t.Context(), "platform_mcp_create_from_functions_manifest")
	require.True(t, fixture.service.valid())

	describe := func(service *MCPToolExposureService) Descriptor {
		registrar := newRegistrar(mcp.NewServer(&mcp.Implementation{Name: "create-from-functions-manifest", Version: "0.0.1"}, nil))
		registerCreateMCPFromFunctionsTool(registrar, service)
		descriptors := registrar.Descriptors()
		require.Len(t, descriptors, 1)
		return descriptors[0]
	}
	live, unavailable := describe(fixture.service), describe(nil)
	require.Equal(t, createMCPFromFunctionsToolName, unavailable.Name)
	require.Equal(t, live.Title, unavailable.Title)
	require.Equal(t, live.Description, unavailable.Description)
	require.Equal(t, live.Meta, unavailable.Meta)
	require.Equal(t, live.Annotations, unavailable.Annotations)
	require.Equal(t, live.InputSchema, unavailable.InputSchema)
	require.Equal(t, externalOnly, unavailable.Meta.Audiences)
	require.Equal(t, ExternalAuthorizationOrgAdmin, unavailable.Meta.Authorization)
	require.Equal(t, ProjectScopeExplicit, unavailable.Meta.ProjectScope)
	require.Contains(t, unavailable.Description, "confirmed: true")
	require.Contains(t, unavailable.Description, "everyone holding that plugin receives it", "the first-server exception is named")
	require.Contains(t, unavailable.Description, "requested, not confirmed delivered")

	refusal := invokeUnavailable(t, unavailable, map[string]any{
		"project_id": fixture.project.ID.String(), "name": "Order Desk", "tool_urns": []string{fixture.tools[0]},
		"idempotency_key": uuid.NewString(), "confirmed": true,
	})
	require.Contains(t, refusal, "Creating an MCP server from a project's functions")
}
