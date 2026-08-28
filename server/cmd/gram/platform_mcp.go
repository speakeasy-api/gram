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
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/o11y"
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
	"github.com/speakeasy-api/gram/server/internal/risk/policycore"
	"github.com/speakeasy-api/gram/server/internal/sessiontokens"
	tenv "github.com/speakeasy-api/gram/server/internal/temporal"
)

type platformMCPConfig struct {
	Logger                  *slog.Logger
	MeterProvider           metric.MeterProvider
	TracerProvider          trace.TracerProvider
	Mux                     goahttp.Muxer
	DB                      *pgxpool.Pool
	Redis                   *redis.Client
	ServerURL               *url.URL
	DashboardURL            *url.URL
	Environment             string
	JWTSigningKey           string
	ProductFeatures         *productfeatures.Client
	FeatureFlags            feature.Provider
	Authz                   *authz.Engine
	Encryption              *encryption.Client
	Identity                *identity.Resolver
	Sessions                *sessions.Manager
	Registry                *externalmcp.RegistryClient
	Catalog                 *externalmcp.CatalogService
	GuardianPolicy          *guardian.Policy
	RemoteChallengeManager  *remotesessions.ChallengeManager
	AuditLogger             *audit.Logger
	PluginPublisher         *plugins.Service
	TemporalEnv             *tenv.Environment
	Skills                  platformmcp.SkillsManagement
	RiskPolicyApprovals     policycore.ApprovalCoordinator
	RiskPolicySignaler      policycore.PolicySignaler
	RiskPolicyCache         policycore.PolicyCacheInvalidator
	RiskExclusionReconciler risk.RiskExclusionReconciler
	// Telemetry is the Gram-owned ClickHouse read model the diagnostics tools
	// answer from. Nil disables them rather than serving an empty answer, which
	// a caller would read as "nothing is wrong".
	Telemetry platformmcp.DiagnosticsTelemetryReader
	// SessionCapture resolves the organization's metrics mode, which decides
	// what a project overview's active-user count measures. Shared with the
	// telemetry service so both surfaces answer from the same source.
	SessionCapture platformmcp.FeatureChecker
	// SessionPortability gates the session-recall tools (list_my_sessions /
	// continue_session). Sibling of SessionCapture: capture records sessions,
	// portability serves them back as redacted handoff digests.
	SessionPortability platformmcp.FeatureChecker
	// TelemetryDrilldown is the row-level half of the same read model. Nil
	// withholds the drill-down tools while leaving the overview-first entry
	// points serving.
	TelemetryDrilldown platformmcp.DrilldownTelemetryReader
	LocalFixture       *platformMCPLocalFixtureConfig
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
	if config.Registry == nil || fixtureConfig == nil {
		return AssistantSurface{}, errors.New("local Platform MCP fixture configuration is incomplete")
	}
	if err := config.Registry.ClearCache(ctx, fixtureConfig.Registry().URL); err != nil {
		return AssistantSurface{}, fmt.Errorf("clear local Platform MCP fixture registry cache: %w", err)
	}

	gate := platformmcp.NewOrganizationGate(config.ProductFeatures)
	authorizer := platformmcp.NewLiveOrgAdminAuthorizer(config.DB, config.Authz)
	oauthTelemetry := platformmcp.NewOAuthTelemetry(config.Logger, config.MeterProvider)
	oauthStore := platformmcp.NewPostgresOAuthStore(config.DB).WithTelemetry(oauthTelemetry)
	oauth, err := platformmcp.NewOAuthHTTP(platformmcp.OAuthHTTPConfig{
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
		// Backs the inbound CIMD document fetcher's SSRF protection; without
		// it the authorization server serves DCR only.
		GuardianPolicy: config.GuardianPolicy,
		MeterProvider:  config.MeterProvider,
	})
	if err != nil {
		return AssistantSurface{}, fmt.Errorf("create local Platform MCP OAuth service: %w", err)
	}
	authenticator, err := platformmcp.NewJWTAuthenticator(sessiontokens.NewSigner(config.JWTSigningKey), config.DB, config.Encryption, oauth.Issuer(), oauth.Audience())
	if err != nil {
		return AssistantSurface{}, fmt.Errorf("create local Platform MCP authenticator: %w", err)
	}

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
	store, err := platformmcp.NewRegistrationStore(config.DB, platformmcp.RegistrationStoreConfig{ActiveRegistrationCap: 5})
	if err != nil {
		return AssistantSurface{}, fmt.Errorf("create local Platform MCP registration store: %w", err)
	}
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
		Handoff:      newBudget(platformmcp.HandoffConnectionLimitName, platformmcp.HandoffOrganizationLimitName),
		SetupStart:   newBudget(platformmcp.SetupConnectionLimitName, platformmcp.SetupOrganizationLimitName),
		Repair:       newBudget(platformmcp.RepairConnectionLimitName, platformmcp.RepairOrganizationLimitName),
		// Documentation search is metered on its own allowances rather than the
		// shared five-per-minute budget: retrieval is in-process and reading is
		// what the corpus is for, so a caller researching a setup should not
		// spend the budget that its registration call needs.
		Docs: platformmcp.OperationBudget{
			Connection:   ratelimit.New(limitStore, platformmcp.DocsConnectionLimitName, ratelimit.PerMinute(platformmcp.DocsQueriesPerConnectionPerMinute), ratelimit.WithMetrics(config.MeterProvider)),
			Organization: ratelimit.New(limitStore, platformmcp.DocsOrganizationLimitName, ratelimit.PerMinute(platformmcp.DocsQueriesPerOrganizationPerMinute), ratelimit.WithMetrics(config.MeterProvider)),
		},
		Skills:            newBudget(platformmcp.SkillsConnectionLimitName, platformmcp.SkillsOrganizationLimitName),
		LifecycleMetadata: newBudget(platformmcp.LifecycleConnectionLimitName, platformmcp.LifecycleOrganizationLimitName),
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
	if !budgets.Valid() {
		return AssistantSurface{}, errors.New("local Platform MCP operation budgets are incomplete")
	}
	telemetry := platformmcp.NewLifecycleTelemetry(config.Logger, config.MeterProvider)
	readiness := platformmcp.NewReadinessService(
		store,
		registrationGate,
		adapters,
		ratelimit.New(limitStore, platformmcp.ForcedReadinessProbeLimit, ratelimit.PerMinute(platformmcp.ForcedReadinessProbesPerMinute), ratelimit.WithMetrics(config.MeterProvider)),
		budgets.Repair,
		platformmcp.NewRemoteMCPReadinessProber(config.Logger, config.DB, config.Encryption, config.GuardianPolicy, config.RemoteChallengeManager),
	).WithTelemetry(telemetry)
	lifecycleMetadata, err := newPlatformMCPLifecycleMetadataService(config)
	if err != nil {
		return AssistantSurface{}, fmt.Errorf("create Platform MCP lifecycle metadata service: %w", err)
	}
	lifecycleVisibility, err := newPlatformMCPLifecycleVisibilityService(config, readiness)
	if err != nil {
		return AssistantSurface{}, fmt.Errorf("create Platform MCP lifecycle visibility service: %w", err)
	}
	registrations := platformmcp.NewRegistrationService(catalog, registrationGate, store).
		WithDirectRemoteInspector(platformmcp.NewGuardianDirectRemoteInspector(config.GuardianPolicy)).
		WithLifecycleMetadata(lifecycleMetadata).
		WithLifecycleVisibility(lifecycleVisibility).
		WithOperationBudgets(budgets).
		WithReadiness(readiness).
		WithDashboardURL(config.DashboardURL).
		WithIdentityProviderAttachment(platformmcp.NewCatalogIdentityProviderAttachmentService(config.DB, config.Encryption, config.GuardianPolicy, config.AuditLogger, config.ServerURL)).
		WithClientAdmission(platformmcp.NewClientAdmissionService(config.DB, config.AuditLogger)).
		WithTelemetry(telemetry)
	dashboardSetupStarter := platformmcp.NewDashboardSetupService(store, registrationGate, authorizer, adapters, budgets.SetupStart)
	feedback := platformmcp.NewFeedbackService(config.DB)
	setupResources, err := platformMCPSetupResources(config)
	if err != nil {
		return AssistantSurface{}, err
	}
	pluginInventory := platformmcp.NewPluginsService(config.DB, budgets.Plugins, config.JWTSigningKey)
	distributions := newPlatformMCPDistributionService(config, pluginInventory)

	registryHandler := localfixture.NewRegistryHTTP(fixtureConfig).Handler()
	config.Mux.Handle(http.MethodGet, "/v0.1/servers", registryHandler.ServeHTTP)
	config.Mux.Handle(http.MethodGet, fixtureConfig.RegistryDetailsPath(), registryHandler.ServeHTTP)
	config.Mux.Handle(http.MethodGet, "/.well-known/oauth-authorization-server/platform-mcp/local-fixture", fixtureOAuth.Handler().ServeHTTP)
	config.Mux.Handle(http.MethodGet, "/platform-mcp/local-fixture/authorize", fixtureOAuth.Handler().ServeHTTP)
	config.Mux.Handle(http.MethodPost, "/platform-mcp/local-fixture/register", fixtureOAuth.Handler().ServeHTTP)
	config.Mux.Handle(http.MethodPost, "/platform-mcp/local-fixture/token", fixtureOAuth.Handler().ServeHTTP)
	config.Mux.Handle(http.MethodPost, "/platform-mcp/local-fixture/revoke", fixtureOAuth.Handler().ServeHTTP)
	config.Mux.Handle(http.MethodPost, "/platform-mcp/local-fixture/mcp", fixtureMCP.Handler().ServeHTTP)

	skillAuthoring := platformmcp.NewSkillsService(config.Skills, platformmcp.NewPostgresSkillTargets(config.DB), store, config.Authz, registrationGate, budgets.Skills)
	platformReader := platformmcp.NewPostgresReader(config.Logger, config.DB)
	diagnostics := platformmcp.NewDiagnosticsService(config.DB, config.Telemetry, config.SessionCapture, platformReader, readiness, budgets.Diagnostics).
		WithDrilldown(config.TelemetryDrilldown, config.JWTSigningKey, budgets.SensitiveDiagnostics, budgets.DrilldownVolume, platformmcp.NewPostgresDrilldownAuditor(config.DB))
	sessionRecall := platformmcp.NewSessionRecallService(config.Logger, config.DB, platformrepo.New(config.DB), audit.NewLogger(), config.SessionPortability, budgets.SensitiveSessionRecall)
	riskMutationControls, err := platformmcp.NewRiskMutationControls(config.DB, config.FeatureFlags, platformmcp.NewPostgresOrganizationSlugResolver(config.DB), budgets.RiskMutations, config.JWTSigningKey)
	if err != nil {
		return AssistantSurface{}, fmt.Errorf("create local Platform MCP risk mutation controls: %w", err)
	}
	riskMutations, err := platformmcp.NewRiskMutationHandlers(
		config.DB,
		riskMutationControls,
		risk.NewPolicyMutationCore(config.DB, config.AuditLogger, config.RiskPolicyApprovals, config.RiskPolicySignaler, config.RiskPolicyCache),
		risk.NewExclusionMutationCore(config.Logger, config.DB, config.AuditLogger, config.RiskExclusionReconciler, config.JWTSigningKey),
	)
	if err != nil {
		return AssistantSurface{}, fmt.Errorf("create local Platform MCP risk policy mutations: %w", err)
	}
	runtime := platformmcp.NewRuntimeWithRiskMutations(
		config.Logger,
		authenticator,
		gate,
		authorizer,
		oauth.ProtectedResourceURL(),
		config.JWTSigningKey,
		platformReader,
		catalog,
		registrations,
		platformmcp.NewPostgresReadinessRecorder(config.DB),
		// The fixture guide plus the reviewed corpus: local runs exercise the
		// synthetic provider, but they must see the same real guides production
		// serves or a corpus defect would only ever surface in production.
		append(fixtureConfig.SetupResources(), setupResources...),
		feedback,
		platformmcp.NewOnboardingService(config.DB),
		distributions,
		skillAuthoring,
		diagnostics,
		pluginInventory,
		sessionRecall,
		riskMutations,
		fixtureConfig.CatalogDescriptor(),
	).WithOAuthTelemetry(oauthTelemetry)
	oauth.Attach(config.Mux)
	platformmcp.NewDashboardSetupHTTP(dashboardSetupStarter, config.Sessions).Attach(config.Mux)
	platformmcp.AttachManagement(config.Mux, platformmcp.NewManagementService(config.Logger, config.TracerProvider, config.DB, config.Sessions, config.Authz, gate, authorizer, config.ServerURL.JoinPath("platform-mcp").String(), registrations, readiness, distributions, config.JWTSigningKey, catalog))
	o11y.AttachHandler(config.Mux, http.MethodPost, platformmcp.Path, runtime.Handler().ServeHTTP)
	return AssistantSurface{Tools: runtime.AssistantTools(), Authorizer: authorizer}, nil
}

// platformMCPSetupResources builds the reviewed setup corpus this deployment
// serves. A failure here is a composition failure, not a degraded feature: a
// corpus that silently lost a provider looks exactly like one that never
// covered it, and the model would be left to invent the steps.
func platformMCPSetupResources(config platformMCPConfig) ([]platformmcp.SetupResource, error) {
	// The one redirect_uri for every provider and slug, derived the same way
	// externalmcp, remotesessions, and the dashboard derive it.
	callbackURL := config.ServerURL.JoinPath("mcp", "remote_login_callback").String()
	resources, err := setupcorpus.Build(setupcorpus.Options{OAuthCallbackURL: callbackURL})
	if err != nil {
		return nil, fmt.Errorf("build platform mcp setup corpus: %w", err)
	}
	return resources, nil
}

func newPlatformMCPLifecycleMetadataService(config platformMCPConfig) (*platformmcp.LifecycleMetadataService, error) {
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

func newPlatformMCPLifecycleVisibilityService(config platformMCPConfig, readiness *platformmcp.ReadinessService) (*platformmcp.LifecycleVisibilityService, error) {
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
		if config.TemporalEnv == nil {
			return nil
		}
		var result []error
		for _, domainID := range domainIDs {
			if _, err := (&background.CustomDomainRegistrationClient{TemporalEnv: config.TemporalEnv}).ExecuteCustomDomainReconcile(ctx, domainID); err != nil {
				result = append(result, err)
			}
		}
		return errors.Join(result...)
	}, readiness, config.JWTSigningKey)
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
	)
}

func loadBrowserPlatformMCPCatalogDescriptors(ctx context.Context, catalog *externalmcp.CatalogService) ([]platformmcp.RegistryCatalogSource, error) {
	if catalog == nil {
		return nil, errors.New("shared MCP catalogue service is not configured")
	}
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
	authorizer := platformmcp.NewLiveOrgAdminAuthorizer(config.DB, config.Authz)
	oauthTelemetry := platformmcp.NewOAuthTelemetry(config.Logger, config.MeterProvider)
	oauthStore := platformmcp.NewPostgresOAuthStore(config.DB).WithTelemetry(oauthTelemetry)
	oauth, err := platformmcp.NewOAuthHTTP(platformmcp.OAuthHTTPConfig{
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
		// Backs the inbound CIMD document fetcher's SSRF protection; without
		// it the authorization server serves DCR only.
		GuardianPolicy: config.GuardianPolicy,
		MeterProvider:  config.MeterProvider,
	})
	if err != nil {
		return AssistantSurface{}, fmt.Errorf("create platform mcp oauth service: %w", err)
	}
	authenticator, err := platformmcp.NewJWTAuthenticator(sessiontokens.NewSigner(config.JWTSigningKey), config.DB, config.Encryption, oauth.Issuer(), oauth.Audience())
	if err != nil {
		return AssistantSurface{}, fmt.Errorf("create platform mcp authenticator: %w", err)
	}

	catalog := platformmcp.NewDynamicRegistryCatalogSources(func(ctx context.Context) ([]platformmcp.RegistryCatalogSource, error) {
		return loadBrowserPlatformMCPCatalogDescriptors(ctx, config.Catalog)
	})
	store, err := platformmcp.NewRegistrationStore(config.DB, platformmcp.RegistrationStoreConfig{ActiveRegistrationCap: 5})
	if err != nil {
		return AssistantSurface{}, fmt.Errorf("create Platform MCP registration store: %w", err)
	}
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
		Handoff:      newBudget(platformmcp.HandoffConnectionLimitName, platformmcp.HandoffOrganizationLimitName),
		SetupStart:   newBudget(platformmcp.SetupConnectionLimitName, platformmcp.SetupOrganizationLimitName),
		Repair:       newBudget(platformmcp.RepairConnectionLimitName, platformmcp.RepairOrganizationLimitName),
		// Documentation search is metered on its own allowances rather than the
		// shared five-per-minute budget: retrieval is in-process and reading is
		// what the corpus is for, so a caller researching a setup should not
		// spend the budget that its registration call needs.
		Docs: platformmcp.OperationBudget{
			Connection:   ratelimit.New(limitStore, platformmcp.DocsConnectionLimitName, ratelimit.PerMinute(platformmcp.DocsQueriesPerConnectionPerMinute), ratelimit.WithMetrics(config.MeterProvider)),
			Organization: ratelimit.New(limitStore, platformmcp.DocsOrganizationLimitName, ratelimit.PerMinute(platformmcp.DocsQueriesPerOrganizationPerMinute), ratelimit.WithMetrics(config.MeterProvider)),
		},
		Skills:            newBudget(platformmcp.SkillsConnectionLimitName, platformmcp.SkillsOrganizationLimitName),
		LifecycleMetadata: newBudget(platformmcp.LifecycleConnectionLimitName, platformmcp.LifecycleOrganizationLimitName),
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
	if !budgets.Valid() {
		return AssistantSurface{}, errors.New("platform MCP operation budgets are incomplete")
	}
	telemetry := platformmcp.NewLifecycleTelemetry(config.Logger, config.MeterProvider)
	readiness := platformmcp.NewReadinessService(
		store,
		registrationGate,
		adapters,
		ratelimit.New(limitStore, platformmcp.ForcedReadinessProbeLimit, ratelimit.PerMinute(platformmcp.ForcedReadinessProbesPerMinute), ratelimit.WithMetrics(config.MeterProvider)),
		budgets.Repair,
		platformmcp.NewRemoteMCPReadinessProber(config.Logger, config.DB, config.Encryption, config.GuardianPolicy, config.RemoteChallengeManager),
	).WithTelemetry(telemetry)
	lifecycleMetadata, err := newPlatformMCPLifecycleMetadataService(config)
	if err != nil {
		return AssistantSurface{}, fmt.Errorf("create local Platform MCP lifecycle metadata service: %w", err)
	}
	lifecycleVisibility, err := newPlatformMCPLifecycleVisibilityService(config, readiness)
	if err != nil {
		return AssistantSurface{}, fmt.Errorf("create browser Platform MCP lifecycle visibility service: %w", err)
	}
	registrations := platformmcp.NewRegistrationService(catalog, registrationGate, store).
		WithDirectRemoteInspector(platformmcp.NewGuardianDirectRemoteInspector(config.GuardianPolicy)).
		WithLifecycleMetadata(lifecycleMetadata).
		WithLifecycleVisibility(lifecycleVisibility).
		WithOperationBudgets(budgets).
		WithReadiness(readiness).
		WithDashboardURL(config.DashboardURL).
		WithIdentityProviderAttachment(platformmcp.NewCatalogIdentityProviderAttachmentService(config.DB, config.Encryption, config.GuardianPolicy, config.AuditLogger, config.ServerURL)).
		WithClientAdmission(platformmcp.NewClientAdmissionService(config.DB, config.AuditLogger)).
		WithTelemetry(telemetry)
	dashboardSetupStarter := platformmcp.NewDashboardSetupService(store, registrationGate, authorizer, adapters, budgets.SetupStart)
	feedback := platformmcp.NewFeedbackService(config.DB)
	setupResources, err := platformMCPSetupResources(config)
	if err != nil {
		return AssistantSurface{}, err
	}
	pluginInventory := platformmcp.NewPluginsService(config.DB, budgets.Plugins, config.JWTSigningKey)
	distributions := newPlatformMCPDistributionService(config, pluginInventory)
	skillAuthoring := platformmcp.NewSkillsService(config.Skills, platformmcp.NewPostgresSkillTargets(config.DB), store, config.Authz, registrationGate, budgets.Skills)
	platformReader := platformmcp.NewPostgresReader(config.Logger, config.DB)
	diagnostics := platformmcp.NewDiagnosticsService(config.DB, config.Telemetry, config.SessionCapture, platformReader, readiness, budgets.Diagnostics).
		WithDrilldown(config.TelemetryDrilldown, config.JWTSigningKey, budgets.SensitiveDiagnostics, budgets.DrilldownVolume, platformmcp.NewPostgresDrilldownAuditor(config.DB))
	sessionRecall := platformmcp.NewSessionRecallService(config.Logger, config.DB, platformrepo.New(config.DB), audit.NewLogger(), config.SessionPortability, budgets.SensitiveSessionRecall)
	riskMutationControls, err := platformmcp.NewRiskMutationControls(config.DB, config.FeatureFlags, platformmcp.NewPostgresOrganizationSlugResolver(config.DB), budgets.RiskMutations, config.JWTSigningKey)
	if err != nil {
		return AssistantSurface{}, fmt.Errorf("create browser Platform MCP risk mutation controls: %w", err)
	}
	riskMutations, err := platformmcp.NewRiskMutationHandlers(
		config.DB,
		riskMutationControls,
		risk.NewPolicyMutationCore(config.DB, config.AuditLogger, config.RiskPolicyApprovals, config.RiskPolicySignaler, config.RiskPolicyCache),
		risk.NewExclusionMutationCore(config.Logger, config.DB, config.AuditLogger, config.RiskExclusionReconciler, config.JWTSigningKey),
	)
	if err != nil {
		return AssistantSurface{}, fmt.Errorf("create browser Platform MCP risk policy mutations: %w", err)
	}
	runtime := platformmcp.NewRuntimeWithRiskMutations(
		config.Logger,
		authenticator,
		gate,
		authorizer,
		oauth.ProtectedResourceURL(),
		config.JWTSigningKey,
		platformReader,
		catalog,
		registrations,
		platformmcp.NewPostgresReadinessRecorder(config.DB),
		setupResources,
		feedback,
		platformmcp.NewOnboardingService(config.DB),
		distributions,
		skillAuthoring,
		diagnostics,
		pluginInventory,
		sessionRecall,
		riskMutations,
		platformmcp.CatalogDescriptor{},
	).WithOAuthTelemetry(oauthTelemetry)
	oauth.Attach(config.Mux)
	platformmcp.NewDashboardSetupHTTP(dashboardSetupStarter, config.Sessions).Attach(config.Mux)
	platformmcp.AttachManagement(config.Mux, platformmcp.NewManagementService(config.Logger, config.TracerProvider, config.DB, config.Sessions, config.Authz, gate, authorizer, config.ServerURL.JoinPath("platform-mcp").String(), registrations, readiness, distributions, config.JWTSigningKey, catalog))
	o11y.AttachHandler(config.Mux, http.MethodPost, platformmcp.Path, runtime.Handler().ServeHTTP)
	return AssistantSurface{Tools: runtime.AssistantTools(), Authorizer: authorizer}, nil
}
