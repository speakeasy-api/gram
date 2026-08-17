package productfeatures

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"
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
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/productfeatures/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// Service implements organization feature management operations.
type Service struct {
	tracer       trace.Tracer
	logger       *slog.Logger
	db           *pgxpool.Pool
	repo         *repo.Queries
	auth         *auth.Auth
	authz        *authz.Engine
	featureCache cache.TypedCacheObject[FeatureCache]
	audit        *audit.Logger
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

	return &Service{
		tracer:       tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/productfeatures"),
		logger:       logger,
		db:           db,
		repo:         repo.New(db),
		auth:         auth.New(logger, db, sessions, authzEngine),
		authz:        authzEngine,
		featureCache: cache.NewTypedObjectCache[FeatureCache](logger.With(attr.SlogCacheNamespace("productfeature")), cache.NewRedisCacheAdapter(redisClient), cache.SuffixNone),
		audit:        auditLogger,
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
	if payload.FeatureName == string(FeatureSkills) && !payload.Enabled {
		return nil
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "begin feature flag transaction").LogError(ctx, s.logger, attr.SlogOrganizationID(orgID))
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	// changed is derived from the write itself — the insert either landed or
	// hit the active-row conflict, the soft delete either matched a live row
	// or found none — so the audit below records exactly the transitions that
	// commit, immune to read-then-write races.
	q := repo.New(dbtx)
	changed := false
	if payload.Enabled && payload.FeatureName == string(FeatureSkills) {
		// Skills enablement also provisions the built-in RBAC grants, so it
		// goes through its dedicated transactional path.
		if err := EnableSkillsTx(ctx, dbtx, orgID); err != nil {
			return oops.E(oops.CodeUnexpected, err, "enable Skills feature").LogError(ctx, s.logger, attr.SlogOrganizationID(orgID))
		}
	} else if payload.Enabled && payload.FeatureName == string(FeatureMCPApproval) {
		// MCP approval enablement also provisions the built-in RBAC grants —
		// without them every approval surface answers 403, since the system
		// role seeder never revisits roles that already hold grants.
		if err := EnableMCPApprovalTx(ctx, dbtx, orgID); err != nil {
			return oops.E(oops.CodeUnexpected, err, "enable MCP approval feature").LogError(ctx, s.logger, attr.SlogOrganizationID(orgID))
		}
	} else if payload.Enabled {
		inserted, err := q.EnableFeature(ctx, repo.EnableFeatureParams{
			OrganizationID: orgID,
			FeatureName:    payload.FeatureName,
		})
		if err != nil {
			return oops.E(oops.CodeUnexpected, err, "enable organization feature flag %q", payload.FeatureName).LogError(ctx, s.logger, attr.SlogOrganizationID(orgID))
		}
		changed = inserted > 0
	} else {
		_, err := q.DeleteFeature(ctx, repo.DeleteFeatureParams{
			OrganizationID: orgID,
			FeatureName:    payload.FeatureName,
		})
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			// Already disabled — a no-op, mirroring how enabling an enabled
			// feature is a no-op.
		case err != nil:
			return oops.E(oops.CodeUnexpected, err, "disable organization feature flag %q", payload.FeatureName).LogError(ctx, s.logger, attr.SlogOrganizationID(orgID))
		default:
			changed = true
		}
	}

	// Fail-open governs whether blocking policies are enforced during a
	// control-plane outage, so flipping it is a security-posture change that
	// must leave an audit trail.
	if payload.FeatureName == string(FeatureHooksFailOpen) && changed {
		org, err := orgrepo.New(dbtx).GetOrganizationMetadata(ctx, orgID)
		if err != nil {
			return oops.E(oops.CodeUnexpected, err, "read organization for hooks fail-open audit event").LogError(ctx, s.logger, attr.SlogOrganizationID(orgID))
		}
		if err := s.audit.LogOrganizationHooksFailOpenToggled(ctx, dbtx, audit.LogOrganizationHooksFailOpenToggledEvent{
			OrganizationID:   orgID,
			Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
			ActorDisplayName: authCtx.Email,
			ActorSlug:        nil,
			OrganizationName: org.Name,
			OrganizationSlug: org.Slug,
			FailOpenEnabled:  payload.Enabled,
		}); err != nil {
			return oops.E(oops.CodeUnexpected, err, "record hooks fail-open audit event").LogError(ctx, s.logger, attr.SlogOrganizationID(orgID))
		}
	}

	if err := dbtx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "commit feature flag change").LogError(ctx, s.logger, attr.SlogOrganizationID(orgID))
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

func (s *Service) SetRemoteSessionAutoRefreshPolicy(ctx context.Context, payload *gen.SetRemoteSessionAutoRefreshPolicyPayload) error {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ActiveOrganizationID == "" {
		return oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return fmt.Errorf("require org admin: %w", err)
	}

	var visible, enforced bool
	switch payload.Policy {
	case "disabled":
	case "user_controlled":
		visible = true
	case "enforced":
		enforced = true
	default:
		return oops.C(oops.CodeBadRequest)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "begin remote session refresh policy transaction").LogError(ctx, s.logger, attr.SlogOrganizationID(authCtx.ActiveOrganizationID))
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	q := repo.New(dbtx)
	setFeatureState := func(feature Feature, enabled bool) error {
		if enabled {
			_, err := q.EnableFeature(ctx, repo.EnableFeatureParams{
				OrganizationID: authCtx.ActiveOrganizationID,
				FeatureName:    string(feature),
			})
			if err != nil {
				return fmt.Errorf("enable %s: %w", feature, err)
			}
			return nil
		}

		_, err := q.DeleteFeature(ctx, repo.DeleteFeatureParams{
			OrganizationID: authCtx.ActiveOrganizationID,
			FeatureName:    string(feature),
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("disable %s: %w", feature, err)
		}
		return nil
	}

	if err := setFeatureState(FeatureRemoteSessionAutoRefresh, visible); err != nil {
		return oops.E(oops.CodeUnexpected, err, "set remote session refresh visibility").LogError(ctx, s.logger, attr.SlogOrganizationID(authCtx.ActiveOrganizationID))
	}
	if err := setFeatureState(FeatureRemoteSessionAutoRefreshEnforced, enforced); err != nil {
		return oops.E(oops.CodeUnexpected, err, "set remote session refresh enforcement").LogError(ctx, s.logger, attr.SlogOrganizationID(authCtx.ActiveOrganizationID))
	}
	if err := dbtx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "commit remote session refresh policy").LogError(ctx, s.logger, attr.SlogOrganizationID(authCtx.ActiveOrganizationID))
	}

	for _, state := range []struct {
		feature Feature
		enabled bool
	}{
		{feature: FeatureRemoteSessionAutoRefresh, enabled: visible},
		{feature: FeatureRemoteSessionAutoRefreshEnforced, enabled: enforced},
	} {
		cacheEntry := FeatureCache{
			OrganizationID: authCtx.ActiveOrganizationID,
			Feature:        state.feature,
			Enabled:        state.enabled,
		}
		if cacheErr := s.featureCache.Store(ctx, cacheEntry); cacheErr != nil {
			s.logger.WarnContext(ctx, "failed to cache remote session refresh policy",
				attr.SlogError(cacheErr),
				attr.SlogOrganizationID(authCtx.ActiveOrganizationID),
				attr.SlogProductFeatureName(string(state.feature)),
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

	// device_agent is derived from device-agent sync activity, not an
	// organization_features row, so it bypasses isEnabled. A read failure degrades
	// to false rather than failing the whole call.
	deviceAgent, err := s.repo.HasDeviceAgentSync(ctx, orgID)
	if err != nil {
		s.logger.WarnContext(ctx, "failed to check device agent syncs",
			attr.SlogError(err),
			attr.SlogOrganizationID(orgID),
		)
		deviceAgent = false
	}

	return &gen.GetProductFeaturesResult{
		LogsEnabled:                             isEnabled(FeatureLogs),
		ToolIoLogsEnabled:                       isEnabled(FeatureToolIOLogs),
		SessionCaptureEnabled:                   isEnabled(FeatureSessionCapture),
		AuthzChallengeLoggingEnabled:            isEnabled(FeatureAuthzChallengeLogging),
		SsoEnabled:                              isEnabled(FeatureSSO),
		ScimEnabled:                             isEnabled(FeatureSCIM),
		HooksBrowserLoginEnabled:                isEnabled(FeatureHooksBrowserLogin),
		HooksFailOpenEnabled:                    isEnabled(FeatureHooksFailOpen),
		CustomModelKeysEnabled:                  isEnabled(FeatureCustomModelKeys),
		SkillsEnabled:                           true,
		SkillCaptureMetadataOnly:                isEnabled(FeatureSkillCaptureMetadataOnly),
		AiPlatformPushIntegrationsEnabled:       isEnabled(FeatureAIPlatformPushIntegrations),
		PlatformMcpEnabled:                      isEnabled(FeaturePlatformMCP),
		CustomerManagedEncryptionKeysEnabled:    isEnabled(FeatureCustomerManagedEncryptionKeys),
		RemoteSessionAutoRefreshEnabled:         isEnabled(FeatureRemoteSessionAutoRefresh),
		RemoteSessionAutoRefreshEnforcedEnabled: isEnabled(FeatureRemoteSessionAutoRefreshEnforced),
		DeviceAgent:                             deviceAgent,
	}, nil
}

func (s *Service) APIKeyAuth(ctx context.Context, key string, schema *security.APIKeyScheme) (context.Context, error) {
	return s.auth.Authorize(ctx, key, schema)
}
