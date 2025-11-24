package productfeatures

import (
	"context"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	gen "github.com/speakeasy-api/gram/server/gen/features"
	srv "github.com/speakeasy-api/gram/server/gen/http/features/server"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/productfeatures/repo"
)

// OpenRouterKeyRefresher defines the interface for managing openrouter key refresh workflows
type OpenRouterKeyRefresher interface {
	ScheduleOpenRouterKeyRefresh(ctx context.Context, orgID string) error
	CancelOpenRouterKeyRefreshWorkflow(ctx context.Context, orgID string) error
}

// Service implements organization feature management operations.
type Service struct {
	tracer              trace.Tracer
	logger              *slog.Logger
	db                  *pgxpool.Pool
	repo                *repo.Queries
	auth                *auth.Auth
	featureCache        cache.TypedCacheObject[FeatureCache]
	openRouterRefresher OpenRouterKeyRefresher
}

var _ gen.Service = (*Service)(nil)

func NewService(logger *slog.Logger, db *pgxpool.Pool, sessions *sessions.Manager, redisClient *redis.Client, openRouterRefresher OpenRouterKeyRefresher) *Service {
	logger = logger.With(attr.SlogComponent("productfeatures"))

	return &Service{
		tracer:              otel.Tracer("github.com/speakeasy-api/gram/server/internal/productfeatures"),
		logger:              logger,
		db:                  db,
		repo:                repo.New(db),
		auth:                auth.New(logger, db, sessions),
		featureCache:        cache.NewTypedObjectCache[FeatureCache](logger.With(attr.SlogCacheNamespace("productfeature")), cache.NewRedisCacheAdapter(redisClient), cache.SuffixNone),
		openRouterRefresher: openRouterRefresher,
	}
}

func Attach(mux goahttp.Muxer, service *Service) {
	endpoints := gen.NewEndpoints(service)
	endpoints.Use(middleware.MapErrors())
	endpoints.Use(middleware.TraceMethods(service.tracer))
	srv.Mount(
		mux,
		srv.New(endpoints, mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, nil),
	)
}

func (s *Service) SetProductFeature(ctx context.Context, payload *gen.SetProductFeaturePayload) error {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ActiveOrganizationID == "" {
		return oops.C(oops.CodeUnauthorized)
	}

	var err error

	if payload.Enabled {
		_, err = s.repo.EnableFeature(ctx, repo.EnableFeatureParams{
			OrganizationID: authCtx.ActiveOrganizationID,
			FeatureName:    payload.FeatureName,
		})
	} else {
		_, err = s.repo.DeleteFeature(ctx, repo.DeleteFeatureParams{
			OrganizationID: authCtx.ActiveOrganizationID,
			FeatureName:    payload.FeatureName,
		})
	}
	if err != nil {
		return oops.E(
			oops.CodeUnexpected,
			err,
			"failed to set organization feature flag %q",
			payload.FeatureName,
		).Log(ctx, s.logger, attr.SlogOrganizationID(authCtx.ActiveOrganizationID))
	}

	cacheEntry := FeatureCache{
		OrganizationID: authCtx.ActiveOrganizationID,
		Feature:        Feature(payload.FeatureName),
		Enabled:        payload.Enabled,
	}

	if cacheErr := s.featureCache.Store(ctx, cacheEntry); cacheErr != nil {
		s.logger.WarnContext(ctx, "failed to cache feature flag state",
			attr.SlogError(cacheErr),
			attr.SlogOrganizationID(authCtx.ActiveOrganizationID),
			attr.SlogProductFeatureName(payload.FeatureName),
		)
	}

	s.handleProductFeatureSideEffects(ctx, payload.FeatureName, authCtx.ActiveOrganizationID)

	return nil
}

func (s *Service) handleProductFeatureSideEffects(ctx context.Context, featureName string, organizationID string) {
	if featureName == string(FeatureChat) {
		// when we provide billable chat/agent usage we need to make sure the openrouter key being has enough credits to cover the usage
		// we cancel the workflow because the previous re-use polcy was allow duplicate failed only
		if err := s.openRouterRefresher.CancelOpenRouterKeyRefreshWorkflow(ctx, organizationID); err != nil {
			s.logger.WarnContext(ctx, "failed to cancel openrouter key refresh workflow",
				attr.SlogError(err),
				attr.SlogOrganizationID(organizationID),
			)
		}
		if err := s.openRouterRefresher.ScheduleOpenRouterKeyRefresh(ctx, organizationID); err != nil {
			s.logger.WarnContext(ctx, "failed to schedule openrouter key refresh workflow",
				attr.SlogError(err),
				attr.SlogOrganizationID(organizationID),
			)
		}
	}
}

func (s *Service) APIKeyAuth(ctx context.Context, key string, schema *security.APIKeyScheme) (context.Context, error) {
	return s.auth.Authorize(ctx, key, schema)
}
