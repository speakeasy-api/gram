package productfeatures

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	gen "github.com/speakeasy-api/gram/server/gen/features"
	srv "github.com/speakeasy-api/gram/server/gen/http/features/server"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/productfeatures/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

// OpenRouterKeyRefresher defines the interface for managing openrouter key refresh workflows
type OpenRouterKeyRefresher interface {
	ScheduleOpenRouterKeyRefresh(ctx context.Context, orgID string) error
	CancelOpenRouterKeyRefreshWorkflow(ctx context.Context, orgID string) error
}

// Service implements organization feature management operations.
type Service struct {
	tracer          trace.Tracer
	logger          *slog.Logger
	db              *pgxpool.Pool
	repo            *repo.Queries
	auth            *auth.Auth
	authz           *authz.Engine
	featureCache    cache.TypedCacheObject[FeatureCache]
	exclusionsCache cache.TypedCacheObject[SessionCaptureExclusionsCache]
	audit           *audit.Logger
}

var _ gen.Service = (*Service)(nil)

func NewService(
	logger *slog.Logger,
	tracerProvider trace.TracerProvider,
	db *pgxpool.Pool,
	sessions *sessions.Manager,
	redisClient *redis.Client,
	authzEngine *authz.Engine,
	auditLogger *audit.Logger,
) *Service {
	logger = logger.With(attr.SlogComponent("product_features"))
	cacheAdapter := cache.NewRedisCacheAdapter(redisClient)

	return &Service{
		tracer:          tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/productfeatures"),
		logger:          logger,
		db:              db,
		repo:            repo.New(db),
		auth:            auth.New(logger, db, sessions, authzEngine),
		authz:           authzEngine,
		featureCache:    cache.NewTypedObjectCache[FeatureCache](logger.With(attr.SlogCacheNamespace("productfeature")), cacheAdapter, cache.SuffixNone),
		exclusionsCache: cache.NewTypedObjectCache[SessionCaptureExclusionsCache](logger.With(attr.SlogCacheNamespace("session_capture_exclusions")), cacheAdapter, cache.SuffixNone),
		audit:           auditLogger,
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
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return fmt.Errorf("require org admin: %w", err)
	}

	var err error

	if payload.Enabled {
		err = s.repo.EnableFeature(ctx, repo.EnableFeatureParams{
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
		).LogError(ctx, s.logger, attr.SlogOrganizationID(authCtx.ActiveOrganizationID))
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

	return nil
}

func (s *Service) GetProductFeatures(ctx context.Context, payload *gen.GetProductFeaturesPayload) (*gen.GetProductFeaturesResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ActiveOrganizationID == "" {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgRead, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, fmt.Errorf("require org read: %w", err)
	}

	orgID := authCtx.ActiveOrganizationID

	// Helper function to check if a feature is enabled (cache first, then DB)
	isEnabled := func(feature Feature) bool {
		cacheKey := FeatureCacheKey(orgID, feature)

		// Try cache first
		cached, err := s.featureCache.Get(ctx, cacheKey)
		if err == nil {
			return cached.Enabled
		}

		// Fall back to database
		enabled, err := s.repo.IsFeatureEnabled(ctx, repo.IsFeatureEnabledParams{
			OrganizationID: orgID,
			FeatureName:    string(feature),
		})
		if err != nil {
			s.logger.WarnContext(ctx, "failed to check feature flag",
				attr.SlogError(err),
				attr.SlogOrganizationID(orgID),
				attr.SlogProductFeatureName(string(feature)),
			)
			return false
		}

		// Cache the result
		cacheEntry := FeatureCache{
			OrganizationID: orgID,
			Feature:        feature,
			Enabled:        enabled,
		}
		if cacheErr := s.featureCache.Store(ctx, cacheEntry); cacheErr != nil {
			s.logger.WarnContext(ctx, "failed to cache feature flag state",
				attr.SlogError(cacheErr),
				attr.SlogOrganizationID(orgID),
				attr.SlogProductFeatureName(string(feature)),
			)
		}

		return enabled
	}

	return &gen.GetProductFeaturesResult{
		LogsEnabled:                  isEnabled(FeatureLogs),
		ToolIoLogsEnabled:            isEnabled(FeatureToolIOLogs),
		SessionCaptureEnabled:        isEnabled(FeatureSessionCapture),
		AuthzChallengeLoggingEnabled: isEnabled(FeatureAuthzChallengeLogging),
		Webhooks:                     isEnabled(FeatureWebhooks),
		SsoEnabled:                   isEnabled(FeatureSSO),
		ScimEnabled:                  isEnabled(FeatureSCIM),
		ObservabilityModeEnabled:     isEnabled(FeatureObservabilityMode),
	}, nil
}

func (s *Service) APIKeyAuth(ctx context.Context, key string, schema *security.APIKeyScheme) (context.Context, error) {
	return s.auth.Authorize(ctx, key, schema)
}

func (s *Service) ListSessionCaptureExclusions(ctx context.Context, _ *gen.ListSessionCaptureExclusionsPayload) (*gen.SessionCaptureExclusionsResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ActiveOrganizationID == "" {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgRead, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, fmt.Errorf("require org read: %w", err)
	}

	userIDs, err := s.repo.ListSessionCaptureExclusions(ctx, authCtx.ActiveOrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to list session capture exclusions").LogError(ctx, s.logger, attr.SlogOrganizationID(authCtx.ActiveOrganizationID))
	}
	if userIDs == nil {
		userIDs = []string{}
	}
	return &gen.SessionCaptureExclusionsResult{UserIds: userIDs}, nil
}

func (s *Service) SetSessionCaptureExclusions(ctx context.Context, payload *gen.SetSessionCaptureExclusionsPayload) (*gen.SessionCaptureExclusionsResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ActiveOrganizationID == "" {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	org := authCtx.ActiveOrganizationID
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: org, Dimensions: nil}); err != nil {
		return nil, fmt.Errorf("require org admin: %w", err)
	}

	// Normalize: drop empties, de-dup, preserve order.
	seen := make(map[string]struct{}, len(payload.UserIds))
	desired := make([]string, 0, len(payload.UserIds))
	for _, uid := range payload.UserIds {
		if uid == "" {
			continue
		}
		if _, dup := seen[uid]; dup {
			continue
		}
		seen[uid] = struct{}{}
		desired = append(desired, uid)
	}

	existing, err := s.repo.ListSessionCaptureExclusions(ctx, org)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list existing session capture exclusions").LogError(ctx, s.logger, attr.SlogOrganizationID(org))
	}
	existingSet := make(map[string]struct{}, len(existing))
	for _, uid := range existing {
		existingSet[uid] = struct{}{}
	}

	var added, removed []string
	for _, uid := range desired {
		if _, ok := existingSet[uid]; !ok {
			added = append(added, uid)
		}
	}
	for _, uid := range existing {
		if _, ok := seen[uid]; !ok {
			removed = append(removed, uid)
		}
	}

	// Display names for the changed users (single query), for audit rendering.
	names := map[string]string{}
	changed := append(append([]string{}, added...), removed...)
	if len(changed) > 0 {
		users, uerr := usersrepo.New(s.db).GetUsersByIDs(ctx, changed)
		if uerr != nil {
			return nil, oops.E(oops.CodeUnexpected, uerr, "load users for audit").LogError(ctx, s.logger, attr.SlogOrganizationID(org))
		}
		for _, u := range users {
			names[u.ID] = u.DisplayName
		}
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, s.logger, attr.SlogOrganizationID(org))
	}
	defer func() { _ = tx.Rollback(ctx) }()
	qtx := s.repo.WithTx(tx)

	if _, err := qtx.ClearSessionCaptureExclusions(ctx, org); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "clear session capture exclusions").LogError(ctx, s.logger, attr.SlogOrganizationID(org))
	}
	for _, uid := range desired {
		if _, err := qtx.AddSessionCaptureExclusion(ctx, repo.AddSessionCaptureExclusionParams{OrganizationID: org, UserID: uid}); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "add session capture exclusion").LogError(ctx, s.logger, attr.SlogOrganizationID(org))
		}
	}

	actor := urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID)
	for _, uid := range added {
		if err := s.audit.LogSessionCaptureExclusionAdd(ctx, tx, audit.LogSessionCaptureExclusionAddEvent{
			OrganizationID: org, Actor: actor, ActorDisplayName: authCtx.Email, ActorSlug: nil,
			ExcludedUserID: uid, ExcludedUserDisplayName: names[uid],
		}); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "audit add exclusion").LogError(ctx, s.logger)
		}
	}
	for _, uid := range removed {
		if err := s.audit.LogSessionCaptureExclusionRemove(ctx, tx, audit.LogSessionCaptureExclusionRemoveEvent{
			OrganizationID: org, Actor: actor, ActorDisplayName: authCtx.Email, ActorSlug: nil,
			ExcludedUserID: uid, ExcludedUserDisplayName: names[uid],
		}); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "audit remove exclusion").LogError(ctx, s.logger)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit session capture exclusions").LogError(ctx, s.logger, attr.SlogOrganizationID(org))
	}

	if cacheErr := s.exclusionsCache.Store(ctx, SessionCaptureExclusionsCache{OrganizationID: org, UserIDs: desired}); cacheErr != nil {
		s.logger.WarnContext(ctx, "failed to cache session capture exclusions", attr.SlogError(cacheErr), attr.SlogOrganizationID(org))
	}

	if desired == nil {
		desired = []string{}
	}
	return &gen.SessionCaptureExclusionsResult{UserIds: desired}, nil
}
