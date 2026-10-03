// Package platformmcptest composes a complete Platform MCP service set for
// tests in other packages.
package platformmcptest

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"testing"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/server/internal/access"
	agentrepo "github.com/speakeasy-api/gram/server/internal/agent/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/email"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/mcp/tunnelrouting"
	"github.com/speakeasy-api/gram/server/internal/mcpapproval"
	"github.com/speakeasy-api/gram/server/internal/mcpapproval/mcpapprovaltest"
	"github.com/speakeasy-api/gram/server/internal/mcpendpoints"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/networkaccess"
	oauthregistration "github.com/speakeasy-api/gram/server/internal/oauth/registration"
	"github.com/speakeasy-api/gram/server/internal/oktaresourceconnections"
	otelchrepo "github.com/speakeasy-api/gram/server/internal/otel/chrepo"
	"github.com/speakeasy-api/gram/server/internal/platformmcp"
	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
	"github.com/speakeasy-api/gram/server/internal/plugins"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/ratelimit"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/risk"
	"github.com/speakeasy-api/gram/server/internal/risk/analysisstatus"
	riskchrepo "github.com/speakeasy-api/gram/server/internal/risk/chrepo"
	"github.com/speakeasy-api/gram/server/internal/risk/policycatalog"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp/admission"
	"github.com/speakeasy-api/gram/server/internal/skills"
	telemetryrepo "github.com/speakeasy-api/gram/server/internal/telemetry/repo"
	"github.com/speakeasy-api/gram/server/internal/temporal"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/loops"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/posthog"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/tunnel/route"
)

// Deps is the shared infrastructure a test already holds.
type Deps struct {
	Logger          *slog.Logger
	TracerProvider  trace.TracerProvider
	MeterProvider   metric.MeterProvider
	DB              *pgxpool.Pool
	Redis           *redis.Client
	ClickHouse      clickhouse.Conn
	Encryption      *encryption.Client
	Sessions        *sessions.Manager
	Authz           *authz.Engine
	Flags           feature.Provider
	ProductFeatures *productfeatures.Client
	GuardianPolicy  *guardian.Policy
	ServerURL       *url.URL
	DashboardURL    *url.URL
	KeyMaterial     string
	TemporalEnv     *temporal.Environment
}

// NewServices composes every Platform MCP capability the way the server does.
func NewServices(t *testing.T, deps Deps) platformmcp.Services {
	t.Helper()

	ctx := t.Context()
	logger, db := deps.Logger, deps.DB
	auditLogger := audit.NewLogger()
	limitStore := ratelimit.NewRedisStore(deps.Redis)
	budget := func(name string) platformmcp.OperationBudget {
		return platformmcp.OperationBudget{
			Connection:   ratelimit.New(limitStore, name+"-connection", ratelimit.PerMinute(1000)),
			Organization: ratelimit.New(limitStore, name+"-organization", ratelimit.PerMinute(1000)),
		}
	}
	budgets := platformmcp.OperationBudgets{
		RiskFindings: budget("risk-findings"), Catalog: budget("catalog"), Registration: budget("registration"),
		ReviewRequests: budget("review-requests"), Handoff: budget("handoff"), SetupStart: budget("setup"),
		Repair: budget("repair"), Docs: budget("docs"), Skills: budget("skills"), LifecycleMetadata: budget("lifecycle"),
		Plugins: budget("plugins"), AccessReads: budget("access-reads"), AccessRoleMutations: budget("access-role-mutations"),
		Diagnostics: budget("diagnostics"), SensitiveDiagnostics: budget("sensitive-diagnostics"),
		SensitiveSessionRecall: budget("session-recall"), RiskMutations: budget("risk-mutations"),
		DrilldownVolume: platformmcp.DrilldownVolumeBudget{
			Rows:          ratelimit.New(limitStore, "drilldown-rows", ratelimit.PerMinute(1000)),
			MetricQueries: ratelimit.New(limitStore, "drilldown-metric-queries", ratelimit.PerMinute(1000)),
		},
	}
	enabled := platformmcp.FeatureChecker(func(context.Context, string) (bool, error) { return true, nil })
	organizationSlugs := platformmcp.NewPostgresOrganizationSlugResolver(db)
	guard := admission.NewGuard(deps.Flags, admission.NewReportMetrics(deps.MeterProvider, logger))
	tunnels := tunnelrouting.NewHTTPClient(route.NewRedis(deps.Redis), "test-forward-token", deps.GuardianPolicy, nil)
	challenges := remotesessions.NewChallengeManager(logger, deps.TracerProvider, deps.MeterProvider, db, deps.Encryption, deps.GuardianPolicy, tunnels, cache.NewRedisCacheAdapter(deps.Redis), deps.ServerURL)
	gate := platformmcp.NewCatalogRegistrationGate(platformmcp.NewOrganizationGate(deps.ProductFeatures))
	store := platformmcp.NewRegistrationStore(db)
	readiness := platformmcp.NewReadinessService(store, gate, platformmcp.NewProviderAdapters(nil), ratelimit.New(limitStore, "forced-readiness", ratelimit.PerMinute(1000)), budgets.Repair, platformmcp.NewRemoteMCPReadinessProber(logger, db, deps.Encryption, deps.GuardianPolicy, challenges))
	catalog := platformmcp.NewRegistryCatalogSources(nil)
	publish := platformmcp.ProjectPublisher(func(context.Context, uuid.UUID, string, string) error { return nil })

	lifecycleMetadata := platformmcp.NewLifecycleMetadataService(db, func(ctx context.Context, tx pgx.Tx, existing mcpserversrepo.McpServer, input platformmcp.LifecycleMetadataUpdate) (mcpserversrepo.McpServer, error) {
		name := input.Name
		return mcpservers.UpdateMCPServerLifecycleInTransaction(ctx, tx, auditLogger, existing, lifecycleInput(existing, input.OrganizationID, input.ProjectID, input.ActorUserID, input.ServerID, &name, existing.Visibility))
	}, deps.KeyMaterial)
	lifecycleVisibility := platformmcp.NewLifecycleVisibilityService(db, auditLogger, mcpservers.LockMCPServerVisibilityDependencies, func(ctx context.Context, tx pgx.Tx, existing mcpserversrepo.McpServer, input platformmcp.LifecycleVisibilityUpdate) (platformmcp.LifecycleVisibilityUpdateResult, error) {
		updated, err := mcpservers.UpdateMCPServerVisibilityInTransaction(ctx, tx, auditLogger, existing, lifecycleInput(existing, input.OrganizationID, input.ProjectID, input.ActorUserID, input.ServerID, nil, input.Visibility))
		if err != nil {
			return platformmcp.LifecycleVisibilityUpdateResult{}, fmt.Errorf("update mcp server visibility: %w", err)
		}
		return platformmcp.LifecycleVisibilityUpdateResult{Server: updated.Server, ClearedRootDomainIDs: updated.ClearedRootDomainIDs}, nil
	}, publish, func(context.Context, []uuid.UUID) error { return nil }, readiness, deps.KeyMaterial, guard, organizationSlugs)

	registrations := platformmcp.NewRegistrationService(catalog, gate, store).
		WithDirectRemoteInspector(platformmcp.NewGuardianDirectRemoteInspector(deps.GuardianPolicy)).
		WithLifecycleMetadata(lifecycleMetadata).
		WithLifecycleVisibility(lifecycleVisibility).
		WithOperationBudgets(budgets).
		WithReadiness(readiness).
		WithDashboardURL(deps.DashboardURL).
		WithIdentityProviderAttachment(platformmcp.NewCatalogIdentityProviderAttachmentService(logger, deps.MeterProvider, db, remotesessions.NewIdentityCommitter(logger, db, deps.Encryption, auditLogger, deps.ServerURL, deps.GuardianPolicy, tunnels, oauthregistration.NewMetrics(logger, deps.MeterProvider)), deps.GuardianPolicy, deps.ServerURL)).
		WithClientAdmission(platformmcp.NewClientAdmissionService(db, auditLogger))

	distributionReads := platformmcp.NewShadowDistributionReadService(logger, db, guard, organizationSlugs)
	pluginInventory := platformmcp.NewPluginsService(db, budgets.Plugins, deps.KeyMaterial, guard).
		WithAuthorization(deps.Authz).
		WithRemoteSessions(challenges).
		WithInstallLinks(deps.DashboardURL, deps.ServerURL).
		WithAssignmentMutations(deps.Flags, organizationSlugs, auditLogger, budget("plugin-assignments")).
		WithDistributionAdmissionReads(distributionReads)
	distributions := platformmcp.NewDistributionService(db, auditLogger, func(ctx context.Context, tx pgx.Tx, authCtx *contextvalues.AuthContext, organizationID string, projectID, pluginID, mcpServerID uuid.UUID, displayName string) (uuid.UUID, bool, error) {
		attached, err := plugins.AttachToExistingPluginAudited(ctx, tx, auditLogger, authCtx, organizationID, projectID, pluginID, mcpServerID, displayName)
		if err != nil {
			return uuid.Nil, false, fmt.Errorf("attach mcp server to plugin: %w", err)
		}
		if attached == nil {
			return uuid.Nil, false, nil
		}
		return attached.Server.ID, true, nil
	}, publish, pluginInventory, guard, organizationSlugs)

	approvals := mcpapproval.NewService(logger, deps.TracerProvider, db, deps.Sessions, deps.Authz, deps.Flags, auditLogger, mcpapprovaltest.NewAssembler(telemetryrepo.New(deps.ClickHouse)), mcpapprovaltest.StartResearch)
	authorizer := platformmcp.NewLiveOrgAdminAuthorizer(db, deps.Authz)
	reader := platformmcp.NewPostgresReader(logger, db).
		WithXAAReadiness(oktaresourceconnections.NewService(logger, deps.TracerProvider, db, deps.Sessions, deps.Authz, auditLogger, deps.Flags), deps.Flags).
		WithAuthorization(deps.Authz).
		WithReviewRequests(approvals, budgets.ReviewRequests).
		WithDataExports(deps.Encryption, deps.DashboardURL).
		WithDataExportMutations(auditLogger, deps.DashboardURL).
		WithRecentToolCalls(telemetryrepo.New(deps.ClickHouse), deps.DashboardURL).
		WithMCPNetworkTraffic(telemetryrepo.New(deps.ClickHouse), enabled).
		WithOrganizationEvents(otelchrepo.New(deps.ClickHouse), enabled, deps.DashboardURL).
		WithNetworkIngressStatus(deps.DashboardURL).
		WithRiskAnalysisStatus(platformmcp.NewRiskAnalysisStatusService(logger, db, noopSignaler{}, deps.Flags, organizationSlugs)).
		WithRiskFindings(platformmcp.NewRiskFindingsService(db, riskchrepo.New(deps.ClickHouse), deps.Flags, organizationSlugs, deps.KeyMaterial), budgets.RiskFindings).
		WithRiskFindingList(platformmcp.NewRiskFindingListService(db, riskchrepo.New(deps.ClickHouse), deps.KeyMaterial), budgets.RiskFindings).
		WithChatMetadata(platformmcp.NewChatMetadataService(db, budgets.SensitiveDiagnostics, deps.KeyMaterial)).
		WithToolExposure(platformmcp.NewMCPToolExposureService(logger, db, auditLogger, deps.Authz, authorizer, deps.KeyMaterial, plugins.PublicationRequests{Enabled: false}, nil, budget("tool-exposure-reads"), budget("tool-exposure-changes"), func(context.Context, uuid.UUID, uuid.UUID) error { return nil }))
	shadowAccess := access.NewService(logger, deps.TracerProvider, db, deps.ClickHouse, deps.Sessions,
		access.NewRoleManager(logger, db, workos.NewStubClient(), auditLogger), deps.Authz, auditLogger,
		email.NewService(logger, loops.New(ctx, logger, deps.GuardianPolicy, ""), email.NewTemplateIDs(nil), false),
		deps.DashboardURL, literalIdentityGate{})
	shadowInventory := platformmcp.NewShadowInventoryService(shadowAccess, approvals, deps.Flags, organizationSlugs, platformrepo.New(db), budgets.SensitiveDiagnostics, deps.KeyMaterial)
	reader.WithShadowInventory(shadowInventory.WithDistributionAdmissionReads(distributionReads)).
		WithShadowDecisions(platformmcp.NewShadowDecisionService(db, shadowInventory, approvals, pluginInventory, deps.Flags, organizationSlugs, budget("shadow-decisions"))).
		WithShadowAI(platformmcp.NewShadowAIService(shadowAccess, platformmcp.NewPostgresShadowAILibrary(agentrepo.New(db)), authorizer, budgets.SensitiveDiagnostics))

	diagnostics := platformmcp.NewDiagnosticsService(db, telemetryrepo.New(deps.ClickHouse), enabled, reader, readiness, budgets.Diagnostics, literalIdentityGate{}).
		WithToolUsageBreakdown(telemetryrepo.New(deps.ClickHouse)).
		WithDrilldown(telemetryrepo.New(deps.ClickHouse), deps.KeyMaterial, budgets.SensitiveDiagnostics, budgets.DrilldownVolume, platformmcp.NewPostgresDrilldownAuditor(db)).
		WithUserSearch(telemetryrepo.New(deps.ClickHouse)).
		WithToolCallSearch(telemetryrepo.New(deps.ClickHouse))
	riskControls := platformmcp.NewRiskMutationControls(db, deps.Flags, organizationSlugs, budgets.RiskMutations, deps.KeyMaterial)
	riskPolicyCatalog, err := policycatalog.Build()
	require.NoError(t, err)
	riskMutations := platformmcp.NewRiskMutationHandlers(
		db,
		riskControls,
		risk.NewPolicyMutationCore(db, auditLogger, approvals, noopSignaler{}, shadowmcp.NewClient(logger, db, cache.NewRedisCacheAdapter(deps.Redis), deps.ServerURL)),
		risk.NewExclusionMutationCore(logger, db, auditLogger, noopSignaler{}, deps.KeyMaterial),
		risk.NewFalsePositiveCore(auditLogger),
		riskPolicyCatalog,
	)

	roleManager := access.NewRoleManager(logger, db, workos.NewStubClient(), auditLogger)
	accessReads := platformmcp.NewAccessReadService(logger, db, roleManager, budgets.AccessReads, deps.KeyMaterial)
	skillsManagement := skills.NewService(logger, deps.TracerProvider, db, deps.Sessions, deps.Authz, deps.ProductFeatures, auditLogger, noopSignaler{}, noopSignaler{}, deps.DashboardURL)
	var candidate platformmcp.CatalogDescriptor
	return platformmcp.Services{
		Reader:              reader,
		Catalog:             catalog,
		Registrations:       registrations,
		SetupResources:      nil,
		Feedback:            platformmcp.NewFeedbackService(db),
		Onboarding:          platformmcp.NewOnboardingService(db),
		Distributions:       distributions,
		Skills:              platformmcp.NewSkillsService(skillsManagement, platformmcp.NewPostgresSkillTargets(db), store, deps.Authz, gate, budgets.Skills).WithInsights(telemetryrepo.New(deps.ClickHouse), budgets.Diagnostics),
		Diagnostics:         diagnostics,
		WorkflowRun:         platformmcp.NewWorkflowRunService(logger, posthog.New(ctx, logger, "", "", "")),
		Plugins:             pluginInventory,
		SessionRecall:       platformmcp.NewSessionRecallService(logger, db, platformrepo.New(db), auditLogger, enabled, budgets.SensitiveSessionRecall),
		RiskMutations:       riskMutations,
		Candidate:           candidate,
		AccessReads:         accessReads,
		AccessRoleMutations: platformmcp.NewAccessRoleMutationService(accessReads, deps.Flags, budgets.AccessRoleMutations, deps.KeyMaterial, roleManager),
		ConnectionMutations: platformmcp.NewMCPConnectionMutationService(db, platformmcp.NewMCPConnectionSettingsService(db), mcpendpoints.NewService(logger, deps.TracerProvider, db, deps.Sessions, deps.Authz, auditLogger, deps.TemporalEnv, false, guard), auditLogger, deps.Authz, allowNetworkAccess{}, plugins.PublicationRequests{Enabled: false}, nil),
	}
}

func lifecycleInput(existing mcpserversrepo.McpServer, organizationID string, projectID uuid.UUID, actorUserID string, serverID uuid.UUID, name *string, visibility string) mcpservers.LifecycleUpdateInput {
	return mcpservers.LifecycleUpdateInput{
		OrganizationID: organizationID, ProjectID: projectID, ActorUserID: actorUserID, ActorEmail: nil,
		ServerID: serverID, Name: name, Visibility: visibility, EnvironmentID: existing.EnvironmentID,
		UserSessionIssuerID: existing.UserSessionIssuerID, RemoteMcpServerID: existing.RemoteMcpServerID,
		TunneledMcpServerID: existing.TunneledMcpServerID, ToolsetID: existing.ToolsetID,
		UnproxiedMcpServerID: existing.UnproxiedMcpServerID, ToolVariationsGroupID: existing.ToolVariationsGroupID,
		NetworkAccessMode: nil,
	}
}

type literalIdentityGate struct{}

func (literalIdentityGate) CanonicalOrgFor(context.Context, string) string { return "" }

// noopSignaler accepts every background signal without starting work.
type noopSignaler struct{}

func (noopSignaler) Signal(context.Context, uuid.UUID) error                      { return nil }
func (noopSignaler) Reconcile(context.Context, uuid.UUID, uuid.UUID) error        { return nil }
func (noopSignaler) SignalManual(context.Context, uuid.UUID, uuid.UUID) error     { return nil }
func (noopSignaler) SignalPluginPublish(context.Context, uuid.UUID, string) error { return nil }
func (noopSignaler) Describe(context.Context, uuid.UUID) (analysisstatus.Status, error) {
	return analysisstatus.Status{State: analysisstatus.StateNever, RunningSince: nil, LastRunStartedAt: nil, LastRunAt: nil, LastRunOutcome: ""}, nil
}

type allowNetworkAccess struct{}

func (allowNetworkAccess) PrepareNetworkAccess(context.Context, networkaccess.EligibilityInput) (networkaccess.AdmissionFinalizer, error) {
	return networkaccess.NewAdmissionFinalizer(func(context.Context, pgx.Tx) error { return nil }), nil
}
