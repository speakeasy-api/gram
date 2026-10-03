package platformmcp

import (
	"context"
	"fmt"
	"net/url"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/access"
	agentrepo "github.com/speakeasy-api/gram/server/internal/agent/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/mcpendpoints"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	oauthregistration "github.com/speakeasy-api/gram/server/internal/oauth/registration"
	"github.com/speakeasy-api/gram/server/internal/oktaresourceconnections"
	otelchrepo "github.com/speakeasy-api/gram/server/internal/otel/chrepo"
	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
	"github.com/speakeasy-api/gram/server/internal/plugins"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/risk"
	riskchrepo "github.com/speakeasy-api/gram/server/internal/risk/chrepo"
	"github.com/speakeasy-api/gram/server/internal/risk/policycatalog"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp/admission"
	telemetryrepo "github.com/speakeasy-api/gram/server/internal/telemetry/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

const testServicesKey = "platform-mcp-test-services-key"

// literalIdentityGate keeps usage attribution on literal identity buckets.
type literalIdentityGate struct{}

func (literalIdentityGate) CanonicalOrgFor(context.Context, string) string { return "" }

// newTestServices composes every Platform MCP capability the way the server
// does, against a cloned database and the shared ClickHouse container.
func newTestServices(t *testing.T) Services {
	t.Helper()

	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)
	meterProvider := testenv.NewMeterProvider(t)
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_services")
	require.NoError(t, err)
	redisClient, err := platformMCPInfra.NewRedisClient(t, 0)
	require.NoError(t, err)
	chConn, err := platformMCPInfra.NewClickhouseClient(t)
	require.NoError(t, err)
	temporalEnv, _ := platformMCPInfra.NewTemporalEnv(t)
	encryptionClient := testenv.NewEncryptionClient(t)
	sessions := testenv.NewTestManager(t, logger, tracerProvider, conn, redisClient, cache.Suffix("platform-mcp-services-"+uuid.NewString()), billing.NewStubClient(logger, tracerProvider))
	engine := authz.NewEngine(logger, conn, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient())
	auditLogger := audit.NewLogger()
	flags := &feature.InMemory{}
	policy, err := guardian.NewUnsafePolicy(tracerProvider, nil)
	require.NoError(t, err)
	serverURL, err := url.Parse("https://gram.example.test")
	require.NoError(t, err)
	dashboardURL := testenv.DefaultSiteURL(t)
	challenges := remotesessions.NewChallengeManager(logger, tracerProvider, meterProvider, conn, encryptionClient, policy, newTestTunnelClient(t, policy), cache.NewRedisCacheAdapter(redisClient), serverURL)
	identity := remotesessions.NewIdentityCommitter(logger, conn, encryptionClient, auditLogger, serverURL, policy, newTestTunnelClient(t, policy), oauthregistration.NewMetrics(logger, meterProvider))
	enabled := FeatureChecker(func(context.Context, string) (bool, error) { return true, nil })
	budget := testOperationBudget()
	budgets := OperationBudgets{
		RiskFindings: budget, Catalog: budget, Registration: budget, ReviewRequests: budget, Handoff: budget,
		SetupStart: budget, Repair: budget, Docs: budget, Skills: budget, LifecycleMetadata: budget, Plugins: budget,
		AccessReads: budget, AccessRoleMutations: budget, Diagnostics: budget, SensitiveDiagnostics: budget,
		SensitiveSessionRecall: budget, RiskMutations: budget,
		DrilldownVolume: DrilldownVolumeBudget{Rows: allowOperationLimiter{}, MetricQueries: allowOperationLimiter{}},
	}
	organizationSlugs := NewPostgresOrganizationSlugResolver(conn)
	distributionGuard := admission.NewGuard(flags, admission.NewReportMetrics(meterProvider, logger))
	gate := NewCatalogRegistrationGate(NewOrganizationGate(testCapabilityChecker{enabled: true, err: nil}))
	store := NewRegistrationStore(conn)
	adapters := NewProviderAdapters(nil)
	readiness := NewReadinessService(store, gate, adapters, allowOperationLimiter{}, budgets.Repair, NewRemoteMCPReadinessProber(logger, conn, encryptionClient, policy, challenges))
	catalog := NewRegistryCatalogSources(nil)

	lifecycleMetadata := NewLifecycleMetadataService(conn, func(ctx context.Context, tx pgx.Tx, existing mcpserversrepo.McpServer, input LifecycleMetadataUpdate) (mcpserversrepo.McpServer, error) {
		name := input.Name
		return mcpservers.UpdateMCPServerLifecycleInTransaction(ctx, tx, auditLogger, existing, mcpservers.LifecycleUpdateInput{
			OrganizationID: input.OrganizationID, ProjectID: input.ProjectID, ActorUserID: input.ActorUserID, ActorEmail: nil,
			ServerID: input.ServerID, Name: &name, Visibility: existing.Visibility, EnvironmentID: existing.EnvironmentID,
			UserSessionIssuerID: existing.UserSessionIssuerID, RemoteMcpServerID: existing.RemoteMcpServerID,
			TunneledMcpServerID: existing.TunneledMcpServerID, ToolsetID: existing.ToolsetID,
			UnproxiedMcpServerID: existing.UnproxiedMcpServerID, ToolVariationsGroupID: existing.ToolVariationsGroupID,
		})
	}, testServicesKey)
	publish := ProjectPublisher(func(context.Context, uuid.UUID, string, string) error { return nil })
	lifecycleVisibility := NewLifecycleVisibilityService(conn, auditLogger, mcpservers.LockMCPServerVisibilityDependencies, func(ctx context.Context, tx pgx.Tx, existing mcpserversrepo.McpServer, input LifecycleVisibilityUpdate) (LifecycleVisibilityUpdateResult, error) {
		updated, err := mcpservers.UpdateMCPServerVisibilityInTransaction(ctx, tx, auditLogger, existing, mcpservers.LifecycleUpdateInput{
			OrganizationID: input.OrganizationID, ProjectID: input.ProjectID, ActorUserID: input.ActorUserID, ActorEmail: nil,
			ServerID: input.ServerID, Name: nil, Visibility: input.Visibility, EnvironmentID: existing.EnvironmentID,
			UserSessionIssuerID: existing.UserSessionIssuerID, RemoteMcpServerID: existing.RemoteMcpServerID,
			TunneledMcpServerID: existing.TunneledMcpServerID, ToolsetID: existing.ToolsetID,
			UnproxiedMcpServerID: existing.UnproxiedMcpServerID, ToolVariationsGroupID: existing.ToolVariationsGroupID,
		})
		if err != nil {
			return LifecycleVisibilityUpdateResult{}, fmt.Errorf("update mcp server visibility: %w", err)
		}
		return LifecycleVisibilityUpdateResult{Server: updated.Server, ClearedRootDomainIDs: updated.ClearedRootDomainIDs}, nil
	}, publish, func(context.Context, []uuid.UUID) error { return nil }, readiness, testServicesKey, distributionGuard, organizationSlugs)

	registrations := NewRegistrationService(catalog, gate, store).
		WithDirectRemoteInspector(NewGuardianDirectRemoteInspector(policy)).
		WithLifecycleMetadata(lifecycleMetadata).
		WithLifecycleVisibility(lifecycleVisibility).
		WithOperationBudgets(budgets).
		WithReadiness(readiness).
		WithDashboardURL(dashboardURL).
		WithIdentityProviderAttachment(NewCatalogIdentityProviderAttachmentService(logger, meterProvider, conn, identity, policy, serverURL)).
		WithClientAdmission(NewClientAdmissionService(conn, auditLogger))

	distributionReads := NewShadowDistributionReadService(logger, conn, distributionGuard, organizationSlugs)
	pluginInventory := NewPluginsService(conn, budgets.Plugins, testServicesKey, distributionGuard).
		WithAuthorization(engine).
		WithRemoteSessions(challenges).
		WithInstallLinks(dashboardURL, serverURL).
		WithAssignmentMutations(flags, organizationSlugs, auditLogger, budget).
		WithDistributionAdmissionReads(distributionReads)
	accessReads := NewAccessReadService(logger, conn, access.NewRoleManager(logger, conn, workos.NewStubClient(), audit.NewLogger()), budgets.AccessReads, testServicesKey)
	distributions := NewDistributionService(conn, auditLogger, func(ctx context.Context, tx pgx.Tx, authCtx *contextvalues.AuthContext, organizationID string, projectID, pluginID, mcpServerID uuid.UUID, displayName string) (uuid.UUID, bool, error) {
		attached, err := plugins.AttachToExistingPluginAudited(ctx, tx, auditLogger, authCtx, organizationID, projectID, pluginID, mcpServerID, displayName)
		if err != nil {
			return uuid.Nil, false, fmt.Errorf("attach mcp server to plugin: %w", err)
		}
		if attached == nil {
			return uuid.Nil, false, nil
		}
		return attached.Server.ID, true, nil
	}, publish, pluginInventory, distributionGuard, organizationSlugs)

	approvals := newTestApprovals(t, conn, flags)
	reader := NewPostgresReader(logger, conn).
		WithXAAReadiness(oktaresourceconnections.NewService(logger, tracerProvider, conn, sessions, engine, auditLogger, flags), flags).
		WithAuthorization(engine).
		WithReviewRequests(approvals, budgets.ReviewRequests).
		WithDataExports(encryptionClient, dashboardURL).
		WithDataExportMutations(auditLogger, dashboardURL).
		WithRecentToolCalls(telemetryrepo.New(chConn), dashboardURL).
		WithMCPNetworkTraffic(telemetryrepo.New(chConn), enabled).
		WithOrganizationEvents(otelchrepo.New(chConn), enabled, dashboardURL).
		WithNetworkIngressStatus(dashboardURL).
		WithRiskAnalysisStatus(NewRiskAnalysisStatusService(logger, conn, &stubAnalysisDescriber{}, flags, organizationSlugs)).
		WithRiskFindings(NewRiskFindingsService(conn, riskchrepo.New(chConn), flags, organizationSlugs, testServicesKey), budgets.RiskFindings).
		WithRiskFindingList(NewRiskFindingListService(conn, riskchrepo.New(chConn), testServicesKey), budgets.RiskFindings).
		WithChatMetadata(NewChatMetadataService(conn, budgets.SensitiveDiagnostics, testServicesKey)).
		WithToolExposure(NewMCPToolExposureService(logger, conn, auditLogger, engine, NewLiveOrgAdminAuthorizer(conn, engine), testServicesKey, plugins.PublicationRequests{Enabled: false}, nil, budget, budget, func(context.Context, uuid.UUID, uuid.UUID) error { return nil }))
	shadowInventory := NewShadowInventoryService(stubShadowInventory{}, stubShadowReview{}, flags, organizationSlugs, platformrepo.New(conn), budgets.SensitiveDiagnostics, testServicesKey)
	reader.WithShadowInventory(shadowInventory.WithDistributionAdmissionReads(distributionReads)).
		WithShadowDecisions(NewShadowDecisionService(conn, shadowInventory, approvals, pluginInventory, flags, organizationSlugs, budget)).
		WithShadowAI(NewShadowAIService(&stubDetectionReader{}, NewPostgresShadowAILibrary(agentrepo.New(conn)), NewLiveOrgAdminAuthorizer(conn, engine), budgets.SensitiveDiagnostics))

	diagnostics := NewDiagnosticsService(conn, telemetryrepo.New(chConn), enabled, reader, readiness, budgets.Diagnostics, literalIdentityGate{}).
		WithToolUsageBreakdown(telemetryrepo.New(chConn)).
		WithDrilldown(telemetryrepo.New(chConn), testServicesKey, budgets.SensitiveDiagnostics, budgets.DrilldownVolume, NewPostgresDrilldownAuditor(conn)).
		WithUserSearch(telemetryrepo.New(chConn)).
		WithToolCallSearch(telemetryrepo.New(chConn))
	riskControls := NewRiskMutationControls(conn, flags, organizationSlugs, budgets.RiskMutations, testServicesKey)
	riskMutations := NewRiskMutationHandlers(
		conn,
		riskControls,
		risk.NewPolicyMutationCore(conn, auditLogger, approvals, noopRiskPolicySignaler{}, shadowmcp.NewClient(logger, conn, cache.NewRedisCacheAdapter(redisClient), serverURL)),
		risk.NewExclusionMutationCore(logger, conn, auditLogger, &recordingRiskExclusionReconciler{}, testServicesKey),
		risk.NewFalsePositiveCore(auditLogger),
		testRiskPolicyCatalog(t),
	)

	return Services{
		Reader:              reader,
		Catalog:             catalog,
		Registrations:       registrations,
		SetupResources:      []SetupResource{testSetupResource()},
		Feedback:            NewFeedbackService(conn),
		Onboarding:          NewOnboardingService(conn),
		Distributions:       distributions,
		Skills:              testSkillsService(t, &recordingSkillsManagement{}).WithInsights(telemetryrepo.New(chConn), budgets.Diagnostics),
		Diagnostics:         diagnostics,
		WorkflowRun:         NewWorkflowRunService(logger, &captureWorkflowRunEmitter{}),
		Plugins:             pluginInventory,
		SessionRecall:       NewSessionRecallService(logger, conn, platformrepo.New(conn), auditLogger, enabled, budgets.SensitiveSessionRecall),
		RiskMutations:       riskMutations,
		Candidate:           CatalogDescriptor{},
		AccessReads:         accessReads,
		AccessRoleMutations: NewAccessRoleMutationService(accessReads, flags, budgets.AccessRoleMutations, testServicesKey, access.NewRoleManager(logger, conn, workos.NewStubClient(), auditLogger)),
		ConnectionMutations: NewMCPConnectionMutationService(conn, NewMCPConnectionSettingsService(conn), mcpendpoints.NewService(logger, tracerProvider, conn, sessions, engine, auditLogger, temporalEnv, false, distributionGuard), auditLogger, engine, allowConnectionNetworkAccess{}, plugins.PublicationRequests{Enabled: false}, nil),
	}
}

func testRiskPolicyCatalog(t *testing.T) policycatalog.Catalog {
	t.Helper()
	catalog, err := policycatalog.Build()
	require.NoError(t, err)
	return catalog
}

// newTestServer registers the full tool catalogue over real services. Callers
// may swap one capability for a test double before registration.
func newTestServer(t *testing.T, override ...func(*Services)) (*mcp.Server, *Registrar) {
	t.Helper()
	services := newTestServices(t)
	for _, apply := range override {
		apply(&services)
	}
	services.Reader.configureKeyMaterial(testServicesKey, testRiskPolicyCatalog(t))
	return newServer(services, testServicesKey)
}

// newTestRuntime builds a runtime over real services with the given edge
// collaborators.
func newTestRuntime(t *testing.T, authenticator Authenticator, gate Gate, authorizer Authorizer, protectedResourceURL string, readiness ReadinessRecorder) *Runtime {
	t.Helper()
	runtime := NewRuntime(testenv.NewLogger(t), authenticator, gate, authorizer, protectedResourceURL, testServicesKey, testRiskPolicyCatalog(t), readiness, newTestServices(t))
	return runtime
}
