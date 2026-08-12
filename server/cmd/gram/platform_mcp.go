//nolint:exhaustruct,wrapcheck // Composition intentionally relies on documented optional zero values.
package gram

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"

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
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/externalmcp"
	externalmcprepo "github.com/speakeasy-api/gram/server/internal/externalmcp/repo"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/platformmcp"
	"github.com/speakeasy-api/gram/server/internal/plugins"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/ratelimit"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/sessiontokens"
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
	ProductFeatures        *productfeatures.Client
	FeatureFlags           feature.Provider
	Authz                  *authz.Engine
	Encryption             *encryption.Client
	Identity               *identity.Resolver
	Sessions               *sessions.Manager
	Registry               *externalmcp.RegistryClient
	GuardianPolicy         *guardian.Policy
	RemoteChallengeManager *remotesessions.ChallengeManager
	AuditLogger            *audit.Logger
	PluginPublisher        *plugins.Service
}

// configurePlatformMCP composes the Platform MCP HTTP surfaces separately from
// the general server startup flow. Dashboard and MCP authentication remain at
// their respective transports; shared management reads are composed inside the
// Platform MCP runtime.
func configurePlatformMCP(ctx context.Context, config platformMCPConfig) error {
	return configureBrowserPlatformMCP(ctx, config)
}

func loadBrowserPlatformMCPCatalogDescriptors(ctx context.Context, db *pgxpool.Pool) ([]platformmcp.CatalogDescriptor, error) {
	browserRegistries, err := externalmcprepo.New(db).ListMCPRegistries(ctx)
	if err != nil {
		return nil, fmt.Errorf("list browser MCP catalogue registries for Platform MCP: %w", err)
	}
	browserDescriptors := make([]platformmcp.CatalogDescriptor, 0, len(browserRegistries))
	for _, registry := range browserRegistries {
		browserDescriptors = append(browserDescriptors, platformmcp.BrowserCatalogDescriptor(externalmcp.Registry{ID: registry.ID, URL: registry.Url}))
	}
	return browserDescriptors, nil
}

func configureBrowserPlatformMCP(ctx context.Context, config platformMCPConfig) error {
	gate := platformmcp.NewOrganizationGate(
		config.ProductFeatures,
		config.FeatureFlags,
		platformmcp.NewPostgresOrganizationSlugResolver(config.DB),
	)
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

	catalog := platformmcp.NewDynamicRegistryCatalog(config.Registry, func(ctx context.Context) ([]platformmcp.CatalogDescriptor, error) {
		return loadBrowserPlatformMCPCatalogDescriptors(ctx, config.DB)
	})
	store, err := platformmcp.NewRegistrationStore(config.DB, platformmcp.RegistrationStoreConfig{ActiveRegistrationCap: 5})
	if err != nil {
		return fmt.Errorf("create Platform MCP registration store: %w", err)
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
	}
	if !budgets.Valid() {
		return errors.New("platform MCP operation budgets are incomplete")
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
	registrations := platformmcp.NewRegistrationService(catalog, registrationGate, store).
		WithOperationBudgets(budgets).
		WithReadiness(readiness).
		WithDashboardURL(config.DashboardURL).
		WithIdentityProviderAttachment(platformmcp.NewCatalogIdentityProviderAttachmentService(config.DB, config.Encryption, config.GuardianPolicy, config.AuditLogger, config.ServerURL)).
		WithTelemetry(telemetry)
	dashboardSetupStarter := platformmcp.NewDashboardSetupService(store, registrationGate, authorizer, adapters, budgets.SetupStart)
	feedback := platformmcp.NewFeedbackService(config.DB)
	distributions := platformmcp.NewDistributionService(
		config.DB,
		config.AuditLogger,
		func(ctx context.Context, tx pgx.Tx, authCtx *contextvalues.AuthContext, organizationID string, projectID, mcpServerID uuid.UUID, displayName string) (uuid.UUID, bool, error) {
			attached, err := plugins.AttachToExistingDefaultPluginAudited(ctx, tx, config.AuditLogger, authCtx, organizationID, projectID, mcpServerID, displayName)
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
	)
	runtime := platformmcp.NewRuntimeWithLifecycle(
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
		nil,
		feedback,
		platformmcp.NewOnboardingService(config.DB),
		distributions,
		platformmcp.CatalogDescriptor{},
	)
	oauth.Attach(config.Mux)
	platformmcp.NewDashboardSetupHTTP(dashboardSetupStarter, config.Sessions).Attach(config.Mux)
	platformmcp.AttachManagement(config.Mux, platformmcp.NewManagementService(config.Logger, config.TracerProvider, config.DB, config.Sessions, config.Authz, gate, authorizer, config.ServerURL.JoinPath("platform-mcp").String(), registrations, readiness, distributions, config.JWTSigningKey, catalog))
	o11y.AttachHandler(config.Mux, http.MethodPost, platformmcp.Path, runtime.Handler().ServeHTTP)
	return nil
}
