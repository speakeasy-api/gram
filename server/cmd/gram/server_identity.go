package gram

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/speakeasy-api/gram/server/internal/auth/chatsessions"
	"github.com/speakeasy-api/gram/server/internal/auth/identity"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/growthsignals"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/posthog"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/pylon"
	stripeclient "github.com/speakeasy-api/gram/server/internal/thirdparty/stripe"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	userrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
	"github.com/speakeasy-api/gram/server/internal/usersessions"
	"github.com/urfave/cli/v2"
	"go.opentelemetry.io/otel/trace"
)

type serverIdentity struct {
	Posthog         *posthog.Posthog
	Features        feature.Provider
	WorkOS          *workos.Client
	WorkOSAvailable bool
	Stripe          stripeclient.Client
	Billing         billing.Repository
	BillingTracker  billing.Tracker
	ProductFeatures *productfeatures.Client
	SiteURL         *url.URL
	Growth          *growthsignals.Emitter
	Identity        *identity.Resolver
	Sessions        *sessions.Manager
	ChatSessions    *chatsessions.Manager
}

func newServerIdentity(ctx context.Context, c *cli.Context, logger *slog.Logger, tracerProvider trace.TracerProvider, db *pgxpool.Pool, redisClient *redis.Client, guardianPolicy *guardian.Policy) (*serverIdentity, error) {
	pylonClient, err := pylon.NewPylon(logger, c.String("pylon-verification-secret"))
	if err != nil {
		return nil, fmt.Errorf("failed to create pylon client: %w", err)
	}
	posthogClient := posthog.New(ctx, logger, c.String("posthog-api-key"), c.String("posthog-endpoint"), c.String("posthog-personal-api-key"))
	var featureFlags feature.Provider = posthogClient
	if c.String("environment") == "local" {
		featureFlags = newLocalFeatureFlags(ctx, logger, c.String("local-feature-flags-csv"))
	}
	workosClient, available, err := newWorkOSClient(guardianPolicy, c)
	if err != nil {
		return nil, fmt.Errorf("failed to create WorkOS client: %w", err)
	}
	stripeClient, err := newStripeClient(ctx, logger, guardianPolicy, c)
	if err != nil {
		return nil, err
	}
	billingRepo, billingTracker, err := newBillingProvider(ctx, logger, tracerProvider, guardianPolicy, redisClient, posthogClient, stripeClient, c)
	if err != nil {
		return nil, err
	}
	umClient := newIDPUserManagementClient(guardianPolicy, c.String("idp-client-secret"), c)
	if umClient == nil {
		return nil, fmt.Errorf("failed to create IDP user management client: idp-client-secret is required")
	}
	idpClient := identity.NewWorkOSAdapter(umClient)
	productFeatures := productfeatures.NewClient(logger, tracerProvider, db, redisClient)
	siteURL, err := url.Parse(c.String("site-url"))
	if err != nil {
		return nil, fmt.Errorf("failed to parse site url: %w", err)
	}
	growth := growthsignals.NewEmitter(logger, posthogClient, growthsignals.NewDatabaseEnricher(db), siteURL)
	resolver := identity.NewResolver(logger, tracerProvider, cache.NewRedisCacheAdapter(redisClient), c.String("idp-base-url"), c.String("idp-client-id"), idpClient, workosClient, orgrepo.New(db), userrepo.New(db), pylonClient, posthogClient, growth, cache.SuffixNone)
	manager := sessions.NewManager(logger, tracerProvider, db, redisClient, cache.SuffixNone, idpClient, billingRepo, resolver)
	return &serverIdentity{Posthog: posthogClient, Features: featureFlags, WorkOS: workosClient, WorkOSAvailable: available, Stripe: stripeClient, Billing: billingRepo, BillingTracker: billingTracker, ProductFeatures: productFeatures, SiteURL: siteURL, Growth: growth, Identity: resolver, Sessions: manager, ChatSessions: chatsessions.NewManager(logger, redisClient, c.String(usersessions.JWTSigningKeyFlag))}, nil
}
