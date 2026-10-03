package platformmcp

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/plugins"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// TestSearchToolCallsNarrowsToOneServerWithoutReportedNames walks the vertical
// the unit tests cannot: a real configured server in Postgres, resolved through
// the production GetMCP authorization and serverIdentity path, and the
// telemetry query parameters that path actually built.
//
// It is the regression guard for attributing one configured server's tool call
// history. The parameters are read back from the reader rather than
// constructed by the test, so re-plumbing a reported name into the search
// fails here, whether it is reintroduced in serverIdentity, in
// toolLogsTargets, or directly at the query call site.
func TestSearchToolCallsNarrowsToOneServerWithoutReportedNames(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_tool_call_search")
	require.NoError(t, err)
	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	servers, err := mcpserversrepo.New(conn).ListMCPServersForTelemetryByProjectID(ctx, project.ID)
	require.NoError(t, err)
	require.Len(t, servers, 1)
	configured := servers[0]
	require.True(t, configured.Slug.Valid)
	require.True(t, configured.Name.Valid)

	fixedNow := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	codec := newSubjectReferenceCodec("tool-call-search-integration-key")
	engine := authz.NewEngine(testenv.NewLogger(t), conn, func(context.Context, string) (bool, error) { return false, nil }, nil)
	reader := NewPostgresReader(testenv.NewLogger(t), conn).
		WithAuthorization(engine).
		WithToolExposure(NewMCPToolExposureService(testenv.NewLogger(t), conn, audit.NewLogger(), engine, NewLiveOrgAdminAuthorizer(conn, engine), "tool-call-search-exposure-key", plugins.PublicationRequests{Enabled: false}, nil, testOperationBudget(), testOperationBudget(), func(context.Context, uuid.UUID, uuid.UUID) error { return nil }))
	search := &recordingToolCallSearchReader{}
	service := &DiagnosticsService{
		db:              conn,
		telemetry:       stubDiagnosticsTelemetry{watermark: fixedNow.Add(-time.Minute).UnixNano()},
		drilldown:       nil,
		toolUsage:       nil,
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
		identityGate:    literalIdentityGate{},
		now:             func() time.Time { return fixedNow },
	}

	ctx = contextvalues.WithAuthenticatedActor(ctx, &contextvalues.AuthContext{ActiveOrganizationID: principal.OrganizationID, UserID: principal.UserID}, urn.NewPrincipal(urn.PrincipalTypeUser, principal.UserID))
	ctx = contextvalues.SetActingSurface(ctx, contextvalues.ActingSurfacePlatformMCP)
	ctx = authz.GrantsToContext(ctx, []authz.Grant{
		authz.NewGrant(authz.ScopeProjectRead, project.ID.String()),
		authz.NewGrant(authz.ScopeMCPRead, configured.ID.String()),
	})

	output, err := service.SearchToolCalls(ctx, principal, SearchToolCallsInput{
		ProjectID: project.ID.String(),
		MCPID:     configured.ID.String(),
		Window:    "24h",
	})
	require.NoError(t, err)
	require.Equal(t, configured.ID.String(), output.MCPID)
	require.False(t, output.AttributionUnavailable, "a configured server resolves to at least its own id")
	require.Equal(t, 1, search.calls, "the search reached the telemetry query")

	params := search.traceParams
	require.Equal(t, project.ID.String(), params.GramProjectID)

	// The reliable links are present. The configured slug is one of them: the
	// matcher stamps it as a proxied call's target id, and the query admits it
	// only under the hosted and tunneled target types, which a client cannot
	// choose. Dropping it would silently stop attributing the server's own
	// proxied traffic, so it is asserted positively.
	require.Equal(t, []string{configured.Slug.String, configured.ID.String()}, params.MCPServerTargetIDs)

	// No selector carries a name a client reported. The display name is the
	// one a hook is most likely to report, so it is named explicitly: a shadow
	// row's target id is whatever the calling app said, and matching it would
	// hand this server an unrelated same-named server's calls.
	require.Empty(t, params.ShadowServerNames,
		"an mcp_id search must never select shadow rows by a reported name")
	require.NotContains(t, params.MCPServerTargetIDs, configured.Name.String)
	require.NotContains(t, params.HostedToolsetSlugs, configured.Name.String)

	// The project's own matchers are still loaded, so hook-observed calls this
	// server can be held to are classified onto it before the target filter
	// runs. Narrowing is what changed, not classification.
	require.NotNil(t, params.MCPServerMatchers)
}
