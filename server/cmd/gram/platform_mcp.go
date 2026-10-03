//nolint:exhaustruct,wrapcheck // Composition intentionally relies on documented optional zero values.
package gram

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"

	"github.com/speakeasy-api/gram/server/internal/access"
	agentrepo "github.com/speakeasy-api/gram/server/internal/agent/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/auth/identity"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/background"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/externalmcp"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/mcpapproval"
	"github.com/speakeasy-api/gram/server/internal/mcpendpoints"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/networkaccess"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oktaresourceconnections"
	"github.com/speakeasy-api/gram/server/internal/platformmcp"
	"github.com/speakeasy-api/gram/server/internal/platformmcp/localfixture"
	"github.com/speakeasy-api/gram/server/internal/platformmcp/remotesessionprovider"
	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
	"github.com/speakeasy-api/gram/server/internal/platformmcp/setupcorpus"
	"github.com/speakeasy-api/gram/server/internal/plugins"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/ratelimit"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/risk"
	"github.com/speakeasy-api/gram/server/internal/risk/analysisstatus"
	"github.com/speakeasy-api/gram/server/internal/risk/policycatalog"
	"github.com/speakeasy-api/gram/server/internal/risk/policycore"
	"github.com/speakeasy-api/gram/server/internal/sessiontokens"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp/admission"
	tenv "github.com/speakeasy-api/gram/server/internal/temporal"
	"github.com/speakeasy-api/gram/server/internal/toolsets"
)

type platformMCPConfig struct {
	Logger                 *slog.Logger
	MeterProvider          metric.MeterProvider
	TracerProvider         trace.TracerProvider
	Mux                    goahttp.Muxer
	DB                     *pgxpool.Pool
	Redis                  *redis.Client
	ServerURL              *url.URL
	DashboardURL           *url.URL
	Environment            string
	JWTSigningKey          string
	RiskPolicyCatalog      policycatalog.Catalog
	ProductFeatures        *productfeatures.Client
	FeatureFlags           feature.Provider
	DistributionAdmission  *admission.Guard
	Authz                  *authz.Engine
	Encryption             *encryption.Client
	Identity               *identity.Resolver
	Sessions               *sessions.Manager
	Registry               *externalmcp.RegistryClient
	Catalog                *externalmcp.CatalogService
	GuardianPolicy         *guardian.Policy
	RemoteChallengeManager *remotesessions.ChallengeManager
	IdentityCommitter      *remotesessions.IdentityCommitter
	AuditLogger            *audit.Logger
	AccessRoles            access.RoleProvider
	PluginPublisher        *plugins.Service
	PluginPublishSignaler  plugins.PluginPublishSignaler
	NetworkAccessAdmission networkaccess.EligibilityChecker
	PublicationRequests    plugins.PublicationRequests
	TemporalEnv            *tenv.Environment
	Skills                 platformmcp.SkillsManagement
	// CallbackOrigin is the origin of the redirect_uri a remote session client
	// created now registers. The setup guides show that URL.
	CallbackOrigin *url.URL
	// OutboundCallbackOrigin is the pinned origin of the identity provider
	// callback the Platform MCP OAuth login registers.
	OutboundCallbackOrigin *url.URL
	// SkillInsights is the ClickHouse read behind the skill insight tools.
	SkillInsights            platformmcp.SkillInsightsReader
	RiskPolicyApprovals      policycore.ApprovalCoordinator
	RiskPolicySignaler       policycore.PolicySignaler
	RiskPolicyCache          policycore.PolicyCacheInvalidator
	RiskExclusionReconciler  risk.RiskExclusionReconciler
	RegistryDiscoveryEnabled bool
	// RiskAnalysisDescriber reports the run state of a project's Watchdog
	// analysis.
	RiskAnalysisDescriber analysisstatus.Describer
	RiskFindings          platformmcp.RiskFindingsReader
	// RiskFindingList is the ClickHouse read path for individual findings.
	RiskFindingList platformmcp.RiskFindingListReader
	// Telemetry is the Gram-owned ClickHouse read model the diagnostics tools
	// answer from.
	Telemetry platformmcp.DiagnosticsTelemetryReader
	// ToolUsage is the target-aware tool usage read behind
	// get_tool_usage_summary: the same pipeline the dashboard Insights board
	// and the managed platform_get_tool_usage_summary tool read.
	ToolUsage platformmcp.ToolUsageBreakdownReader
	// SessionCapture resolves the organization's metrics mode, which decides
	// what a project overview's active-user count measures. Shared with the
	// telemetry service so both surfaces answer from the same source.
	SessionCapture platformmcp.FeatureChecker
	// CanonicalIdentity applies the telemetry service's rollout-aware email fold
	// to usage attribution.
	CanonicalIdentity platformmcp.CanonicalIdentityGate
	// SessionPortability gates the session-recall tools (list_my_sessions /
	// continue_session). Sibling of SessionCapture: capture records sessions,
	// portability serves them back as redacted handoff digests.
	SessionPortability platformmcp.FeatureChecker
	// TelemetryDrilldown is the row-level half of the same read model.
	TelemetryDrilldown platformmcp.DrilldownTelemetryReader
	// UserSearch is the per-person read model behind search_users and
	// get_user_metrics_summary.
	UserSearch platformmcp.UserSearchReader
	// ToolCallSearch is the bounded Tool Logs summary list and attribute key
	// inventory behind search_tool_calls and list_attribute_keys.
	ToolCallSearch platformmcp.ToolCallSearchReader

	// WorkflowRun delivers shipped-workflow run reports to Speakeasy's own
	// analytics.
	WorkflowRun platformmcp.WorkflowRunEmitter
	// RecentToolCalls reads only the bounded Tool Logs summary path.
	RecentToolCalls platformmcp.RecentToolCallReader
	NetworkTraffic  platformmcp.MCPNetworkTrafficReader
	// EventFeed reads the org-scoped OpenTelemetry event feed.
	EventFeed platformmcp.EventFeedReader
	// LogsEnabled is the same product-feature gate the dashboard Event Feed
	// uses. A false result for the caller's organization withholds live
	// organization-event reads.
	LogsEnabled     platformmcp.FeatureChecker
	ShadowInventory *access.Service
	ShadowReview    *mcpapproval.Service
	LocalFixture    *platformMCPLocalFixtureConfig
}

var platformMCPLocalFixtureLoopbackCIDRBlocks = []string{"127.0.0.0/8", "::1/128"}

const platformMCPLocalFixtureReadinessLifetime = 15 * time.Minute

// configurePlatformMCP composes the Platform MCP HTTP surfaces separately from
// the general server startup flow. Dashboard and MCP authentication remain at
// their respective transports; shared management reads are composed inside the
// Platform MCP runtime.
// AssistantSurface is what a project's managed assistant needs to reach the
// Platform MCP catalogue: the tools admitted to its audience, and the
// authorizer every one of its calls is rechecked against. Reviewed guides
// reach it through read_gram_doc rather than a second resource channel — the
// assistant's tool transport has no resources/* methods to serve.
type AssistantSurface struct {
	Tools      []platformmcp.Descriptor
	Authorizer platformmcp.Authorizer
}

func configurePlatformMCP(ctx context.Context, config platformMCPConfig) (AssistantSurface, error) {
	if config.LocalFixture != nil {
		return configureLocalFixturePlatformMCP(ctx, config)
	}
	return configureBrowserPlatformMCP(ctx, config)
}

func configureLocalFixturePlatformMCP(ctx context.Context, config platformMCPConfig) (AssistantSurface, error) {
	fixtureConfig := config.LocalFixture.Fixture
	if config.RegistryDiscoveryEnabled {
		fixtureConfig.SetRegistryPrefix("/platform-mcp/local-fixture/registry")
	}
	if err := config.Registry.ClearCache(ctx, fixtureConfig.Registry().URL); err != nil {
		return AssistantSurface{}, fmt.Errorf("clear local Platform MCP fixture registry cache: %w", err)
	}

	gate := platformmcp.NewOrganizationGate(config.ProductFeatures)
	authorizer := platformmcp.NewLiveOrgAdminAuthorizer(config.DB, config.Authz).WithDashboardURL(config.DashboardURL).WithServerURL(config.ServerURL)
	oauthTelemetry := platformmcp.NewOAuthTelemetry(config.Logger, config.MeterProvider)
	oauthStore := platformmcp.NewPostgresOAuthStore(config.DB).WithTelemetry(oauthTelemetry)
	oauth := platformmcp.NewOAuthHTTP(platformmcp.OAuthHTTPConfig{
		BaseURL:       config.ServerURL,
		Environment:   config.Environment,
		Cache:         cache.NewRedisCacheAdapter(config.Redis),
		Store:         oauthStore,
		Identity:      config.Identity,
		Gate:          gate,
		Authorizer:    authorizer,
		Organizations: platformmcp.NewLiveOrganizationSelector(config.DB, authorizer),
		Signer:        sessiontokens.NewSigner(config.JWTSigningKey),
		Encryption:    config.Encryption,
		Telemetry:     oauthTelemetry,
		Logger:        config.Logger,
		// Backs the inbound CIMD document fetcher's SSRF protection.
		GuardianPolicy:     config.GuardianPolicy,
		MeterProvider:      config.MeterProvider,
		IDPCallbackBaseURL: config.OutboundCallbackOrigin,
	})
	authenticator := platformmcp.NewJWTAuthenticator(sessiontokens.NewSigner(config.JWTSigningKey), config.DB, config.Encryption, config.ServerURL)

	fixtureOAuth := localfixture.NewOAuthHTTP(fixtureConfig)
	fixtureMCP := localfixture.NewMCPHTTP(fixtureOAuth)
	fixtureRegistry := config.Registry.WithAllowedCIDRBlocks(platformMCPLocalFixtureLoopbackCIDRBlocks...)
	catalog := platformmcp.NewDynamicRegistryCatalogSources(func(ctx context.Context) ([]platformmcp.RegistryCatalogSource, error) {
		browserSources, err := loadBrowserPlatformMCPCatalogDescriptors(ctx, config.Catalog)
		if err != nil {
			return nil, err
		}
		return append(browserSources, platformmcp.RegistryCatalogSource{Client: fixtureRegistry, Descriptors: []platformmcp.CatalogDescriptor{fixtureConfig.CatalogDescriptor()}}), nil
	})
	store := platformmcp.NewRegistrationStore(config.DB)
	registrationGate := platformmcp.NewCatalogRegistrationGate(gate)
	fixtureAdapter := remotesessionprovider.New(
		config.GuardianPolicy,
		config.RemoteChallengeManager,
		remotesessionprovider.Descriptor{
			ProviderKey:                localfixture.ProviderKey,
			RemoteSessionIssuerID:      fixtureConfig.RemoteSessionIssuerID(),
			StreamableHTTPURL:          fixtureConfig.RemoteURL(),
			ProviderSetupCompletionURL: oauth.ProviderSetupCompletionURL(),
			Resource:                   fixtureConfig.RemoteURL(),
			TestOnlyAllowedCIDRBlocks:  platformMCPLocalFixtureLoopbackCIDRBlocks,
			TestOnlyReadinessLifetime:  platformMCPLocalFixtureReadinessLifetime,
		},
		localfixture.NewClientConfigurator(fixtureConfig, fixtureOAuth, config.DB, config.GuardianPolicy),
	)
	adapters := platformmcp.NewProviderAdapters([]platformmcp.ProviderAdapter{fixtureAdapter})
	limitStore := ratelimit.NewRedisStore(config.Redis)
	newBudget := func(connectionName, organizationName string) platformmcp.OperationBudget {
		return platformmcp.OperationBudget{
			Connection:   ratelimit.New(limitStore, connectionName, ratelimit.PerMinute(5), ratelimit.WithMetrics(config.MeterProvider)),
			Organization: ratelimit.New(limitStore, organizationName, ratelimit.PerMinute(50), ratelimit.WithMetrics(config.MeterProvider)),
		}
	}
	budgets := platformmcp.OperationBudgets{
		Catalog:      newBudget(platformmcp.CatalogConnectionLimitName, platformmcp.CatalogOrganizationLimitName),
		Registration: newBudget(platformmcp.RegistrationConnectionLimitName, platformmcp.RegistrationOrganizationLimitName),
		ReviewRequests: platformmcp.OperationBudget{
			Connection:   ratelimit.New(limitStore, platformmcp.ReviewRequestConnectionLimitName, ratelimit.PerMinute(platformmcp.ReviewRequestsPerConnectionPerMinute), ratelimit.WithMetrics(config.MeterProvider)),
			Organization: ratelimit.New(limitStore, platformmcp.ReviewRequestOrganizationLimitName, ratelimit.PerMinute(platformmcp.ReviewRequestsPerOrganizationPerMinute), ratelimit.WithMetrics(config.MeterProvider)),
		},
		Handoff:    newBudget(platformmcp.HandoffConnectionLimitName, platformmcp.HandoffOrganizationLimitName),
		SetupStart: newBudget(platformmcp.SetupConnectionLimitName, platformmcp.SetupOrganizationLimitName),
		Repair:     newBudget(platformmcp.RepairConnectionLimitName, platformmcp.RepairOrganizationLimitName),
		// Documentation search is metered on its own allowances rather than the
		// shared five-per-minute budget: retrieval is in-process and reading is
		// what the corpus is for, so a caller researching a setup should not
		// spend the budget that its registration call needs.
		Docs: platformmcp.OperationBudget{
			Connection:   ratelimit.New(limitStore, platformmcp.DocsConnectionLimitName, ratelimit.PerMinute(platformmcp.DocsQueriesPerConnectionPerMinute), ratelimit.WithMetrics(config.MeterProvider)),
			Organization: ratelimit.New(limitStore, platformmcp.DocsOrganizationLimitName, ratelimit.PerMinute(platformmcp.DocsQueriesPerOrganizationPerMinute), ratelimit.WithMetrics(config.MeterProvider)),
		},
		Skills: platformmcp.OperationBudget{
			Connection:   ratelimit.New(limitStore, platformmcp.SkillsConnectionLimitName, ratelimit.PerMinute(platformmcp.SkillsOperationsPerConnectionPerMinute), ratelimit.WithMetrics(config.MeterProvider)),
			Organization: ratelimit.New(limitStore, platformmcp.SkillsOrganizationLimitName, ratelimit.PerMinute(platformmcp.SkillsOperationsPerOrganizationPerMinute), ratelimit.WithMetrics(config.MeterProvider)),
		},
		LifecycleMetadata: platformmcp.OperationBudget{
			Connection:   ratelimit.New(limitStore, platformmcp.LifecycleConnectionLimitName, ratelimit.PerMinute(platformmcp.LifecycleOperationsPerConnectionPerMinute), ratelimit.WithMetrics(config.MeterProvider)),
			Organization: ratelimit.New(limitStore, platformmcp.LifecycleOrganizationLimitName, ratelimit.PerMinute(platformmcp.LifecycleOperationsPerOrganizationPerMinute), ratelimit.WithMetrics(config.MeterProvider)),
		},
		RiskFindings: platformmcp.OperationBudget{
			Connection:   ratelimit.New(limitStore, platformmcp.RiskFindingsConnectionLimitName, ratelimit.PerMinute(platformmcp.RiskFindingsQueriesPerConnectionPerMinute), ratelimit.WithMetrics(config.MeterProvider)),
			Organization: ratelimit.New(limitStore, platformmcp.RiskFindingsOrganizationLimitName, ratelimit.PerMinute(platformmcp.RiskFindingsQueriesPerOrganizationPerMinute), ratelimit.WithMetrics(config.MeterProvider)),
		},
		RiskMutations: platformmcp.OperationBudget{
			Connection:   ratelimit.New(limitStore, platformmcp.RiskMutationConnectionLimitName, ratelimit.PerMinute(platformmcp.RiskMutationsPerConnectionPerMinute), ratelimit.WithMetrics(config.MeterProvider)),
			Organization: ratelimit.New(limitStore, platformmcp.RiskMutationOrganizationLimitName, ratelimit.PerMinute(platformmcp.RiskMutationsPerOrganizationPerMinute), ratelimit.WithMetrics(config.MeterProvider)),
		},

		// Diagnostics are read-only aggregate queries an administrator runs
		// while investigating, so they are metered well above the shared
		// five-per-minute mutation budget.
		Diagnostics: platformmcp.OperationBudget{
			Connection:   ratelimit.New(limitStore, platformmcp.DiagnosticsConnectionLimitName, ratelimit.PerMinute(platformmcp.DiagnosticQueriesPerConnectionPerMinute), ratelimit.WithMetrics(config.MeterProvider)),
			Organization: ratelimit.New(limitStore, platformmcp.DiagnosticsOrganizationLimitName, ratelimit.PerMinute(platformmcp.DiagnosticQueriesPerOrganizationPerMinute), ratelimit.WithMetrics(config.MeterProvider)),
		},
		Plugins: platformmcp.OperationBudget{
			Connection:   ratelimit.New(limitStore, platformmcp.PluginsConnectionLimitName, ratelimit.PerMinute(platformmcp.PluginQueriesPerConnectionPerMinute), ratelimit.WithMetrics(config.MeterProvider)),
			Organization: ratelimit.New(limitStore, platformmcp.PluginsOrganizationLimitName, ratelimit.PerMinute(platformmcp.PluginQueriesPerOrganizationPerMinute), ratelimit.WithMetrics(config.MeterProvider)),
		},
		AccessReads: platformmcp.OperationBudget{
			Connection:   ratelimit.New(limitStore, platformmcp.AccessReadsConnectionLimitName, ratelimit.PerMinute(platformmcp.AccessReadQueriesPerConnectionPerMinute), ratelimit.WithMetrics(config.MeterProvider)),
			Organization: ratelimit.New(limitStore, platformmcp.AccessReadsOrganizationLimitName, ratelimit.PerMinute(platformmcp.AccessReadQueriesPerOrganizationPerMinute), ratelimit.WithMetrics(config.MeterProvider)),
		},
		AccessRoleMutations: platformmcp.OperationBudget{
			Connection:   ratelimit.New(limitStore, platformmcp.AccessRoleMutationConnectionLimitName, ratelimit.PerMinute(platformmcp.AccessRoleMutationsPerConnectionPerMinute), ratelimit.WithMetrics(config.MeterProvider)),
			Organization: ratelimit.New(limitStore, platformmcp.AccessRoleMutationOrganizationLimitName, ratelimit.PerMinute(platformmcp.AccessRoleMutationsPerOrganizationPerMinute), ratelimit.WithMetrics(config.MeterProvider)),
		},
		// Metered separately and lower: personal-data reads (this and session
		// recall below, each on its own budget) must not be fundable by
		// spending the ordinary diagnostic allowance.
		SensitiveDiagnostics: platformmcp.OperationBudget{
			Connection:   ratelimit.New(limitStore, platformmcp.SensitiveDiagnosticsConnectionLimitName, ratelimit.PerMinute(platformmcp.SensitiveDiagnosticQueriesPerConnectionPerMinute), ratelimit.WithMetrics(config.MeterProvider)),
			Organization: ratelimit.New(limitStore, platformmcp.SensitiveDiagnosticsOrganizationLimitName, ratelimit.PerMinute(platformmcp.SensitiveDiagnosticQueriesPerOrganizationPerMinute), ratelimit.WithMetrics(config.MeterProvider)),
		},
		// Session recall serves whole-transcript digests, so it is metered on
		// its own low allowance that no other budget can fund.
		SensitiveSessionRecall: platformmcp.OperationBudget{
			Connection:   ratelimit.New(limitStore, platformmcp.SessionRecallConnectionLimitName, ratelimit.PerMinute(platformmcp.SessionRecallsPerConnectionPerMinute), ratelimit.WithMetrics(config.MeterProvider)),
			Organization: ratelimit.New(limitStore, platformmcp.SessionRecallOrganizationLimitName, ratelimit.PerMinute(platformmcp.SessionRecallsPerOrganizationPerMinute), ratelimit.WithMetrics(config.MeterProvider)),
		},
		// The second drill-down cap: what a connection may accumulate over ten
		// minutes, rather than how often it may call. Both buckets refill over
		// that window, so a caller paging steadily under the per-minute rate
		// still runs out of rows before it has walked a whole window.
		DrilldownVolume: platformmcp.DrilldownVolumeBudget{
			Rows: ratelimit.New(limitStore, platformmcp.DrilldownRowsLimitName, ratelimit.Rate{
				Tokens:   platformmcp.DrilldownRowsPerConnectionPerWindow,
				Interval: platformmcp.DrilldownVolumeWindow,
				Burst:    platformmcp.DrilldownRowsPerConnectionPerWindow,
			}, ratelimit.WithMetrics(config.MeterProvider)),
			MetricQueries: ratelimit.New(limitStore, platformmcp.DrilldownMetricQueriesLimitName, ratelimit.Rate{
				Tokens:   platformmcp.DrilldownMetricQueriesPerConnectionPerWindow,
				Interval: platformmcp.DrilldownVolumeWindow,
				Burst:    platformmcp.DrilldownMetricQueriesPerConnectionPerWindow,
			}, ratelimit.WithMetrics(config.MeterProvider)),
		},
	}
	telemetry := platformmcp.NewLifecycleTelemetry(config.Logger, config.MeterProvider)
	riskTelemetry := platformmcp.NewRiskTelemetry(config.Logger, config.MeterProvider)
	readiness := platformmcp.NewReadinessService(
		store,
		registrationGate,
		adapters,
		ratelimit.New(limitStore, platformmcp.ForcedReadinessProbeLimit, ratelimit.PerMinute(platformmcp.ForcedReadinessProbesPerMinute), ratelimit.WithMetrics(config.MeterProvider)),
		budgets.Repair,
		platformmcp.NewRemoteMCPReadinessProber(config.Logger, config.DB, config.Encryption, config.GuardianPolicy, config.RemoteChallengeManager),
	).WithTelemetry(telemetry)
	lifecycleMetadata := newPlatformMCPLifecycleMetadataService(config)
	lifecycleVisibility := newPlatformMCPLifecycleVisibilityService(config, readiness)
	registrations := platformmcp.NewRegistrationService(catalog, registrationGate, store).
		WithDirectRemoteInspector(platformmcp.NewGuardianDirectRemoteInspector(config.GuardianPolicy)).
		WithLifecycleMetadata(lifecycleMetadata).
		WithLifecycleVisibility(lifecycleVisibility).
		WithOperationBudgets(budgets).
		WithReadiness(readiness).
		WithDashboardURL(config.DashboardURL).
		WithIdentityProviderAttachment(platformmcp.NewCatalogIdentityProviderAttachmentService(config.Logger, config.MeterProvider, config.DB, config.IdentityCommitter, config.GuardianPolicy, config.ServerURL)).
		WithClientAdmission(platformmcp.NewClientAdmissionService(config.DB, config.AuditLogger)).
		WithTelemetry(telemetry)
	dashboardSetupStarter := platformmcp.NewDashboardSetupService(store, registrationGate, authorizer, adapters, budgets.SetupStart)
	feedback := platformmcp.NewFeedbackService(config.DB)
	workflowRun := platformmcp.NewWorkflowRunService(config.Logger, config.WorkflowRun)
	setupResources, err := platformMCPSetupResources(config)
	if err != nil {
		return AssistantSurface{}, err
	}
	pluginAssignmentMutationBudget := platformmcp.OperationBudget{
		Connection:   ratelimit.New(limitStore, platformmcp.PluginAssignmentMutationConnectionLimitName, ratelimit.PerMinute(platformmcp.PluginAssignmentMutationsPerConnectionPerMinute), ratelimit.WithMetrics(config.MeterProvider)),
		Organization: ratelimit.New(limitStore, platformmcp.PluginAssignmentMutationOrganizationLimitName, ratelimit.PerMinute(platformmcp.PluginAssignmentMutationsPerOrganizationPerMinute), ratelimit.WithMetrics(config.MeterProvider)),
	}
	organizationSlugs := platformmcp.NewPostgresOrganizationSlugResolver(config.DB)
	distributionAdmissionReads := platformmcp.NewShadowDistributionReadService(config.Logger, config.DB, config.DistributionAdmission, organizationSlugs)
	pluginInventory := platformmcp.NewPluginsService(config.DB, budgets.Plugins, config.JWTSigningKey, config.DistributionAdmission).
		WithAuthorization(config.Authz).
		WithRemoteSessions(config.RemoteChallengeManager).
		WithInstallLinks(config.DashboardURL, config.ServerURL).
		WithAssignmentMutations(config.FeatureFlags, organizationSlugs, config.AuditLogger, pluginAssignmentMutationBudget).
		WithDistributionAdmissionReads(distributionAdmissionReads)
	if config.PluginPublisher != nil {
		pluginInventory.WithPublicationEvidence(config.PluginPublisher)
	}
	pluginInventory.WithRepublish(config.PublicationRequests, config.PluginPublishSignaler, platformmcp.OperationBudget{
		Connection:   ratelimit.New(limitStore, platformmcp.PluginRepublishConnectionLimitName, ratelimit.PerMinute(platformmcp.PluginRepublishesPerConnectionPerMinute), ratelimit.WithMetrics(config.MeterProvider)),
		Organization: ratelimit.New(limitStore, platformmcp.PluginRepublishOrganizationLimitName, ratelimit.PerMinute(platformmcp.PluginRepublishesPerOrganizationPerMinute), ratelimit.WithMetrics(config.MeterProvider)),
	}).WithPublishStatus(&background.TemporalPluginPublisher{TemporalEnv: config.TemporalEnv})
	roleManager := access.NewRoleManager(config.Logger, config.DB, config.AccessRoles, config.AuditLogger)
	accessReads := platformmcp.NewAccessReadService(config.Logger, config.DB, roleManager, budgets.AccessReads, config.JWTSigningKey)
	accessRoleMutations := platformmcp.NewAccessRoleMutationService(accessReads, config.FeatureFlags, budgets.AccessRoleMutations, config.JWTSigningKey, roleManager)
	distributions := newPlatformMCPDistributionService(config, pluginInventory)

	// Keep the local fixture on its original paths unless discovery owns them.
	registryPrefix := ""
	if config.RegistryDiscoveryEnabled {
		registryPrefix = "/platform-mcp/local-fixture/registry"
	}
	registryHandler := localfixture.NewRegistryHTTP(fixtureConfig).Handler()
	if registryPrefix != "" {
		registryHandler = http.StripPrefix(registryPrefix, registryHandler)
	}
	config.Mux.Handle(http.MethodGet, registryPrefix+"/v0.1/servers", registryHandler.ServeHTTP)
	config.Mux.Handle(http.MethodGet, fixtureConfig.RegistryDetailsPath(), registryHandler.ServeHTTP)
	config.Mux.Handle(http.MethodGet, "/.well-known/oauth-authorization-server/platform-mcp/local-fixture", fixtureOAuth.Handler().ServeHTTP)
	config.Mux.Handle(http.MethodGet, "/platform-mcp/local-fixture/authorize", fixtureOAuth.Handler().ServeHTTP)
	config.Mux.Handle(http.MethodPost, "/platform-mcp/local-fixture/register", fixtureOAuth.Handler().ServeHTTP)
	config.Mux.Handle(http.MethodPost, "/platform-mcp/local-fixture/token", fixtureOAuth.Handler().ServeHTTP)
	config.Mux.Handle(http.MethodPost, "/platform-mcp/local-fixture/revoke", fixtureOAuth.Handler().ServeHTTP)
	config.Mux.Handle(http.MethodPost, "/platform-mcp/local-fixture/mcp", fixtureMCP.Handler().ServeHTTP)

	skillAuthoring := platformmcp.NewSkillsService(config.Skills, platformmcp.NewPostgresSkillTargets(config.DB), store, config.Authz, registrationGate, budgets.Skills).
		WithInsights(config.SkillInsights, budgets.Diagnostics)
	platformReader := platformmcp.NewPostgresReader(config.Logger, config.DB).
		WithXAAReadiness(oktaresourceconnections.NewService(config.Logger, config.TracerProvider, config.DB, config.Sessions, config.Authz, config.AuditLogger, config.FeatureFlags), config.FeatureFlags).
		WithAuthorization(config.Authz).
		WithReviewRequests(config.ShadowReview, budgets.ReviewRequests).
		WithDataExports(config.Encryption, config.DashboardURL).
		WithDataExportMutations(config.AuditLogger, config.DashboardURL).
		WithRecentToolCalls(config.RecentToolCalls, config.DashboardURL).
		WithMCPNetworkTraffic(config.NetworkTraffic, config.LogsEnabled).
		WithOrganizationEvents(config.EventFeed, config.LogsEnabled, config.DashboardURL).
		WithNetworkIngressStatus(config.DashboardURL).
		WithRiskAnalysisStatus(platformmcp.NewRiskAnalysisStatusService(config.Logger, config.DB, config.RiskAnalysisDescriber, config.FeatureFlags, organizationSlugs)).
		WithRiskFindings(platformmcp.NewRiskFindingsService(config.DB, config.RiskFindings, config.FeatureFlags, organizationSlugs, config.JWTSigningKey), budgets.RiskFindings).
		WithRiskFindingList(platformmcp.NewRiskFindingListService(config.DB, config.RiskFindingList, config.JWTSigningKey), budgets.RiskFindings).
		// Metered on the sensitive allowance: a chat page carries masked
		// participants and person references, like the drill-down reads.
		WithChatMetadata(platformmcp.NewChatMetadataService(config.DB, budgets.SensitiveDiagnostics, config.JWTSigningKey)).
		WithToolExposure(newPlatformMCPToolExposure(config, authorizer, limitStore))
	attachShadow(platformReader, config, authorizer, limitStore, pluginInventory, distributionAdmissionReads, organizationSlugs, budgets.SensitiveDiagnostics)
	diagnostics := platformmcp.NewDiagnosticsService(config.DB, config.Telemetry, config.SessionCapture, platformReader, readiness, budgets.Diagnostics, config.CanonicalIdentity).
		WithToolUsageBreakdown(config.ToolUsage).
		WithDrilldown(config.TelemetryDrilldown, config.JWTSigningKey, budgets.SensitiveDiagnostics, budgets.DrilldownVolume, platformmcp.NewPostgresDrilldownAuditor(config.DB)).
		WithUserSearch(config.UserSearch).
		WithToolCallSearch(config.ToolCallSearch)
	sessionRecall := platformmcp.NewSessionRecallService(config.Logger, config.DB, platformrepo.New(config.DB), audit.NewLogger(), config.SessionPortability, budgets.SensitiveSessionRecall)
	riskMutations := platformmcp.NewRiskMutationHandlers(
		config.DB,
		platformmcp.NewRiskMutationControls(config.DB, config.FeatureFlags, platformmcp.NewPostgresOrganizationSlugResolver(config.DB), budgets.RiskMutations, config.JWTSigningKey),
		risk.NewPolicyMutationCore(config.DB, config.AuditLogger, config.RiskPolicyApprovals, config.RiskPolicySignaler, config.RiskPolicyCache),
		risk.NewExclusionMutationCore(config.Logger, config.DB, config.AuditLogger, config.RiskExclusionReconciler, config.JWTSigningKey),
		risk.NewFalsePositiveCore(config.AuditLogger),
		config.RiskPolicyCatalog,
	)
	runtime := platformmcp.NewRuntime(
		config.Logger,
		authenticator,
		gate,
		authorizer,
		oauth.ProtectedResourceURL(),
		config.JWTSigningKey,
		config.RiskPolicyCatalog,
		platformmcp.NewPostgresReadinessRecorder(config.DB),
		platformmcp.Services{
			Reader:        platformReader,
			Catalog:       catalog,
			Registrations: registrations,
			// The fixture guide plus the reviewed corpus: local runs exercise the
			// synthetic provider, but they must see the same real guides production
			// serves or a corpus defect would only ever surface in production.
			SetupResources:      append(fixtureConfig.SetupResources(), setupResources...),
			Feedback:            feedback,
			Onboarding:          platformmcp.NewOnboardingService(config.DB),
			Distributions:       distributions,
			Skills:              skillAuthoring,
			Diagnostics:         diagnostics,
			WorkflowRun:         workflowRun,
			Plugins:             pluginInventory,
			SessionRecall:       sessionRecall,
			RiskMutations:       riskMutations,
			Candidate:           fixtureConfig.CatalogDescriptor(),
			AccessReads:         accessReads,
			AccessRoleMutations: accessRoleMutations,
			ConnectionMutations: newPlatformMCPConnectionMutations(config),
		},
	)
	runtime = runtime.WithOAuthTelemetry(oauthTelemetry).WithRiskTelemetry(riskTelemetry)
	oauth.Attach(config.Mux)
	platformmcp.NewDashboardSetupHTTP(dashboardSetupStarter, config.Sessions).Attach(config.Mux)
	platformmcp.AttachManagement(config.Mux, platformmcp.NewManagementService(config.Logger, config.TracerProvider, config.DB, config.Sessions, config.Authz, gate, authorizer, config.ServerURL.JoinPath("platform-mcp").String(), registrations, readiness, distributions, config.JWTSigningKey, catalog))
	o11y.AttachHandler(config.Mux, http.MethodPost, platformmcp.Path, runtime.Handler().ServeHTTP)
	return AssistantSurface{Tools: runtime.AssistantTools(), Authorizer: authorizer}, nil
}

// attachShadow registers the Shadow MCP inventory, review decisions, and the
// organization-scoped Shadow AI reads on both browser and local-fixture
// surfaces.
func attachShadow(reader *platformmcp.PostgresReader, config platformMCPConfig, authorizer platformmcp.Authorizer, limitStore ratelimit.Store, pluginInventory *platformmcp.PluginsService, distributionAdmissionReads *platformmcp.ShadowDistributionReadService, organizationSlugs *platformmcp.PostgresOrganizationSlugResolver, budget platformmcp.OperationBudget) {
	shadowInventory := platformmcp.NewShadowInventoryService(config.ShadowInventory, config.ShadowReview, config.FeatureFlags, organizationSlugs, platformrepo.New(config.DB), budget, config.JWTSigningKey)
	shadowInventory.WithDistributionAdmissionReads(distributionAdmissionReads)
	shadowDecisionBudget := platformmcp.OperationBudget{
		Connection:   ratelimit.New(limitStore, platformmcp.ShadowAccessDecisionConnectionLimitName, ratelimit.PerMinute(platformmcp.ShadowAccessDecisionsPerConnectionPerMinute), ratelimit.WithMetrics(config.MeterProvider)),
		Organization: ratelimit.New(limitStore, platformmcp.ShadowAccessDecisionOrganizationLimitName, ratelimit.PerMinute(platformmcp.ShadowAccessDecisionsPerOrganizationPerMinute), ratelimit.WithMetrics(config.MeterProvider)),
	}
	reader.WithShadowInventory(shadowInventory).
		WithShadowDecisions(platformmcp.NewShadowDecisionService(config.DB, shadowInventory, config.ShadowReview, pluginInventory, config.FeatureFlags, organizationSlugs, shadowDecisionBudget)).
		WithShadowAI(platformmcp.NewShadowAIService(config.ShadowInventory, platformmcp.NewPostgresShadowAILibrary(agentrepo.New(config.DB)), authorizer, budget))
}

// platformMCPSetupResources builds the reviewed setup corpus this deployment
// serves. A failure here is a composition failure, not a degraded feature: a
// corpus that silently lost a provider looks exactly like one that never
// covered it, and the model would be left to invent the steps.
func platformMCPSetupResources(config platformMCPConfig) ([]platformmcp.SetupResource, error) {
	// The redirect_uri a newly created client registers, derived the same way
	// externalmcp, remotesessions, and the dashboard derive it.
	callbackURL := remotesessions.RemoteLoginCallbackURL(config.CallbackOrigin)
	resources, err := setupcorpus.Build(setupcorpus.Options{OAuthCallbackURL: callbackURL})
	if err != nil {
		return nil, fmt.Errorf("build platform mcp setup corpus: %w", err)
	}
	return resources, nil
}

func newPlatformMCPLifecycleMetadataService(config platformMCPConfig) *platformmcp.LifecycleMetadataService {
	return platformmcp.NewLifecycleMetadataService(config.DB, func(ctx context.Context, tx pgx.Tx, existing mcpserversrepo.McpServer, input platformmcp.LifecycleMetadataUpdate) (mcpserversrepo.McpServer, error) {
		name := input.Name
		return mcpservers.UpdateMCPServerLifecycleInTransaction(ctx, tx, config.AuditLogger, existing, mcpservers.LifecycleUpdateInput{
			OrganizationID:        input.OrganizationID,
			ProjectID:             input.ProjectID,
			ActorUserID:           input.ActorUserID,
			ActorEmail:            nil,
			ServerID:              input.ServerID,
			Name:                  &name,
			Visibility:            existing.Visibility,
			EnvironmentID:         existing.EnvironmentID,
			UserSessionIssuerID:   existing.UserSessionIssuerID,
			RemoteMcpServerID:     existing.RemoteMcpServerID,
			TunneledMcpServerID:   existing.TunneledMcpServerID,
			ToolsetID:             existing.ToolsetID,
			UnproxiedMcpServerID:  existing.UnproxiedMcpServerID,
			ToolVariationsGroupID: existing.ToolVariationsGroupID,
		})
	}, config.JWTSigningKey)
}

func newPlatformMCPLifecycleVisibilityService(config platformMCPConfig, readiness *platformmcp.ReadinessService) *platformmcp.LifecycleVisibilityService {
	return platformmcp.NewLifecycleVisibilityService(config.DB, config.AuditLogger, mcpservers.LockMCPServerVisibilityDependencies, func(ctx context.Context, tx pgx.Tx, existing mcpserversrepo.McpServer, input platformmcp.LifecycleVisibilityUpdate) (platformmcp.LifecycleVisibilityUpdateResult, error) {
		updated, err := mcpservers.UpdateMCPServerVisibilityInTransaction(ctx, tx, config.AuditLogger, existing, mcpservers.LifecycleUpdateInput{
			OrganizationID:        input.OrganizationID,
			ProjectID:             input.ProjectID,
			ActorUserID:           input.ActorUserID,
			ActorEmail:            nil,
			ServerID:              input.ServerID,
			Name:                  nil,
			Visibility:            input.Visibility,
			EnvironmentID:         existing.EnvironmentID,
			UserSessionIssuerID:   existing.UserSessionIssuerID,
			RemoteMcpServerID:     existing.RemoteMcpServerID,
			TunneledMcpServerID:   existing.TunneledMcpServerID,
			ToolsetID:             existing.ToolsetID,
			UnproxiedMcpServerID:  existing.UnproxiedMcpServerID,
			ToolVariationsGroupID: existing.ToolVariationsGroupID,
		})
		if err != nil {
			return platformmcp.LifecycleVisibilityUpdateResult{}, err
		}
		return platformmcp.LifecycleVisibilityUpdateResult{Server: updated.Server, ClearedRootDomainIDs: updated.ClearedRootDomainIDs}, nil
	}, func(ctx context.Context, projectID uuid.UUID, userID, commitMessage string) error {
		if config.PluginPublisher == nil {
			return fmt.Errorf("plugin publishing is not configured")
		}
		_, err := config.PluginPublisher.PublishProject(ctx, plugins.PublishProjectInput{ProjectID: projectID, CreatedByUserID: userID, CommitMessage: commitMessage, SkipIfUnchanged: true})
		return err
	}, func(ctx context.Context, domainIDs []uuid.UUID) error {
		var result []error
		for _, domainID := range domainIDs {
			if _, err := (&background.CustomDomainRegistrationClient{TemporalEnv: config.TemporalEnv}).ExecuteCustomDomainReconcile(ctx, domainID); err != nil {
				result = append(result, err)
			}
		}
		return errors.Join(result...)
	}, readiness, config.JWTSigningKey, config.DistributionAdmission, platformmcp.NewPostgresOrganizationSlugResolver(config.DB))
}

func newPlatformMCPDistributionService(config platformMCPConfig, pluginTargets platformmcp.PluginTargetResolver) *platformmcp.DistributionService {
	return platformmcp.NewDistributionService(
		config.DB,
		config.AuditLogger,
		func(ctx context.Context, tx pgx.Tx, authCtx *contextvalues.AuthContext, organizationID string, projectID, pluginID, mcpServerID uuid.UUID, displayName string) (uuid.UUID, bool, error) {
			attached, err := plugins.AttachToExistingPluginAudited(ctx, tx, config.AuditLogger, authCtx, organizationID, projectID, pluginID, mcpServerID, displayName)
			if err != nil {
				return uuid.Nil, false, err
			}
			if attached == nil {
				return uuid.Nil, false, nil
			}
			return attached.Server.ID, true, nil
		},
		func(ctx context.Context, projectID uuid.UUID, userID, commitMessage string) error {
			if config.PluginPublisher == nil {
				return fmt.Errorf("plugin publishing is not configured")
			}
			_, err := config.PluginPublisher.PublishProject(ctx, plugins.PublishProjectInput{ProjectID: projectID, CreatedByUserID: userID, CommitMessage: commitMessage, SkipIfUnchanged: true})
			return err
		},
		pluginTargets,
		config.DistributionAdmission,
		platformmcp.NewPostgresOrganizationSlugResolver(config.DB),
	)
}

// newPlatformMCPToolExposure composes the reads and the incremental write that
// decide which tools a hosted MCP server exposes.
func newPlatformMCPToolExposure(config platformMCPConfig, authorizer platformmcp.Authorizer, limitStore ratelimit.Store) *platformmcp.MCPToolExposureService {
	return platformmcp.NewMCPToolExposureService(
		config.Logger, config.DB, config.AuditLogger, config.Authz, authorizer, config.JWTSigningKey,
		config.PublicationRequests, config.PluginPublishSignaler,
		// The catalogue read is metered well above the write: an administrator
		// narrowing down one tool legitimately pages through it, while each
		// write locks the toolset and republishes every plugin carrying the
		// server. Separate allowances so neither can fund the other.
		platformmcp.OperationBudget{
			Connection:   ratelimit.New(limitStore, platformmcp.ToolExposureReadConnectionLimitName, ratelimit.PerMinute(platformmcp.ToolExposureReadsPerConnectionPerMinute), ratelimit.WithMetrics(config.MeterProvider)),
			Organization: ratelimit.New(limitStore, platformmcp.ToolExposureReadOrganizationLimitName, ratelimit.PerMinute(platformmcp.ToolExposureReadsPerOrganizationPerMinute), ratelimit.WithMetrics(config.MeterProvider)),
		},
		platformmcp.OperationBudget{
			Connection:   ratelimit.New(limitStore, platformmcp.ToolExposureMutationConnectionLimitName, ratelimit.PerMinute(platformmcp.ToolExposureMutationsPerConnectionPerMinute), ratelimit.WithMetrics(config.MeterProvider)),
			Organization: ratelimit.New(limitStore, platformmcp.ToolExposureMutationOrganizationLimitName, ratelimit.PerMinute(platformmcp.ToolExposureMutationsPerOrganizationPerMinute), ratelimit.WithMetrics(config.MeterProvider)),
		},
		// A new toolset version has no search index, and dynamic-mode tools/list
		// refuses to serve a version without one, so a committed change must
		// schedule the rebuild the dashboard's own toolset update schedules.
		// Without it the periodic sweep is the only recovery and an otherwise
		// working server cannot list its tools in the meantime.
		func(ctx context.Context, projectID, toolsetID uuid.UUID) error {
			return toolsets.TriggerToolsetIndexForVersion(ctx, config.Logger, config.DB, config.TemporalEnv, projectID, toolsetID)
		},
	)
}

func newPlatformMCPConnectionMutations(config platformMCPConfig) *platformmcp.MCPConnectionMutationService {
	return platformmcp.NewMCPConnectionMutationService(
		config.DB,
		platformmcp.NewMCPConnectionSettingsService(config.DB),
		mcpendpoints.NewService(config.Logger, config.TracerProvider, config.DB, config.Sessions, config.Authz, config.AuditLogger, config.TemporalEnv, config.PluginPublisher != nil, config.DistributionAdmission),
		config.AuditLogger, config.Authz, config.NetworkAccessAdmission, config.PublicationRequests, config.PluginPublishSignaler,
	)
}

func loadBrowserPlatformMCPCatalogDescriptors(ctx context.Context, catalog *externalmcp.CatalogService) ([]platformmcp.RegistryCatalogSource, error) {
	sources, err := catalog.Sources(ctx)
	if err != nil {
		return nil, fmt.Errorf("list reviewed MCP catalogue sources for Platform MCP: %w", err)
	}
	result := make([]platformmcp.RegistryCatalogSource, 0, len(sources))
	for _, source := range sources {
		reader, err := catalog.ReaderFor(source)
		if err != nil {
			return nil, fmt.Errorf("resolve reviewed MCP catalogue source %q: %w", source.SourceKey, err)
		}
		// Keep the source sequence from CatalogService, which is ordered by
		// operator-defined priority and source key; grouping through a map would
		// discard that deterministic composition order.
		result = append(result, platformmcp.RegistryCatalogSource{
			Client:      reader,
			Descriptors: []platformmcp.CatalogDescriptor{platformmcp.BrowserCatalogDescriptor(source.Registry)},
		})
	}
	return result, nil
}

func configureBrowserPlatformMCP(ctx context.Context, config platformMCPConfig) (AssistantSurface, error) {
	gate := platformmcp.NewOrganizationGate(config.ProductFeatures)
	authorizer := platformmcp.NewLiveOrgAdminAuthorizer(config.DB, config.Authz).WithDashboardURL(config.DashboardURL).WithServerURL(config.ServerURL)
	oauthTelemetry := platformmcp.NewOAuthTelemetry(config.Logger, config.MeterProvider)
	oauthStore := platformmcp.NewPostgresOAuthStore(config.DB).WithTelemetry(oauthTelemetry)
	oauth := platformmcp.NewOAuthHTTP(platformmcp.OAuthHTTPConfig{
		BaseURL:       config.ServerURL,
		Environment:   config.Environment,
		Cache:         cache.NewRedisCacheAdapter(config.Redis),
		Store:         oauthStore,
		Identity:      config.Identity,
		Gate:          gate,
		Authorizer:    authorizer,
		Organizations: platformmcp.NewLiveOrganizationSelector(config.DB, authorizer),
		Signer:        sessiontokens.NewSigner(config.JWTSigningKey),
		Encryption:    config.Encryption,
		Telemetry:     oauthTelemetry,
		Logger:        config.Logger,
		// Backs the inbound CIMD document fetcher's SSRF protection.
		GuardianPolicy:     config.GuardianPolicy,
		MeterProvider:      config.MeterProvider,
		IDPCallbackBaseURL: config.OutboundCallbackOrigin,
	})
	authenticator := platformmcp.NewJWTAuthenticator(sessiontokens.NewSigner(config.JWTSigningKey), config.DB, config.Encryption, config.ServerURL)

	catalog := platformmcp.NewDynamicRegistryCatalogSources(func(ctx context.Context) ([]platformmcp.RegistryCatalogSource, error) {
		return loadBrowserPlatformMCPCatalogDescriptors(ctx, config.Catalog)
	}).WithIdentityService(config.Catalog)
	store := platformmcp.NewRegistrationStore(config.DB)
	registrationGate := platformmcp.NewCatalogRegistrationGate(gate)
	adapters := platformmcp.NewProviderAdapters(nil)
	limitStore := ratelimit.NewRedisStore(config.Redis)
	newBudget := func(connectionName, organizationName string) platformmcp.OperationBudget {
		return platformmcp.OperationBudget{
			Connection:   ratelimit.New(limitStore, connectionName, ratelimit.PerMinute(5), ratelimit.WithMetrics(config.MeterProvider)),
			Organization: ratelimit.New(limitStore, organizationName, ratelimit.PerMinute(50), ratelimit.WithMetrics(config.MeterProvider)),
		}
	}
	budgets := platformmcp.OperationBudgets{
		Catalog:      newBudget(platformmcp.CatalogConnectionLimitName, platformmcp.CatalogOrganizationLimitName),
		Registration: newBudget(platformmcp.RegistrationConnectionLimitName, platformmcp.RegistrationOrganizationLimitName),
		ReviewRequests: platformmcp.OperationBudget{
			Connection:   ratelimit.New(limitStore, platformmcp.ReviewRequestConnectionLimitName, ratelimit.PerMinute(platformmcp.ReviewRequestsPerConnectionPerMinute), ratelimit.WithMetrics(config.MeterProvider)),
			Organization: ratelimit.New(limitStore, platformmcp.ReviewRequestOrganizationLimitName, ratelimit.PerMinute(platformmcp.ReviewRequestsPerOrganizationPerMinute), ratelimit.WithMetrics(config.MeterProvider)),
		},
		Handoff:    newBudget(platformmcp.HandoffConnectionLimitName, platformmcp.HandoffOrganizationLimitName),
		SetupStart: newBudget(platformmcp.SetupConnectionLimitName, platformmcp.SetupOrganizationLimitName),
		Repair:     newBudget(platformmcp.RepairConnectionLimitName, platformmcp.RepairOrganizationLimitName),
		// Documentation search is metered on its own allowances rather than the
		// shared five-per-minute budget: retrieval is in-process and reading is
		// what the corpus is for, so a caller researching a setup should not
		// spend the budget that its registration call needs.
		Docs: platformmcp.OperationBudget{
			Connection:   ratelimit.New(limitStore, platformmcp.DocsConnectionLimitName, ratelimit.PerMinute(platformmcp.DocsQueriesPerConnectionPerMinute), ratelimit.WithMetrics(config.MeterProvider)),
			Organization: ratelimit.New(limitStore, platformmcp.DocsOrganizationLimitName, ratelimit.PerMinute(platformmcp.DocsQueriesPerOrganizationPerMinute), ratelimit.WithMetrics(config.MeterProvider)),
		},
		Skills: platformmcp.OperationBudget{
			Connection:   ratelimit.New(limitStore, platformmcp.SkillsConnectionLimitName, ratelimit.PerMinute(platformmcp.SkillsOperationsPerConnectionPerMinute), ratelimit.WithMetrics(config.MeterProvider)),
			Organization: ratelimit.New(limitStore, platformmcp.SkillsOrganizationLimitName, ratelimit.PerMinute(platformmcp.SkillsOperationsPerOrganizationPerMinute), ratelimit.WithMetrics(config.MeterProvider)),
		},
		LifecycleMetadata: platformmcp.OperationBudget{
			Connection:   ratelimit.New(limitStore, platformmcp.LifecycleConnectionLimitName, ratelimit.PerMinute(platformmcp.LifecycleOperationsPerConnectionPerMinute), ratelimit.WithMetrics(config.MeterProvider)),
			Organization: ratelimit.New(limitStore, platformmcp.LifecycleOrganizationLimitName, ratelimit.PerMinute(platformmcp.LifecycleOperationsPerOrganizationPerMinute), ratelimit.WithMetrics(config.MeterProvider)),
		},
		RiskFindings: platformmcp.OperationBudget{
			Connection:   ratelimit.New(limitStore, platformmcp.RiskFindingsConnectionLimitName, ratelimit.PerMinute(platformmcp.RiskFindingsQueriesPerConnectionPerMinute), ratelimit.WithMetrics(config.MeterProvider)),
			Organization: ratelimit.New(limitStore, platformmcp.RiskFindingsOrganizationLimitName, ratelimit.PerMinute(platformmcp.RiskFindingsQueriesPerOrganizationPerMinute), ratelimit.WithMetrics(config.MeterProvider)),
		},
		RiskMutations: platformmcp.OperationBudget{
			Connection:   ratelimit.New(limitStore, platformmcp.RiskMutationConnectionLimitName, ratelimit.PerMinute(platformmcp.RiskMutationsPerConnectionPerMinute), ratelimit.WithMetrics(config.MeterProvider)),
			Organization: ratelimit.New(limitStore, platformmcp.RiskMutationOrganizationLimitName, ratelimit.PerMinute(platformmcp.RiskMutationsPerOrganizationPerMinute), ratelimit.WithMetrics(config.MeterProvider)),
		},

		// Diagnostics are read-only aggregate queries an administrator runs
		// while investigating, so they are metered well above the shared
		// five-per-minute mutation budget.
		Diagnostics: platformmcp.OperationBudget{
			Connection:   ratelimit.New(limitStore, platformmcp.DiagnosticsConnectionLimitName, ratelimit.PerMinute(platformmcp.DiagnosticQueriesPerConnectionPerMinute), ratelimit.WithMetrics(config.MeterProvider)),
			Organization: ratelimit.New(limitStore, platformmcp.DiagnosticsOrganizationLimitName, ratelimit.PerMinute(platformmcp.DiagnosticQueriesPerOrganizationPerMinute), ratelimit.WithMetrics(config.MeterProvider)),
		},
		Plugins: platformmcp.OperationBudget{
			Connection:   ratelimit.New(limitStore, platformmcp.PluginsConnectionLimitName, ratelimit.PerMinute(platformmcp.PluginQueriesPerConnectionPerMinute), ratelimit.WithMetrics(config.MeterProvider)),
			Organization: ratelimit.New(limitStore, platformmcp.PluginsOrganizationLimitName, ratelimit.PerMinute(platformmcp.PluginQueriesPerOrganizationPerMinute), ratelimit.WithMetrics(config.MeterProvider)),
		},
		AccessReads: platformmcp.OperationBudget{
			Connection:   ratelimit.New(limitStore, platformmcp.AccessReadsConnectionLimitName, ratelimit.PerMinute(platformmcp.AccessReadQueriesPerConnectionPerMinute), ratelimit.WithMetrics(config.MeterProvider)),
			Organization: ratelimit.New(limitStore, platformmcp.AccessReadsOrganizationLimitName, ratelimit.PerMinute(platformmcp.AccessReadQueriesPerOrganizationPerMinute), ratelimit.WithMetrics(config.MeterProvider)),
		},
		AccessRoleMutations: platformmcp.OperationBudget{
			Connection:   ratelimit.New(limitStore, platformmcp.AccessRoleMutationConnectionLimitName, ratelimit.PerMinute(platformmcp.AccessRoleMutationsPerConnectionPerMinute), ratelimit.WithMetrics(config.MeterProvider)),
			Organization: ratelimit.New(limitStore, platformmcp.AccessRoleMutationOrganizationLimitName, ratelimit.PerMinute(platformmcp.AccessRoleMutationsPerOrganizationPerMinute), ratelimit.WithMetrics(config.MeterProvider)),
		},
		// Metered separately and lower: personal-data reads (this and session
		// recall below, each on its own budget) must not be fundable by
		// spending the ordinary diagnostic allowance.
		SensitiveDiagnostics: platformmcp.OperationBudget{
			Connection:   ratelimit.New(limitStore, platformmcp.SensitiveDiagnosticsConnectionLimitName, ratelimit.PerMinute(platformmcp.SensitiveDiagnosticQueriesPerConnectionPerMinute), ratelimit.WithMetrics(config.MeterProvider)),
			Organization: ratelimit.New(limitStore, platformmcp.SensitiveDiagnosticsOrganizationLimitName, ratelimit.PerMinute(platformmcp.SensitiveDiagnosticQueriesPerOrganizationPerMinute), ratelimit.WithMetrics(config.MeterProvider)),
		},
		// Session recall serves whole-transcript digests, so it is metered on
		// its own low allowance that no other budget can fund.
		SensitiveSessionRecall: platformmcp.OperationBudget{
			Connection:   ratelimit.New(limitStore, platformmcp.SessionRecallConnectionLimitName, ratelimit.PerMinute(platformmcp.SessionRecallsPerConnectionPerMinute), ratelimit.WithMetrics(config.MeterProvider)),
			Organization: ratelimit.New(limitStore, platformmcp.SessionRecallOrganizationLimitName, ratelimit.PerMinute(platformmcp.SessionRecallsPerOrganizationPerMinute), ratelimit.WithMetrics(config.MeterProvider)),
		},
		// The second drill-down cap: what a connection may accumulate over ten
		// minutes, rather than how often it may call. Both buckets refill over
		// that window, so a caller paging steadily under the per-minute rate
		// still runs out of rows before it has walked a whole window.
		DrilldownVolume: platformmcp.DrilldownVolumeBudget{
			Rows: ratelimit.New(limitStore, platformmcp.DrilldownRowsLimitName, ratelimit.Rate{
				Tokens:   platformmcp.DrilldownRowsPerConnectionPerWindow,
				Interval: platformmcp.DrilldownVolumeWindow,
				Burst:    platformmcp.DrilldownRowsPerConnectionPerWindow,
			}, ratelimit.WithMetrics(config.MeterProvider)),
			MetricQueries: ratelimit.New(limitStore, platformmcp.DrilldownMetricQueriesLimitName, ratelimit.Rate{
				Tokens:   platformmcp.DrilldownMetricQueriesPerConnectionPerWindow,
				Interval: platformmcp.DrilldownVolumeWindow,
				Burst:    platformmcp.DrilldownMetricQueriesPerConnectionPerWindow,
			}, ratelimit.WithMetrics(config.MeterProvider)),
		},
	}
	telemetry := platformmcp.NewLifecycleTelemetry(config.Logger, config.MeterProvider)
	riskTelemetry := platformmcp.NewRiskTelemetry(config.Logger, config.MeterProvider)
	readiness := platformmcp.NewReadinessService(
		store,
		registrationGate,
		adapters,
		ratelimit.New(limitStore, platformmcp.ForcedReadinessProbeLimit, ratelimit.PerMinute(platformmcp.ForcedReadinessProbesPerMinute), ratelimit.WithMetrics(config.MeterProvider)),
		budgets.Repair,
		platformmcp.NewRemoteMCPReadinessProber(config.Logger, config.DB, config.Encryption, config.GuardianPolicy, config.RemoteChallengeManager),
	).WithTelemetry(telemetry)
	lifecycleMetadata := newPlatformMCPLifecycleMetadataService(config)
	lifecycleVisibility := newPlatformMCPLifecycleVisibilityService(config, readiness)
	registrations := platformmcp.NewRegistrationService(catalog, registrationGate, store).
		WithDirectRemoteInspector(platformmcp.NewGuardianDirectRemoteInspector(config.GuardianPolicy)).
		WithLifecycleMetadata(lifecycleMetadata).
		WithLifecycleVisibility(lifecycleVisibility).
		WithOperationBudgets(budgets).
		WithReadiness(readiness).
		WithDashboardURL(config.DashboardURL).
		WithIdentityProviderAttachment(platformmcp.NewCatalogIdentityProviderAttachmentService(config.Logger, config.MeterProvider, config.DB, config.IdentityCommitter, config.GuardianPolicy, config.ServerURL)).
		WithClientAdmission(platformmcp.NewClientAdmissionService(config.DB, config.AuditLogger)).
		WithTelemetry(telemetry)
	dashboardSetupStarter := platformmcp.NewDashboardSetupService(store, registrationGate, authorizer, adapters, budgets.SetupStart)
	feedback := platformmcp.NewFeedbackService(config.DB)
	workflowRun := platformmcp.NewWorkflowRunService(config.Logger, config.WorkflowRun)
	setupResources, err := platformMCPSetupResources(config)
	if err != nil {
		return AssistantSurface{}, err
	}
	pluginAssignmentMutationBudget := platformmcp.OperationBudget{
		Connection:   ratelimit.New(limitStore, platformmcp.PluginAssignmentMutationConnectionLimitName, ratelimit.PerMinute(platformmcp.PluginAssignmentMutationsPerConnectionPerMinute), ratelimit.WithMetrics(config.MeterProvider)),
		Organization: ratelimit.New(limitStore, platformmcp.PluginAssignmentMutationOrganizationLimitName, ratelimit.PerMinute(platformmcp.PluginAssignmentMutationsPerOrganizationPerMinute), ratelimit.WithMetrics(config.MeterProvider)),
	}
	organizationSlugs := platformmcp.NewPostgresOrganizationSlugResolver(config.DB)
	distributionAdmissionReads := platformmcp.NewShadowDistributionReadService(config.Logger, config.DB, config.DistributionAdmission, organizationSlugs)
	pluginInventory := platformmcp.NewPluginsService(config.DB, budgets.Plugins, config.JWTSigningKey, config.DistributionAdmission).
		WithAuthorization(config.Authz).
		WithRemoteSessions(config.RemoteChallengeManager).
		WithInstallLinks(config.DashboardURL, config.ServerURL).
		WithAssignmentMutations(config.FeatureFlags, organizationSlugs, config.AuditLogger, pluginAssignmentMutationBudget).
		WithDistributionAdmissionReads(distributionAdmissionReads)
	if config.PluginPublisher != nil {
		pluginInventory.WithPublicationEvidence(config.PluginPublisher)
	}
	pluginInventory.WithRepublish(config.PublicationRequests, config.PluginPublishSignaler, platformmcp.OperationBudget{
		Connection:   ratelimit.New(limitStore, platformmcp.PluginRepublishConnectionLimitName, ratelimit.PerMinute(platformmcp.PluginRepublishesPerConnectionPerMinute), ratelimit.WithMetrics(config.MeterProvider)),
		Organization: ratelimit.New(limitStore, platformmcp.PluginRepublishOrganizationLimitName, ratelimit.PerMinute(platformmcp.PluginRepublishesPerOrganizationPerMinute), ratelimit.WithMetrics(config.MeterProvider)),
	}).WithPublishStatus(&background.TemporalPluginPublisher{TemporalEnv: config.TemporalEnv})
	roleManager := access.NewRoleManager(config.Logger, config.DB, config.AccessRoles, config.AuditLogger)
	accessReads := platformmcp.NewAccessReadService(config.Logger, config.DB, roleManager, budgets.AccessReads, config.JWTSigningKey)
	accessRoleMutations := platformmcp.NewAccessRoleMutationService(accessReads, config.FeatureFlags, budgets.AccessRoleMutations, config.JWTSigningKey, roleManager)
	distributions := newPlatformMCPDistributionService(config, pluginInventory)
	skillAuthoring := platformmcp.NewSkillsService(config.Skills, platformmcp.NewPostgresSkillTargets(config.DB), store, config.Authz, registrationGate, budgets.Skills).
		WithInsights(config.SkillInsights, budgets.Diagnostics)
	platformReader := platformmcp.NewPostgresReader(config.Logger, config.DB).
		WithXAAReadiness(oktaresourceconnections.NewService(config.Logger, config.TracerProvider, config.DB, config.Sessions, config.Authz, config.AuditLogger, config.FeatureFlags), config.FeatureFlags).
		WithAuthorization(config.Authz).
		WithReviewRequests(config.ShadowReview, budgets.ReviewRequests).
		WithDataExports(config.Encryption, config.DashboardURL).
		WithDataExportMutations(config.AuditLogger, config.DashboardURL).
		WithRecentToolCalls(config.RecentToolCalls, config.DashboardURL).
		WithMCPNetworkTraffic(config.NetworkTraffic, config.LogsEnabled).
		WithOrganizationEvents(config.EventFeed, config.LogsEnabled, config.DashboardURL).
		WithNetworkIngressStatus(config.DashboardURL).
		WithRiskAnalysisStatus(platformmcp.NewRiskAnalysisStatusService(config.Logger, config.DB, config.RiskAnalysisDescriber, config.FeatureFlags, organizationSlugs)).
		WithRiskFindings(platformmcp.NewRiskFindingsService(config.DB, config.RiskFindings, config.FeatureFlags, organizationSlugs, config.JWTSigningKey), budgets.RiskFindings).
		WithRiskFindingList(platformmcp.NewRiskFindingListService(config.DB, config.RiskFindingList, config.JWTSigningKey), budgets.RiskFindings).
		// Metered on the sensitive allowance: a chat page carries masked
		// participants and person references, like the drill-down reads.
		WithChatMetadata(platformmcp.NewChatMetadataService(config.DB, budgets.SensitiveDiagnostics, config.JWTSigningKey)).
		WithToolExposure(newPlatformMCPToolExposure(config, authorizer, limitStore))
	attachShadow(platformReader, config, authorizer, limitStore, pluginInventory, distributionAdmissionReads, organizationSlugs, budgets.SensitiveDiagnostics)
	diagnostics := platformmcp.NewDiagnosticsService(config.DB, config.Telemetry, config.SessionCapture, platformReader, readiness, budgets.Diagnostics, config.CanonicalIdentity).
		WithToolUsageBreakdown(config.ToolUsage).
		WithDrilldown(config.TelemetryDrilldown, config.JWTSigningKey, budgets.SensitiveDiagnostics, budgets.DrilldownVolume, platformmcp.NewPostgresDrilldownAuditor(config.DB)).
		WithUserSearch(config.UserSearch).
		WithToolCallSearch(config.ToolCallSearch)
	sessionRecall := platformmcp.NewSessionRecallService(config.Logger, config.DB, platformrepo.New(config.DB), audit.NewLogger(), config.SessionPortability, budgets.SensitiveSessionRecall)
	riskMutations := platformmcp.NewRiskMutationHandlers(
		config.DB,
		platformmcp.NewRiskMutationControls(config.DB, config.FeatureFlags, platformmcp.NewPostgresOrganizationSlugResolver(config.DB), budgets.RiskMutations, config.JWTSigningKey),
		risk.NewPolicyMutationCore(config.DB, config.AuditLogger, config.RiskPolicyApprovals, config.RiskPolicySignaler, config.RiskPolicyCache),
		risk.NewExclusionMutationCore(config.Logger, config.DB, config.AuditLogger, config.RiskExclusionReconciler, config.JWTSigningKey),
		risk.NewFalsePositiveCore(config.AuditLogger),
		config.RiskPolicyCatalog,
	)
	runtime := platformmcp.NewRuntime(
		config.Logger,
		authenticator,
		gate,
		authorizer,
		oauth.ProtectedResourceURL(),
		config.JWTSigningKey,
		config.RiskPolicyCatalog,
		platformmcp.NewPostgresReadinessRecorder(config.DB),
		platformmcp.Services{
			Reader:              platformReader,
			Catalog:             catalog,
			Registrations:       registrations,
			SetupResources:      setupResources,
			Feedback:            feedback,
			Onboarding:          platformmcp.NewOnboardingService(config.DB),
			Distributions:       distributions,
			Skills:              skillAuthoring,
			Diagnostics:         diagnostics,
			WorkflowRun:         workflowRun,
			Plugins:             pluginInventory,
			SessionRecall:       sessionRecall,
			RiskMutations:       riskMutations,
			Candidate:           platformmcp.CatalogDescriptor{},
			AccessReads:         accessReads,
			AccessRoleMutations: accessRoleMutations,
			ConnectionMutations: newPlatformMCPConnectionMutations(config),
		},
	)
	runtime = runtime.WithOAuthTelemetry(oauthTelemetry).WithRiskTelemetry(riskTelemetry)
	oauth.Attach(config.Mux)
	platformmcp.NewDashboardSetupHTTP(dashboardSetupStarter, config.Sessions).Attach(config.Mux)
	platformmcp.AttachManagement(config.Mux, platformmcp.NewManagementService(config.Logger, config.TracerProvider, config.DB, config.Sessions, config.Authz, gate, authorizer, config.ServerURL.JoinPath("platform-mcp").String(), registrations, readiness, distributions, config.JWTSigningKey, catalog))
	o11y.AttachHandler(config.Mux, http.MethodPost, platformmcp.Path, runtime.Handler().ServeHTTP)
	return AssistantSurface{Tools: runtime.AssistantTools(), Authorizer: authorizer}, nil
}
