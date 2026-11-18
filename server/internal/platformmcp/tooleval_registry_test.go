package platformmcp

import (
	"context"
	"errors"
	"log/slog"
	"net/url"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/access"
	agentrepo "github.com/speakeasy-api/gram/server/internal/agent/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/dataexports"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/mcpapproval"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
	"github.com/speakeasy-api/gram/server/internal/plugins"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/projects"
	"github.com/speakeasy-api/gram/server/internal/risk"
	"github.com/speakeasy-api/gram/server/internal/risk/chatfindings"
	"github.com/speakeasy-api/gram/server/internal/risk/chrepo"
	"github.com/speakeasy-api/gram/server/internal/sigint"
	telemetryrepo "github.com/speakeasy-api/gram/server/internal/telemetry/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

// errToolEvalNotInvoked is what a tool eval stub returns. The eval only lists
// tools, so no handler reaches a stub.
var errToolEvalNotInvoked = errors.New("tool eval: not invoked")

// composeToolEvalRegistry builds the deployment's registry with every
// external tool's live variant. It starts from the database-outage backstop's
// composition and adds each dependency that otherwise registers a
// placeholder. Handlers never run in the eval, so the non-PostgreSQL
// dependencies are stubs that only satisfy the registration-time checks; live
// descriptions and schemas do not depend on them.
func composeToolEvalRegistry(t *testing.T, logger *slog.Logger, pool *pgxpool.Pool, engine *authz.Engine) *Registrar {
	t.Helper()
	const key = "platform-mcp-tooleval-key"
	budget := allowBudget()
	auditLogger := audit.NewLogger()
	chConn, err := platformMCPInfra.NewClickhouseClient(t)
	require.NoError(t, err)
	findings := chrepo.New(chConn)
	flags := &feature.InMemory{}
	slugs := NewPostgresOrganizationSlugResolver(pool)
	authorizer := NewLiveOrgAdminAuthorizer(pool, engine)
	dashboardURL, err := url.Parse("https://app.example.test")
	require.NoError(t, err)
	encryption := testenv.NewEncryptionClient(t)
	publication := plugins.PublicationRequests{}
	enabled := func(context.Context, string) (bool, error) { return true, nil }

	toolExposure, err := NewMCPToolExposureService(logger, pool, auditLogger, engine, authorizer, key, publication, nil, budget, budget)
	require.NoError(t, err)
	projectLifecycle, err := NewProjectLifecycleService(logger, pool, projects.NewCore(logger, auditLogger, nil, false), engine, authorizer, budget)
	require.NoError(t, err)
	routeToggle, err := NewDataExportRouteToggleService(logger, pool, dataexports.NewRouteEnabledCore(auditLogger, encryption), authorizer, budget)
	require.NoError(t, err)
	shadowInventory, err := NewShadowInventoryService(stubShadowInventory{}, stubShadowReview{}, platformrepo.New(pool), budget, key)
	require.NoError(t, err)
	reader := NewPostgresReader(logger, pool).
		WithAuthorization(engine).
		WithReviewRequests(&stubMCPReviewRequests{}, budget).
		WithDataExports(encryption, dashboardURL).
		WithDataExportMutations(auditLogger, dashboardURL).
		WithDataExportRouteToggle(routeToggle).
		WithRecentToolCalls(&recordingRecentToolCallReader{}, dashboardURL).
		WithMCPNetworkTraffic(&recordingNetworkTrafficReader{}, enabled).
		WithOrganizationEvents(&recordingEventFeedReader{}, enabled, dashboardURL).
		WithNetworkIngressStatus(dashboardURL).
		WithRiskAnalysisStatus(NewRiskAnalysisStatusService(logger, pool, &stubAnalysisDescriber{})).
		WithRiskFindings(NewRiskFindingsService(pool, findings, key), budget).
		WithRiskFindingList(NewRiskFindingListService(pool, nil, key), budget).
		WithChatMetadata(NewChatMetadataService(pool, engine, chatfindings.New(pool, findings), budget, key)).
		WithOrganizationGateway(NewOrganizationGatewayService(pool, dashboardURL)).
		WithGatewayProxy(&recordingGatewayProxy{}, flags).
		WithToolExposure(toolExposure).
		WithProjectLifecycle(projectLifecycle).
		WithShadowInventory(shadowInventory).
		WithShadowAI(NewShadowAIService(&stubDetectionReader{}, NewPostgresShadowAILibrary(agentrepo.New(pool)), authorizer, budget)).
		WithDeviceCoverage(NewDeviceCoverageService(&stubDeviceCoverageReader{}, engine, budget)).
		WithOrganizationGatewayActivity(NewOrganizationGatewayActivityService(pool, &stubGatewayActivityReader{}, engine, budget))
	// The risk policy and exclusion reads hang off the cursor key, which the
	// runtime sets in production.
	reader.setInventoryCursorKey(key)

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
		return mcpserversrepo.McpServer{}, errToolEvalNotInvoked
	}, key)
	require.NoError(t, err)
	visibility, err := NewLifecycleVisibilityService(pool, auditLogger,
		func(context.Context, pgx.Tx, string, uuid.UUID, uuid.UUID) error { return errToolEvalNotInvoked },
		func(context.Context, pgx.Tx, mcpserversrepo.McpServer, LifecycleVisibilityUpdate) (LifecycleVisibilityUpdateResult, error) {
			return LifecycleVisibilityUpdateResult{}, errToolEvalNotInvoked
		},
		func(context.Context, uuid.UUID, string, string) error { return errToolEvalNotInvoked },
		func(context.Context, []uuid.UUID) error { return nil },
		readiness, key)
	require.NoError(t, err)
	registrations := NewRegistrationService(nil, gate, store).
		WithDirectRemoteInspector(&testDirectRemoteInspector{}).
		WithLifecycleMetadata(metadata).
		WithLifecycleVisibility(visibility).
		WithOperationBudgets(budgets).
		WithReadiness(readiness).
		WithDashboardURL(dashboardURL).
		WithClientAdmission(NewClientAdmissionService(pool, auditLogger))

	pluginInventory := NewPluginsService(pool, budget, key).
		WithAuthorization(engine).
		WithInstallLinks(dashboardURL, dashboardURL).
		WithAssignmentMutations(flags, slugs, auditLogger, budget).
		WithMetadataMutations(logger, plugins.NewPluginMetadataCore(auditLogger, publication), budget).
		WithPublicationRequests(publication).
		WithPublicationEvidence(stubPluginPublicationEvidence{}).
		WithRepublish(publication, &recordingPluginPublishSignaler{}, budget)
	reader.WithShadowDecisions(NewShadowDecisionService(pool, shadowInventory, &mcpapproval.Service{}, pluginInventory, flags, slugs, budget))
	distributions := NewDistributionService(pool, auditLogger,
		func(context.Context, pgx.Tx, *contextvalues.AuthContext, string, uuid.UUID, uuid.UUID, uuid.UUID, string) (uuid.UUID, bool, error) {
			return uuid.Nil, false, errToolEvalNotInvoked
		},
		func(context.Context, uuid.UUID, string, string) error { return errToolEvalNotInvoked },
		pluginInventory)
	skills := NewSkillsService(&registrySkillsManagement{}, NewPostgresSkillTargets(pool), store, engine, stubSkillsGate{enabled: true, err: nil}, budget).
		WithInsights(&stubSkillInsightsReader{}, budget)
	diagnostics := NewDiagnosticsService(pool, stubUsageSummaryTelemetry{}, enabled, reader, readiness, budget).
		WithToolUsageBreakdown(&recordingToolUsageReader{}).
		WithDrilldown(toolEvalDrilldown{}, key, budget, budgets.DrilldownVolume, NewPostgresDrilldownAuditor(pool)).
		WithUserSearch(&recordingUserSearchReader{}).
		WithToolCallSearch(&recordingToolCallSearchReader{})
	accessReads := NewAccessReadService(logger, pool, budget, key)
	accessRoleMutations, err := NewAccessRoleMutationService(accessReads, flags, budget, key, access.NewRoleManager(logger, pool, workos.NewStubClient(), auditLogger, publication, nil))
	require.NoError(t, err)
	sessionRecall := NewSessionRecallService(logger, pool, platformrepo.New(pool), auditLogger, enabled, findings, budget)
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
	reader.WithAnalytics(NewAnalyticsService(logger, &stubAnalyticsEngine{}, flags, slugs, reader, budget, features))
	management := sigint.NewService(logger, tracer, pool, &sessions.Manager{}, engine, auditLogger, features)
	signalAuthoring := NewSignalAuthoringService(management, reader, engine, key)

	_, registrar := newServerWithRiskMutations(reader, testCatalog{}, registrations, key, nil, NewFeedbackService(pool), NewOnboardingService(pool),
		distributions, skills, diagnostics, NewWorkflowRunService(logger, &captureWorkflowRunEmitter{}), pluginInventory, sessionRecall, riskMutations,
		CatalogDescriptor{}, accessReads, accessRoleMutations, nil, signalAuthoring)
	registrar.withLogger(logger)
	return registrar
}

// toolEvalDrilldown satisfies the drill-down tools' registration check. No
// package test stub covers DrilldownTelemetryReader.
type toolEvalDrilldown struct{}

func (toolEvalDrilldown) GetMCPToolOutcomeBreakdown(context.Context, telemetryrepo.GetMCPToolOutcomeBreakdownParams) ([]telemetryrepo.MCPToolOutcomeBreakdownRow, error) {
	return nil, errToolEvalNotInvoked
}

func (toolEvalDrilldown) ListMCPTraceReferences(context.Context, telemetryrepo.ListMCPTraceReferencesParams) ([]telemetryrepo.MCPTraceReferenceRow, error) {
	return nil, errToolEvalNotInvoked
}

func (toolEvalDrilldown) ListMCPUsageUsers(context.Context, telemetryrepo.GetMCPOutcomeBreakdownParams) ([]telemetryrepo.MCPUsageUserRow, error) {
	return nil, errToolEvalNotInvoked
}

func (toolEvalDrilldown) ListMCPUsageUserTools(context.Context, telemetryrepo.GetMCPOutcomeBreakdownParams, string, string) ([]telemetryrepo.MCPUsageUserToolRow, error) {
	return nil, errToolEvalNotInvoked
}
