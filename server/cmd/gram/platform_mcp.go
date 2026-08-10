package gram

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel/metric"
	goahttp "goa.design/goa/v3/http"

	"github.com/speakeasy-api/gram/server/internal/auth/identity"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/externalmcp"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/platformmcp"
	"github.com/speakeasy-api/gram/server/internal/platformmcp/localfixture"
	"github.com/speakeasy-api/gram/server/internal/platformmcp/remotesessionprovider"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/ratelimit"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/sessiontokens"
)

var platformMCPLocalFixtureLoopbackCIDRBlocks = []string{"127.0.0.0/8", "::1/128"}

type platformMCPConfig struct {
	Logger                 *slog.Logger
	MeterProvider          metric.MeterProvider
	Mux                    goahttp.Muxer
	DB                     *pgxpool.Pool
	Redis                  *redis.Client
	ServerURL              *url.URL
	Environment            string
	JWTSigningKey          string
	ProductFeatures        *productfeatures.Client
	FeatureFlags           feature.Provider
	Authz                  *authz.Engine
	Encryption             *encryption.Client
	Identity               *identity.Resolver
	Sessions               *sessions.Manager
	Registry               *externalmcp.RegistryClient
	GuardianPolicy         *guardian.Policy
	RemoteChallengeManager *remotesessions.ChallengeManager
	LocalFixture           *platformMCPLocalFixtureConfig
}

// configurePlatformMCP composes the Platform MCP HTTP surfaces separately from
// the general server startup flow. Dashboard and MCP authentication remain at
// their respective transports; shared management reads are composed inside the
// Platform MCP runtime.
func configurePlatformMCP(ctx context.Context, config platformMCPConfig) error {
	organizationSlugs := platformmcp.NewPostgresOrganizationSlugResolver(config.DB)
	gate := platformmcp.NewOrganizationGate(config.ProductFeatures, config.FeatureFlags, organizationSlugs)
	authorizer := platformmcp.NewLiveOrgAdminAuthorizer(config.DB, config.Authz)
	oauth, err := platformmcp.NewOAuthHTTP(platformmcp.OAuthHTTPConfig{
		BaseURL:       config.ServerURL,
		Environment:   config.Environment,
		Cache:         cache.NewRedisCacheAdapter(config.Redis),
		Store:         platformmcp.NewPostgresOAuthStore(config.DB),
		Identity:      config.Identity,
		Gate:          gate,
		Authorizer:    authorizer,
		Organizations: platformmcp.NewLiveOrganizationSelector(config.DB, authorizer),
		Signer:        sessiontokens.NewSigner(config.JWTSigningKey),
		Encryption:    config.Encryption,
	})
	if err != nil {
		return fmt.Errorf("create platform mcp oauth service: %w", err)
	}
	authenticator, err := platformmcp.NewJWTAuthenticator(sessiontokens.NewSigner(config.JWTSigningKey), config.DB, config.Encryption, oauth.Issuer(), oauth.Audience())
	if err != nil {
		return fmt.Errorf("create platform mcp authenticator: %w", err)
	}

	var catalog platformmcp.Catalog
	var registrations *platformmcp.RegistrationService
	var dashboardSetupStarter platformmcp.DashboardSetupStarter
	var setupResources []platformmcp.SetupResource
	if config.LocalFixture != nil {
		fixtureConfig := config.LocalFixture.Fixture
		if err := config.Registry.ClearCache(ctx, fixtureConfig.Registry().URL); err != nil {
			return fmt.Errorf("clear local Platform MCP fixture registry cache: %w", err)
		}
		fixtureOAuth := localfixture.NewOAuthHTTP(fixtureConfig)
		fixtureMCP := localfixture.NewMCPHTTP(fixtureOAuth)
		fixtureRegistry := config.Registry.WithAllowedCIDRBlocks(platformMCPLocalFixtureLoopbackCIDRBlocks...)
		catalog = platformmcp.NewRegistryCatalog(fixtureRegistry, []platformmcp.CatalogDescriptor{fixtureConfig.CatalogDescriptor()})
		setupResources = fixtureConfig.SetupResources()
		store, err := platformmcp.NewRegistrationStore(config.DB, platformmcp.RegistrationStoreConfig{ActiveRegistrationCap: 5})
		if err != nil {
			return fmt.Errorf("create local Platform MCP registration store: %w", err)
		}
		registrationGate := platformmcp.NewCatalogRegistrationGate(gate, config.FeatureFlags, organizationSlugs)
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
		}
		if !budgets.Valid() {
			return errors.New("local Platform MCP operation budgets are incomplete")
		}
		telemetry := platformmcp.NewLifecycleTelemetry(config.Logger, config.MeterProvider)
		readiness := platformmcp.NewReadinessService(
			store,
			gate,
			adapters,
			ratelimit.New(limitStore, platformmcp.ForcedReadinessProbeLimit, ratelimit.PerMinute(1), ratelimit.WithMetrics(config.MeterProvider)),
			budgets.Repair,
		).WithTelemetry(telemetry)
		registrations = platformmcp.NewRegistrationService(catalog, registrationGate, store).WithOperationBudgets(budgets).WithReadiness(readiness).WithTelemetry(telemetry)
		dashboardSetupStarter = platformmcp.NewDashboardSetupService(store, registrationGate, authorizer, adapters, budgets.SetupStart)

		registryHandler := localfixture.NewRegistryHTTP(fixtureConfig).Handler()
		config.Mux.Handle(http.MethodGet, "/v0.1/servers", registryHandler.ServeHTTP)
		config.Mux.Handle(http.MethodGet, fixtureConfig.RegistryDetailsPath(), registryHandler.ServeHTTP)
		config.Mux.Handle(http.MethodGet, "/.well-known/oauth-authorization-server/platform-mcp/local-fixture", fixtureOAuth.Handler().ServeHTTP)
		config.Mux.Handle(http.MethodGet, "/platform-mcp/local-fixture/authorize", fixtureOAuth.Handler().ServeHTTP)
		config.Mux.Handle(http.MethodPost, "/platform-mcp/local-fixture/register", fixtureOAuth.Handler().ServeHTTP)
		config.Mux.Handle(http.MethodPost, "/platform-mcp/local-fixture/token", fixtureOAuth.Handler().ServeHTTP)
		config.Mux.Handle(http.MethodPost, "/platform-mcp/local-fixture/revoke", fixtureOAuth.Handler().ServeHTTP)
		config.Mux.Handle(http.MethodPost, "/platform-mcp/local-fixture/mcp", fixtureMCP.Handler().ServeHTTP)
	}

	runtime := platformmcp.NewRuntime(
		config.Logger,
		authenticator,
		gate,
		authorizer,
		oauth.ProtectedResourceURL(),
		config.JWTSigningKey,
		platformmcp.NewPostgresReader(config.DB),
		catalog,
		registrations,
		platformmcp.NewPostgresReadinessRecorder(config.DB),
		setupResources,
	)
	oauth.Attach(config.Mux)
	platformmcp.NewDashboardSetupHTTP(dashboardSetupStarter, config.Sessions).Attach(config.Mux)
	o11y.AttachHandler(config.Mux, http.MethodPost, platformmcp.Path, runtime.Handler().ServeHTTP)
	return nil
}
