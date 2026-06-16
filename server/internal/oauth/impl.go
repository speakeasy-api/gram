package oauth

import (
	"log/slog"
	"net/url"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"

	_ "embed"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth/identity"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/cache"
	customdomains_repo "github.com/speakeasy-api/gram/server/internal/customdomains/repo"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/environments"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/oauth/repo"
	toolsets_repo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

type Service struct {
	logger            *slog.Logger
	tracer            trace.Tracer
	meter             metric.Meter
	db                *pgxpool.Pool
	toolsetsRepo      *toolsets_repo.Queries
	customDomainsRepo *customdomains_repo.Queries
	environments      *environments.EnvironmentEntries
	serverURL         *url.URL
	oauthRepo         *repo.Queries
	enc               *encryption.Client
	sessions          *sessions.Manager
	identity          *identity.Resolver
	guardianPolicy    *guardian.Policy
}

func NewService(logger *slog.Logger, tracerProvider trace.TracerProvider, meterProvider metric.MeterProvider, db *pgxpool.Pool, serverURL *url.URL, cacheImpl cache.Cache, enc *encryption.Client, env *environments.EnvironmentEntries, sessions *sessions.Manager, identityResolver *identity.Resolver, guardianPolicy *guardian.Policy) *Service {
	logger = logger.With(attr.SlogComponent("oauth"))
	tracer := tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/oauth")
	meter := meterProvider.Meter("github.com/speakeasy-api/gram/server/internal/oauth")

	return &Service{
		logger:            logger,
		tracer:            tracer,
		meter:             meter,
		db:                db,
		toolsetsRepo:      toolsets_repo.New(db),
		customDomainsRepo: customdomains_repo.New(db),
		environments:      env,
		serverURL:         serverURL,
		oauthRepo:         repo.New(db),
		enc:               enc,
		sessions:          sessions,
		identity:          identityResolver,
		guardianPolicy:    guardianPolicy,
	}
}

// Attach is retained as the OAuth service mount point. The legacy OAuth proxy
// serving endpoints have been removed; external-OAuth endpoints are mounted via
// AttachExternalOAuth.
func Attach(mux goahttp.Muxer, service *Service) {
}
