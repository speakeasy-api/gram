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
	tracer        trace.Tracer
	logger        *slog.Logger
	db            *pgxpool.Pool
	repo          *repo.Queries
	auth          *auth.Auth
	authz         *authz.Engine
	featureClient *Client
	audit         *audit.Logger
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
		tracer:        tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/productfeatures"),
		logger:        logger,
		db:            db,
		repo:          repo.New(db),
		auth:          auth.New(logger, db, sessions, authzEngine),
		authz:         authzEngine,
		featureClient: NewClient(logger, tracerProvider, db, redisClient),
		audit:         auditLogger,
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

// authorizeOrganization returns the organization metadata row when it had to
// read one to verify a cross-organization request, and nil for the active-org
// path, so callers that need the row can skip a second lookup.
func (s *Service) authorizeOrganization(ctx context.Context, organizationID string, scope authz.Scope) (*contextvalues.AuthContext, string, *orgrepo.OrganizationMetadatum, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, "", nil, oops.C(oops.CodeUnauthorized)
	}

	if organizationID == "" {
		return nil, "", nil, oops.C(oops.CodeUnauthorized)
	}

	if organizationID == authCtx.ActiveOrganizationID {
		check := authz.Check{Scope: scope, ResourceKind: "", ResourceID: organizationID, Dimensions: nil}
		if err := s.authz.Require(ctx, check); err != nil {
			return nil, "", nil, fmt.Errorf("require %s: %w", scope, err)
		}
		return authCtx, organizationID, nil, nil
	}

	if err := s.authz.RequireUserOrganizationScope(ctx, organizationID, authCtx.UserID, scope); err != nil {
		return nil, "", nil, fmt.Errorf("require %s for requested organization: %w", scope, err)
	}

	org, err := orgrepo.New(s.db).GetOrganizationMetadata(ctx, organizationID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, "", nil, oops.E(oops.CodeNotFound, err, "organization not found")
		}
		return nil, "", nil, oops.E(oops.CodeUnexpected, err, "get organization metadata").LogError(ctx, s.logger, attr.SlogOrganizationID(organizationID))
	}

	return authCtx, organizationID, &org, nil
}

func (s *Service) SetProductFeature(ctx context.Context, payload *gen.SetProductFeaturePayload) error {
	authCtx, orgID, org, err := s.authorizeOrganization(ctx, payload.OrganizationID, authz.ScopeOrgAdmin)
	if err != nil {
		return err
	}

	// Disabling skills is a silent no-op (skills are always on), so it stays
	// available to org admins and resolves before the staff-only gate below.
	if payload.FeatureName == string(FeatureSkills) && !payload.Enabled {
		return nil
	}

	// Staff-managed entitlements (SSO, SCIM, ...) must not be self-granted by
	// organization admins; org-settable operational toggles need org:admin only.
	if Feature(payload.FeatureName).RequiresPlatformAdmin() {
		if _, _, err := auth.RequirePlatformAdmin(ctx, s.logger); err != nil {
			return err
		}
	}

	lockConn, releaseFeatureLock, err := s.featureClient.acquireFeatureCacheLocks(ctx, orgID, []Feature{Feature(payload.FeatureName)})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "lock feature cache state").LogError(ctx, s.logger, attr.SlogOrganizationID(orgID))
	}
	defer releaseFeatureLock()

	dbtx, err := lockConn.Begin(ctx)
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
		inserted, err := EnableSkillsTx(ctx, dbtx, orgID)
		if err != nil {
			return oops.E(oops.CodeUnexpected, err, "enable Skills feature").LogError(ctx, s.logger, attr.SlogOrganizationID(orgID))
		}
		changed = inserted
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

	if changed {
		if org == nil {
			row, err := orgrepo.New(dbtx).GetOrganizationMetadata(ctx, orgID)
			if err != nil {
				return oops.E(oops.CodeUnexpected, err, "read organization for feature toggle audit event").LogError(ctx, s.logger, attr.SlogOrganizationID(orgID))
			}
			org = &row
		}
		if err := s.audit.LogOrganizationProductFeatureToggled(ctx, dbtx, audit.LogOrganizationProductFeatureToggledEvent{
			OrganizationID:   orgID,
			Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
			ActorDisplayName: authCtx.Email,
			ActorSlug:        nil,
			OrganizationName: org.Name,
			OrganizationSlug: org.Slug,
			FeatureName:      payload.FeatureName,
			FeatureEnabled:   payload.Enabled,
		}); err != nil {
			return oops.E(oops.CodeUnexpected, err, "record feature toggle audit event").LogError(ctx, s.logger, attr.SlogOrganizationID(orgID))
		}
	}

	if err := dbtx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "commit feature flag change").LogError(ctx, s.logger, attr.SlogOrganizationID(orgID))
	}

	_ = s.featureClient.storeFeatureCache(ctx, orgID, Feature(payload.FeatureName), payload.Enabled, "failed to cache feature flag state")

	return nil
}

func (s *Service) SetRemoteSessionAutoRefreshPolicy(ctx context.Context, payload *gen.SetRemoteSessionAutoRefreshPolicyPayload) error {
	_, orgID, _, err := s.authorizeOrganization(ctx, payload.OrganizationID, authz.ScopeOrgAdmin)
	if err != nil {
		return err
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

	lockConn, releaseFeatureLocks, err := s.featureClient.acquireFeatureCacheLocks(ctx, orgID, []Feature{
		FeatureRemoteSessionAutoRefresh, FeatureRemoteSessionAutoRefreshEnforced,
	})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "lock remote session refresh cache state").LogError(ctx, s.logger, attr.SlogOrganizationID(orgID))
	}
	defer releaseFeatureLocks()

	dbtx, err := lockConn.Begin(ctx)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "begin remote session refresh policy transaction").LogError(ctx, s.logger, attr.SlogOrganizationID(orgID))
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	q := repo.New(dbtx)
	setFeatureState := func(feature Feature, enabled bool) error {
		if enabled {
			_, err := q.EnableFeature(ctx, repo.EnableFeatureParams{
				OrganizationID: orgID,
				FeatureName:    string(feature),
			})
			if err != nil {
				return fmt.Errorf("enable %s: %w", feature, err)
			}
			return nil
		}

		_, err := q.DeleteFeature(ctx, repo.DeleteFeatureParams{
			OrganizationID: orgID,
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
		return oops.E(oops.CodeUnexpected, err, "set remote session refresh visibility").LogError(ctx, s.logger, attr.SlogOrganizationID(orgID))
	}
	if err := setFeatureState(FeatureRemoteSessionAutoRefreshEnforced, enforced); err != nil {
		return oops.E(oops.CodeUnexpected, err, "set remote session refresh enforcement").LogError(ctx, s.logger, attr.SlogOrganizationID(orgID))
	}
	if err := dbtx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "commit remote session refresh policy").LogError(ctx, s.logger, attr.SlogOrganizationID(orgID))
	}

	for _, state := range []struct {
		feature Feature
		enabled bool
	}{
		{feature: FeatureRemoteSessionAutoRefresh, enabled: visible},
		{feature: FeatureRemoteSessionAutoRefreshEnforced, enabled: enforced},
	} {
		_ = s.featureClient.storeFeatureCache(ctx, orgID, state.feature, state.enabled, "failed to cache remote session refresh policy")
	}

	return nil
}

func (s *Service) GetProductFeatures(ctx context.Context, payload *gen.GetProductFeaturesPayload) (*gen.GetProductFeaturesResult, error) {
	_, orgID, _, err := s.authorizeOrganization(ctx, payload.OrganizationID, authz.ScopeOrgRead)
	if err != nil {
		return nil, err
	}

	isEnabled := func(feature Feature) bool {
		enabled, err := s.featureClient.IsFeatureEnabled(ctx, orgID, feature)
		if err != nil {
			s.logger.WarnContext(ctx, "failed to check feature flag",
				attr.SlogError(err),
				attr.SlogOrganizationID(orgID),
				attr.SlogProductFeatureName(string(feature)),
			)
			return false
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
		ConsentToolFilteringEnabled:             isEnabled(FeatureConsentToolFiltering),
		SessionPortabilityEnabled:               isEnabled(FeatureSessionPortability),
		NetworkIngressEnabled:                   isEnabled(FeatureNetworkIngress),
		DeviceAgent:                             deviceAgent,
	}, nil
}

func (s *Service) APIKeyAuth(ctx context.Context, key string, schema *security.APIKeyScheme) (context.Context, error) {
	return s.auth.Authorize(ctx, key, schema)
}
