package platformmcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/url"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/access"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/dataexports"
	"github.com/speakeasy-api/gram/server/internal/feature"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
	"github.com/speakeasy-api/gram/server/internal/plugins"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/projects"
	"github.com/speakeasy-api/gram/server/internal/risk"
	"github.com/speakeasy-api/gram/server/internal/sigint"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

// errNotReached marks a write callback that a closed pool fails before.
var errNotReached = errors.New("not reached: the closed pool fails first")

// Nothing between a tool and an external MCP client sanitizes its error
// beyond the registration-layer backstop, so this runs every tool the
// deployment registers — including ones nobody has written yet — against a
// database that is down, and asserts that no caller is shown database text.
//
// Authorization answers from a live database, as the request path does, so
// each call gets past it and fails inside the tool's own service. Every
// service with a PostgreSQL dependency is composed live on the closed pool;
// the rest keep their unavailable registration, which is still invoked.
func TestNoRegisteredToolReturnsDatabaseErrorTextWhenTheDatabaseIsDown(t *testing.T) {
	t.Parallel()

	ctx, fixture := seedProjectLifecycleFixture(t, t.Context(), "platform_mcp_registry_db_failure")
	// A second pool on the same database, so closing it leaves the live
	// authorization pool untouched.
	broken, err := pgxpool.NewWithConfig(ctx, fixture.conn.Config().Copy())
	require.NoError(t, err)
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	registrar := composeRegistryOnPool(t, logger, broken, fixture.conn, fixture.engine)
	// Every query on a closed pool fails with a driver error, which is what a
	// database outage looks like from here.
	broken.Close()

	ctx = ContextWithPrincipal(ctx, fixture.principal)
	// The fixture's real project, wherever a schema asks for one, so project
	// resolution is not the only thing that fails.
	project := map[string]any{"project_id": fixture.project.ID.String(), "project_slug": fixture.project.Slug}
	descriptors := registrar.Descriptors()
	require.NotEmpty(t, descriptors)
	var refused atomic.Int32
	// Checked once every tool's subtest has finished, which is when cleanups
	// run.
	t.Cleanup(func() {
		require.Greater(t, int(refused.Load()), len(descriptors)/2, "most tools need the database, so most must have refused")
		require.Contains(t, logs.String(), "closed pool", "the server log keeps the underlying cause")
	})
	// One subtest per tool, so a regression names every tool that leaks
	// rather than only the first.
	for _, descriptor := range descriptors {
		t.Run(descriptor.Name, func(t *testing.T) {
			t.Parallel()
			output, err := invokeWithValidArguments(t, ctx, descriptor, project)
			if err != nil {
				refused.Add(1)
				requireNoDatabaseText(t, descriptor.Name, err.Error())
				return
			}
			// A tool that answered anyway — a preview, or a read that needs no
			// database — must not have put database text in its result either.
			encoded, marshalErr := json.Marshal(output)
			require.NoError(t, marshalErr)
			requireNoneOf(t, descriptor.Name, string(encoded), driverErrorText)
		})
	}
}

// composeRegistryOnPool builds the deployment's registry with every service
// that depends only on PostgreSQL composed live on pool. Authorization reads
// live and liveEngine instead, so it can admit the caller.
func composeRegistryOnPool(t *testing.T, logger *slog.Logger, pool, live *pgxpool.Pool, liveEngine *authz.Engine) *Registrar {
	t.Helper()
	const key = "platform-mcp-registry-backstop-key"
	// Stateless, so tools can run concurrently against one composition.
	budget := allowBudget()
	auditLogger := audit.NewLogger()
	flags := &feature.InMemory{}
	slugs := NewPostgresOrganizationSlugResolver(pool)
	authorizer := NewLiveOrgAdminAuthorizer(live, liveEngine)
	dashboardURL, err := url.Parse("https://app.example.test")
	require.NoError(t, err)
	encryption := testenv.NewEncryptionClient(t)
	publication := plugins.PublicationRequests{}

	toolExposure, err := NewMCPToolExposureService(logger, pool, auditLogger, liveEngine, authorizer, key, publication, nil, budget, budget)
	require.NoError(t, err)
	projectLifecycle, err := NewProjectLifecycleService(logger, pool, projects.NewCore(logger, auditLogger, nil, false), liveEngine, authorizer, budget)
	require.NoError(t, err)
	routeToggle, err := NewDataExportRouteToggleService(logger, pool, dataexports.NewRouteEnabledCore(auditLogger, encryption), authorizer, budget)
	require.NoError(t, err)
	reader := NewPostgresReader(logger, pool).
		WithAuthorization(liveEngine).
		WithDataExports(encryption, dashboardURL).
		WithDataExportMutations(auditLogger, dashboardURL).
		WithDataExportRouteToggle(routeToggle).
		WithNetworkIngressStatus(dashboardURL).
		WithRiskAnalysisStatus(NewRiskAnalysisStatusService(logger, pool, nil, flags, slugs)).
		WithRiskFindings(NewRiskFindingsService(pool, nil, flags, slugs, key), budget).
		WithRiskFindingList(NewRiskFindingListService(pool, nil, key), budget).
		WithChatMetadata(NewChatMetadataService(pool, budget, key)).
		WithToolExposure(toolExposure).
		WithProjectLifecycle(projectLifecycle)

	store, err := NewRegistrationStore(pool)
	require.NoError(t, err)
	gate := NewCatalogRegistrationGate(allowGate{})
	budgets := OperationBudgets{
		RiskFindings: budget, Catalog: budget, Registration: budget, ReviewRequests: budget, Handoff: budget,
		SetupStart: budget, Repair: budget, Docs: budget, Skills: budget, LifecycleMetadata: budget, Plugins: budget,
		AccessReads: budget, AccessRoleMutations: budget, Diagnostics: budget, SensitiveDiagnostics: budget,
		SensitiveSessionRecall: budget, RiskMutations: budget,
		DrilldownVolume: DrilldownVolumeBudget{Rows: budget.Connection, MetricQueries: budget.Connection},
	}
	readiness := NewReadinessService(store, gate, NewProviderAdapters(nil), budget.Connection, budget)
	metadata, err := NewLifecycleMetadataService(pool, func(context.Context, pgx.Tx, mcpserversrepo.McpServer, LifecycleMetadataUpdate) (mcpserversrepo.McpServer, error) {
		return mcpserversrepo.McpServer{}, errNotReached
	}, key)
	require.NoError(t, err)
	visibility, err := NewLifecycleVisibilityService(pool, auditLogger,
		func(context.Context, pgx.Tx, string, uuid.UUID, uuid.UUID) error { return errNotReached },
		func(context.Context, pgx.Tx, mcpserversrepo.McpServer, LifecycleVisibilityUpdate) (LifecycleVisibilityUpdateResult, error) {
			return LifecycleVisibilityUpdateResult{}, errNotReached
		},
		func(context.Context, uuid.UUID, string, string) error { return errNotReached },
		func(context.Context, []uuid.UUID) error { return nil },
		readiness, key)
	require.NoError(t, err)
	registrations := NewRegistrationService(nil, gate, store).
		WithLifecycleMetadata(metadata).
		WithLifecycleVisibility(visibility).
		WithOperationBudgets(budgets).
		WithReadiness(readiness).
		WithDashboardURL(dashboardURL).
		WithClientAdmission(NewClientAdmissionService(pool, auditLogger))

	pluginInventory := NewPluginsService(pool, budget, key).
		WithAuthorization(liveEngine).
		WithInstallLinks(dashboardURL, dashboardURL).
		WithAssignmentMutations(flags, slugs, auditLogger, budget).
		WithMetadataMutations(logger, plugins.NewPluginMetadataCore(auditLogger, publication), budget).
		WithPublicationRequests(publication).
		WithRepublish(publication, nil, budget)
	distributions := NewDistributionService(pool, auditLogger,
		func(context.Context, pgx.Tx, *contextvalues.AuthContext, string, uuid.UUID, uuid.UUID, uuid.UUID, string) (uuid.UUID, bool, error) {
			return uuid.Nil, false, errNotReached
		},
		func(context.Context, uuid.UUID, string, string) error { return errNotReached },
		pluginInventory)
	skills := NewSkillsService(&registrySkillsManagement{}, NewPostgresSkillTargets(pool), store, liveEngine, stubSkillsGate{enabled: true, err: nil}, budget)
	diagnostics := NewDiagnosticsService(pool, stubUsageSummaryTelemetry{}, func(context.Context, string) (bool, error) { return true, nil }, reader, readiness, budget)
	accessReads := NewAccessReadService(logger, pool, budget, key)
	accessRoleMutations, err := NewAccessRoleMutationService(accessReads, flags, budget, key, access.NewRoleManager(logger, pool, workos.NewStubClient(), auditLogger, publication, nil))
	require.NoError(t, err)
	sessionRecall := NewSessionRecallService(logger, pool, platformrepo.New(pool), auditLogger, func(context.Context, string) (bool, error) { return true, nil }, budget)
	controls, err := NewRiskMutationControls(pool, flags, slugs, budget, key)
	require.NoError(t, err)
	riskMutations, err := NewRiskMutationHandlers(pool, controls,
		risk.NewPolicyMutationCore(pool, auditLogger, nil, nil, nil),
		risk.NewExclusionMutationCore(logger, pool, auditLogger, nil, key),
		risk.NewFalsePositiveCore(auditLogger, nil))
	require.NoError(t, err)

	redis, err := platformMCPInfra.NewRedisClient(t, 0)
	require.NoError(t, err)

	tracer := testenv.NewTracerProvider(t)
	features := productfeatures.NewClient(logger, tracer, pool, redis)
	management := sigint.NewService(logger, tracer, pool, &sessions.Manager{}, liveEngine, auditLogger, features)
	signalAuthoring := NewSignalAuthoringService(management, reader, liveEngine, key)

	_, registrar := newServerWithRiskMutations(reader, nil, registrations, key, nil, NewFeedbackService(pool), NewOnboardingService(pool),
		distributions, skills, diagnostics, NewWorkflowRunService(logger, nil), pluginInventory, sessionRecall, riskMutations,
		CatalogDescriptor{}, accessReads, accessRoleMutations, nil, signalAuthoring)
	registrar.withLogger(logger)
	return registrar
}
