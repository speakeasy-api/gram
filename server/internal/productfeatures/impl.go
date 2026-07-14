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
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/productfeatures/repo"
)

// PluginPublisher lets this service propagate org-level settings that change
// generated plugin/hook output (currently observability mode) to the org's
// published marketplaces. It is a narrow interface (rather than a direct
// dependency on the plugins service) because the plugins package imports this
// one, so importing it back would create a cycle; the concrete *plugins.Service
// is injected as this interface in cmd/gram. Nil when plugin publishing is not
// configured, in which case observability-mode changes are not gated or
// republished here and the automated rollout propagates them instead.
type PluginPublisher interface {
	// HooksRolloutEligible reports whether the org is cleared for the current
	// observability (hooks) version, i.e. whether a hook-output change can be
	// published to it now.
	HooksRolloutEligible(ctx context.Context, orgID, orgSlug string) bool
	// RepublishOrganizationProjects republishes every connected project in the org
	// so a changed org-level setting reaches its marketplaces.
	RepublishOrganizationProjects(ctx context.Context, orgID string) error
}

// Service implements organization feature management operations.
type Service struct {
	tracer       trace.Tracer
	logger       *slog.Logger
	db           *pgxpool.Pool
	repo         *repo.Queries
	auth         *auth.Auth
	authz        *authz.Engine
	featureCache cache.TypedCacheObject[FeatureCache]
	plugins      PluginPublisher
}

var _ gen.Service = (*Service)(nil)

func NewService(
	logger *slog.Logger,
	tracerProvider trace.TracerProvider,
	db *pgxpool.Pool,
	sessions *sessions.Manager,
	redisClient *redis.Client,
	authzEngine *authz.Engine,
	pluginPublisher PluginPublisher,
) *Service {
	logger = logger.With(attr.SlogComponent("product_features"))

	return &Service{
		tracer:       tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/productfeatures"),
		logger:       logger,
		db:           db,
		repo:         repo.New(db),
		auth:         auth.New(logger, db, sessions, authzEngine),
		authz:        authzEngine,
		featureCache: cache.NewTypedObjectCache[FeatureCache](logger.With(attr.SlogCacheNamespace("productfeature")), cache.NewRedisCacheAdapter(redisClient), cache.SuffixNone),
		plugins:      pluginPublisher,
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

	orgID := authCtx.ActiveOrganizationID

	// Observability mode and the install-failure policy change the generated
	// observability (hooks) plugin output (non-blocking events, bootstrap exit
	// behavior). That output can only be regenerated at the current hooks
	// generator version, so a toggle can't take effect for an org that isn't
	// cleared for it. Reject the change up front — before writing the feature —
	// so the persisted feature state never claims a hook behavior that isn't
	// actually published. Only gate a real change, and only when plugin
	// publishing is wired.
	hookOutputToggle := payload.FeatureName == string(FeatureObservabilityMode) ||
		payload.FeatureName == string(FeatureHooksInstallFailOpen)
	hookOutputChanged := false
	if hookOutputToggle && s.plugins != nil {
		current, ferr := s.repo.IsFeatureEnabled(ctx, repo.IsFeatureEnabledParams{
			OrganizationID: orgID,
			FeatureName:    payload.FeatureName,
		})
		if ferr != nil {
			return oops.E(oops.CodeUnexpected, ferr, "check hooks feature state").LogError(ctx, s.logger, attr.SlogOrganizationID(orgID))
		}
		hookOutputChanged = current != payload.Enabled
		if hookOutputChanged && !s.plugins.HooksRolloutEligible(ctx, orgID, authCtx.OrganizationSlug) {
			return oops.E(oops.CodeConflict, nil, "can't change this hooks setting yet: your organization isn't approved for the latest observability hooks version. It will become available soon.")
		}
	}

	var err error

	if payload.Enabled {
		err = s.repo.EnableFeature(ctx, repo.EnableFeatureParams{
			OrganizationID: orgID,
			FeatureName:    payload.FeatureName,
		})
	} else {
		_, err = s.repo.DeleteFeature(ctx, repo.DeleteFeatureParams{
			OrganizationID: orgID,
			FeatureName:    payload.FeatureName,
		})
	}
	if err != nil {
		return oops.E(
			oops.CodeUnexpected,
			err,
			"failed to set organization feature flag %q",
			payload.FeatureName,
		).LogError(ctx, s.logger, attr.SlogOrganizationID(orgID))
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

	// Propagate a hook-output-affecting change to the org's published
	// marketplaces now. Eligibility was already verified above, so eligible
	// orgs regenerate their hooks immediately. This is best-effort: on failure
	// the feature is already written and the automated generator rollout
	// republishes the org on its next tick (the config-hash signal detects the
	// drift), so we log rather than fail the toggle.
	if hookOutputToggle && hookOutputChanged && s.plugins != nil {
		if repErr := s.plugins.RepublishOrganizationProjects(ctx, orgID); repErr != nil {
			s.logger.WarnContext(ctx, "failed to republish org plugins after hooks feature change; automated rollout will retry",
				attr.SlogError(repErr),
				attr.SlogOrganizationID(orgID),
			)
		}
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
		HooksBrowserLoginEnabled:     isEnabled(FeatureHooksBrowserLogin),
		HooksInstallFailOpenEnabled:  isEnabled(FeatureHooksInstallFailOpen),
		CustomModelKeysEnabled:       isEnabled(FeatureCustomModelKeys),
	}, nil
}

func (s *Service) APIKeyAuth(ctx context.Context, key string, schema *security.APIKeyScheme) (context.Context, error) {
	return s.auth.Authorize(ctx, key, schema)
}
