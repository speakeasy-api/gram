package platformmcp

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	telemetryrepo "github.com/speakeasy-api/gram/server/internal/telemetry/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// fixedIdentityGate folds every organization.
type fixedIdentityGate struct{}

func (fixedIdentityGate) CanonicalOrgFor(_ context.Context, orgID string) string {
	return orgID
}

type toolCallScopeFixture struct {
	conn      *pgxpool.Pool
	principal Principal
	project   ResolvedProject
	agents    ownerScopeAgents
	recent    *recordingRecentToolCallReader
	search    *recordingToolCallSearchReader
	reader    *PostgresReader
	service   *DiagnosticsService
}

// newToolCallScopeFixture wires both call-listing tools through the production
// PostgresReader and authorization engine, with recording telemetry readers so
// the scope each read was handed is observable.
func newToolCallScopeFixture(t *testing.T, name string) toolCallScopeFixture {
	t.Helper()
	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, name)
	require.NoError(t, err)
	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	agents := seedOwnerScopeAgents(t, ctx, conn, principal)
	engine := authz.NewEngine(testenv.NewLogger(t), conn, func(context.Context, string) (bool, error) { return false, nil }, nil)
	recent := &recordingRecentToolCallReader{rows: nil}
	search := &recordingToolCallSearchReader{}
	reader := NewPostgresReader(testenv.NewLogger(t), conn).
		WithAuthorization(engine).
		WithCanonicalIdentityGate(fixedIdentityGate{}).
		WithRecentToolCalls(recent, mustParseURL(t, "https://app.getgram.test"))
	codec, err := newSubjectReferenceCodec("tool-call-scope-test-key")
	require.NoError(t, err)
	fixedNow := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	service := &DiagnosticsService{
		db:              conn,
		telemetry:       stubDiagnosticsTelemetry{watermark: fixedNow.Add(-time.Minute).UnixNano()},
		drilldown:       nil,
		toolUsage:       nil,
		userSearch:      nil,
		search:          search,
		references:      codec,
		sensitiveBudget: allowBudget(),
		volume:          DrilldownVolumeBudget{Rows: allowOperationLimiter{}, MetricQueries: allowOperationLimiter{}},
		auditor:         &recordingDrilldownAuditor{},
		sessions:        nil,
		sessionCapture:  nil,
		reader:          reader,
		readiness:       nil,
		budget:          allowBudget(),
		identityGate:    fixedIdentityGate{},
		now:             func() time.Time { return fixedNow },
	}
	return toolCallScopeFixture{conn: conn, principal: principal, project: project, agents: agents, recent: recent, search: search, reader: reader, service: service}
}

// ownScope is the scope a member without log:read is expected to read under:
// themselves by id and email, plus the agents they currently own. The latched
// and foreign agents are absent.
func (f toolCallScopeFixture) ownScope() *telemetryrepo.OwnScope {
	return &telemetryrepo.OwnScope{UserID: f.principal.UserID, UserEmail: f.agents.email, AgentIDs: []string{f.agents.own.String()}, IdentityOrg: f.principal.OrganizationID}
}

// callBoth runs both call-listing tools and returns the scope each one handed
// to telemetry, failing if either call errors.
func (f toolCallScopeFixture) callBoth(t *testing.T, ctx context.Context, principal Principal) (*telemetryrepo.OwnScope, *telemetryrepo.OwnScope) {
	t.Helper()
	_, err := f.reader.ListRecentToolCalls(ctx, principal, ListRecentToolCallsInput{ProjectID: f.project.ID.String(), AgentID: f.agents.foreign.String()})
	require.NoError(t, err)
	_, err = f.service.SearchToolCalls(ctx, principal, SearchToolCallsInput{ProjectID: f.project.ID.String(), Outcome: ToolCallOutcomeFailure})
	require.NoError(t, err)
	return f.recent.params.OwnScope, f.search.traceParams.OwnScope
}

func (f toolCallScopeFixture) refuseBoth(t *testing.T, ctx context.Context, principal Principal, why string) {
	t.Helper()
	recentCalls := f.recent.params.GramProjectID
	searchCalls := f.search.calls
	_, err := f.reader.ListRecentToolCalls(ctx, principal, ListRecentToolCallsInput{ProjectID: f.project.ID.String()})
	require.Error(t, err, why)
	_, err = f.service.SearchToolCalls(ctx, principal, SearchToolCallsInput{ProjectID: f.project.ID.String()})
	require.Error(t, err, why)
	require.Equal(t, recentCalls, f.recent.params.GramProjectID, "%s: no rows are read", why)
	require.Equal(t, searchCalls, f.search.calls, "%s: no rows are read", why)
}

func TestToolCallListingsMemberReadsOwnScope(t *testing.T) {
	t.Parallel()
	f := newToolCallScopeFixture(t, "platform_mcp_tool_call_own_scope")
	ctx := t.Context()

	// No grants, and project or organization administration, read the same own
	// scope; the caller's filters still travel alongside it.
	for name, grants := range map[string][]authz.Grant{
		"no grants": nil,
		"project and org admin": {
			authz.NewGrant(authz.ScopeProjectRead, f.project.ID.String()),
			authz.NewGrant(authz.ScopeProjectWrite, f.project.ID.String()),
			authz.NewGrant(authz.ScopeOrgAdmin, f.principal.OrganizationID),
		},
		"log:read on another project": {authz.NewGrant(authz.ScopeLogRead, uuid.NewString())},
	} {
		recent, search := f.callBoth(t, platformMemberContext(ctx, f.principal, grants...), f.principal)
		require.Equal(t, f.ownScope(), recent, name)
		require.Equal(t, f.ownScope(), search, name)
	}
	require.Equal(t, []string{f.agents.foreign.String()}, f.recent.params.AgentIDs, "an agent filter is ANDed with the scope, so it cannot reach another owner's agent")
	require.Equal(t, []string{ToolCallOutcomeFailure}, f.search.traceParams.Statuses)

	recent, search := f.callBoth(t, platformMemberContext(ctx, f.principal, authz.NewGrant(authz.ScopeLogRead, f.project.ID.String())), f.principal)
	require.Nil(t, recent, "log:read on the project reads every call")
	require.Nil(t, search, "log:read on the project reads every call")
}

// Non-human callers keep the plain log:read check: without it they are refused
// rather than handed a member's scope.
func TestToolCallListingsRefuseNonHumanCallersWithoutLogRead(t *testing.T) {
	t.Parallel()
	f := newToolCallScopeFixture(t, "platform_mcp_tool_call_non_human")
	ctx := t.Context()
	member := platformMemberContext(ctx, f.principal)

	assistantRuntime := contextvalues.SetAssistantPrincipal(member, contextvalues.AssistantPrincipal{AssistantID: uuid.New(), ThreadID: uuid.New()})
	f.refuseBoth(t, assistantRuntime, f.principal, "assistant runtime")

	projectAssistant := f.principal
	projectAssistant.Surface = SurfaceProjectAssistant
	projectAssistant.ConnectionID, projectAssistant.Generation = "", ""
	f.refuseBoth(t, member, projectAssistant, "project assistant surface")

	external := contextvalues.WithAuthenticatedActor(ctx, &contextvalues.AuthContext{ActiveOrganizationID: f.principal.OrganizationID, UserID: f.principal.UserID, ExternalUserID: "external-user"}, urn.NewPrincipal(urn.PrincipalTypeUser, f.principal.UserID))
	external = contextvalues.SetActingSurface(external, contextvalues.ActingSurfacePlatformMCP)
	f.refuseBoth(t, ContextWithPrincipal(authz.GrantsToContext(external, []authz.Grant{}), f.principal), f.principal, "embedded chat session")

	apiKey := contextvalues.WithAuthenticatedActor(ctx, &contextvalues.AuthContext{ActiveOrganizationID: f.principal.OrganizationID, UserID: f.principal.UserID, APIKeyID: "key"}, urn.NewPrincipal(urn.PrincipalTypeUser, f.principal.UserID))
	f.refuseBoth(t, ContextWithPrincipal(authz.GrantsToContext(apiKey, []authz.Grant{}), f.principal), f.principal, "API key")

	// With log:read the assistant runtime reads the whole project as before.
	_, err := f.reader.ListRecentToolCalls(contextvalues.SetAssistantPrincipal(platformMemberContext(ctx, f.principal, authz.NewGrant(authz.ScopeLogRead, f.project.ID.String())), contextvalues.AssistantPrincipal{AssistantID: uuid.New(), ThreadID: uuid.New()}), f.principal, ListRecentToolCallsInput{ProjectID: f.project.ID.String()})
	require.NoError(t, err)
	require.Nil(t, f.recent.params.OwnScope)
}
