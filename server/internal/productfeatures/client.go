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

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/productfeatures/repo"
)

type Client struct {
	tracer       trace.Tracer
	logger       *slog.Logger
	repo         *repo.Queries
	featureCache cache.TypedCacheObject[FeatureCache]
}

func NewClient(logger *slog.Logger, tracerProvider trace.TracerProvider, db *pgxpool.Pool, redisClient *redis.Client) *Client {
	logger = logger.With(attr.SlogComponent("productfeatures"))

	return &Client{
		tracer:       tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/productfeatures"),
		logger:       logger,
		repo:         repo.New(db),
		featureCache: cache.NewTypedObjectCache[FeatureCache](logger.With(attr.SlogCacheNamespace("productfeature")), cache.NewRedisCacheAdapter(redisClient), cache.SuffixNone),
	}
}

func (c *Client) IsFeatureEnabled(ctx context.Context, organizationID string, feature Feature) (bool, error) {
	// Skills is generally available; the feature remains in the API for compatibility.
	if feature == FeatureSkills {
		return true, nil
	}

	if cached, err := c.featureCache.Get(ctx, FeatureCacheKey(organizationID, feature)); err == nil {
		return cached.Enabled, nil
	}

	res, err := c.repo.IsFeatureEnabled(ctx, repo.IsFeatureEnabledParams{
		OrganizationID: organizationID,
		FeatureName:    string(feature),
	})
	switch {
	case errors.Is(err, context.Canceled):
		// Do not cache results if the context was canceled, as this likely
		// indicates a timeout or shutdown in progress. Caching in this case
		// could lead to incorrect feature flag states being stored.
		return false, nil
	case errors.Is(err, pgx.ErrNoRows):
		// If there is no row, the feature is not enabled. Cache this result to
		// avoid hitting the database repeatedly for missing features.
		res = false
	case err != nil:
		return false, oops.E(
			oops.CodeUnexpected,
			err,
			"failed to get organization feature flag %q",
			string(feature),
		).LogError(ctx, c.logger, attr.SlogOrganizationID(organizationID))
	}

	cacheEntry := FeatureCache{
		OrganizationID: organizationID,
		Feature:        feature,
		Enabled:        res,
	}

	if cacheErr := c.featureCache.Store(ctx, cacheEntry); cacheErr != nil {
		c.logger.WarnContext(ctx, "failed to cache feature flag state",
			attr.SlogError(cacheErr),
			attr.SlogOrganizationID(organizationID),
			attr.SlogProductFeatureName(string(feature)),
		)
	}

	return res, nil
}

// IsFeatureEnabledUncached reads the durable feature state directly. Security-
// sensitive request gates use this when revocation must take effect faster than
// the shared feature cache TTL. Unlike the cached lookup, cancellation remains
// an error so callers can distinguish an incomplete live security check from a
// durable disabled result.
func (c *Client) IsFeatureEnabledUncached(ctx context.Context, organizationID string, feature Feature) (bool, error) {
	if feature == FeatureSkills {
		return true, nil
	}
	res, err := c.repo.IsFeatureEnabled(ctx, repo.IsFeatureEnabledParams{OrganizationID: organizationID, FeatureName: string(feature)})
	switch {
	case errors.Is(err, context.Canceled):
		return false, fmt.Errorf("check uncached organization feature: %w", err)
	case errors.Is(err, pgx.ErrNoRows):
		return false, nil
	case err != nil:
		return false, oops.E(
			oops.CodeUnexpected,
			err,
			"failed to get uncached organization feature flag %q",
			string(feature),
		).LogError(ctx, c.logger, attr.SlogOrganizationID(organizationID))
	default:
		return res, nil
	}
}

// PlatformFeatureCheck adapts IsFeatureEnabled to the
// platformtools.FeatureChecker signature so it can gate platform-tool
// dispatch. Errors degrade to "disabled" so a transient lookup failure does
// not silently grant access; the underlying error is logged for ops.
func (c *Client) PlatformFeatureCheck(ctx context.Context, organizationID string, feature string) bool {
	enabled, err := c.IsFeatureEnabled(ctx, organizationID, Feature(feature))
	if err != nil {
		c.logger.ErrorContext(ctx, "platform tool feature check failed",
			attr.SlogError(err),
			attr.SlogOrganizationID(organizationID),
			attr.SlogProductFeatureName(feature),
		)
		return false
	}
	return enabled
}

// SetFeatureEnabled writes a generic product-feature flag and refreshes its
// cache entry. Callers must perform authorization and restrict feature names to
// flags without specialized write behavior before calling this method.
func (c *Client) SetFeatureEnabled(ctx context.Context, organizationID string, feature Feature, enabled bool) error {
	if enabled {
		if _, err := c.repo.EnableFeature(ctx, repo.EnableFeatureParams{
			OrganizationID: organizationID,
			FeatureName:    string(feature),
		}); err != nil {
			return fmt.Errorf("enable feature: %w", err)
		}
	} else {
		_, err := c.repo.DeleteFeature(ctx, repo.DeleteFeatureParams{
			OrganizationID: organizationID,
			FeatureName:    string(feature),
		})
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("disable feature: %w", err)
		}
	}

	c.UpdateFeatureCache(ctx, organizationID, feature, enabled)
	return nil
}

// UpdateFeatureCache stores the given enabled state for the feature directly
// into the cache. Call this after writing the feature flag to the database
// from a code path that bypasses this client, so the cache stays consistent.
func (c *Client) UpdateFeatureCache(ctx context.Context, organizationID string, feature Feature, enabled bool) {
	cacheEntry := FeatureCache{
		OrganizationID: organizationID,
		Feature:        feature,
		Enabled:        enabled,
	}
	if err := c.featureCache.Store(ctx, cacheEntry); err != nil {
		c.logger.WarnContext(ctx, "failed to update feature flag cache",
			attr.SlogError(err),
			attr.SlogOrganizationID(organizationID),
			attr.SlogProductFeatureName(string(feature)),
		)
	}
}

func provisionSkillsSystemRoleGrantsTx(ctx context.Context, dbtx repo.DBTX, organizationID string) error {
	if _, err := authz.PatchRoleGrantsTx(ctx, dbtx, organizationID, authz.SystemRoleMember, "", []*authz.RoleGrant{
		{
			Scope:     string(authz.ScopeSkillRead),
			Selectors: nil,
		},
	}, nil); err != nil {
		return fmt.Errorf("provision member Skills grants: %w", err)
	}

	if _, err := authz.PatchRoleGrantsTx(ctx, dbtx, organizationID, authz.SystemRoleAdmin, "", []*authz.RoleGrant{
		{
			Scope:     string(authz.ScopeSkillRead),
			Selectors: nil,
		},
		{
			Scope:     string(authz.ScopeSkillWrite),
			Selectors: nil,
		},
	}, nil); err != nil {
		return fmt.Errorf("provision admin Skills grants: %w", err)
	}

	return nil
}

// SeedOrganizationDefaultsTx enables baseline entitlements for every newly
// provisioned organization. It is intentionally separate from trial seeding so
// an explicit org-admin disable remains durable and absent rows stay disabled.
func SeedOrganizationDefaultsTx(ctx context.Context, tx pgx.Tx, organizationID string) error {
	if _, err := repo.New(tx).EnableFeature(ctx, repo.EnableFeatureParams{
		OrganizationID: organizationID,
		FeatureName:    string(FeaturePlatformMCP),
	}); err != nil {
		return fmt.Errorf("enable default %s entitlement: %w", FeaturePlatformMCP, err)
	}
	return nil
}

// EnterpriseTrialBundle is the entitlement set an enterprise trial organization
// receives at signup. A trial gates only on the time window, so identity (SSO,
// SCIM) is included rather than held back as a conversion lever.
//
// FeatureSkills is absent because Skills is generally available. The bundle
// still calls EnableSkillsTx, which provisions the Skills role grants that the
// entitlement cannot work without. FeatureHooksFailOpen and
// FeatureSkillCaptureMetadataOnly are absent because they change how an
// entitlement behaves rather than granting one.
var EnterpriseTrialBundle = []Feature{
	FeatureLogs,
	FeatureToolIOLogs,
	FeatureSessionCapture,
	FeatureAuthzChallengeLogging,
	FeatureSSO,
	FeatureSCIM,
	FeatureHooksBrowserLogin,
	FeatureCustomModelKeys,
	FeatureAIPlatformPushIntegrations,
	FeatureCustomerManagedEncryptionKeys,
}

// TrialRuntimeFeatures are disabled when an enterprise trial expires and
// restored when the organization returns to a paid or trial state.
var TrialRuntimeFeatures = []Feature{
	FeatureLogs,
	FeatureToolIOLogs,
	FeatureSessionCapture,
	FeaturePlatformMCP,
}

// SetTrialRuntimeFeaturesTx toggles the runtime capabilities whose trial state
// must not outlive an expired trial. It does not modify any other entitlement.
func SetTrialRuntimeFeaturesTx(ctx context.Context, tx pgx.Tx, organizationID string, enabled bool) error {
	q := repo.New(tx)
	for _, feature := range TrialRuntimeFeatures {
		if enabled {
			if _, err := q.EnableFeature(ctx, repo.EnableFeatureParams{
				OrganizationID: organizationID,
				FeatureName:    string(feature),
			}); err != nil {
				return fmt.Errorf("enable %s: %w", feature, err)
			}
			continue
		}

		if _, err := q.DeleteFeature(ctx, repo.DeleteFeatureParams{
			OrganizationID: organizationID,
			FeatureName:    string(feature),
		}); errors.Is(err, pgx.ErrNoRows) {
			continue
		} else if err != nil {
			return fmt.Errorf("disable %s: %w", feature, err)
		}
	}
	return nil
}

// SeedEnterpriseTrialBundleTx enables the enterprise trial entitlements in the
// caller's transaction. Idempotent, so a replayed signup is safe. The feature
// cache is left untouched: the organization is created in the same transaction,
// so no reader can have cached a state for it yet.
func SeedEnterpriseTrialBundleTx(ctx context.Context, tx pgx.Tx, organizationID string) error {
	q := repo.New(tx)

	for _, feature := range EnterpriseTrialBundle {
		if _, err := q.EnableFeature(ctx, repo.EnableFeatureParams{
			OrganizationID: organizationID,
			FeatureName:    string(feature),
		}); err != nil {
			return fmt.Errorf("enable %s for enterprise trial: %w", feature, err)
		}
	}

	if err := EnableSkillsTx(ctx, tx, organizationID); err != nil {
		return fmt.Errorf("enable Skills for enterprise trial: %w", err)
	}

	return nil
}

// SeedPaygEntitlementsTx grants PAYG capabilities only when an organization
// has never configured them. A soft-deleted feature remains disabled. The
// returned features are the rows this transaction inserted, so callers can
// update caches after commit without exposing uncommitted state.
func SeedPaygEntitlementsTx(ctx context.Context, tx pgx.Tx, organizationID string) ([]Feature, error) {
	q := repo.New(tx)
	features := make([]Feature, 0, len(EnterpriseTrialBundle)+2)
	features = append(features, FeaturePlatformMCP)
	features = append(features, EnterpriseTrialBundle...)
	features = append(features, FeatureSkills)

	enabled := make([]Feature, 0, len(features))
	for _, feature := range features {
		inserted, err := q.EnableFeatureIfNeverConfigured(ctx, repo.EnableFeatureIfNeverConfiguredParams{
			OrganizationID: organizationID,
			FeatureName:    string(feature),
		})
		if err != nil {
			return nil, fmt.Errorf("enable new PAYG entitlement %s: %w", feature, err)
		}
		if inserted == 0 {
			continue
		}

		if feature == FeatureSkills {
			if err := provisionSkillsSystemRoleGrantsTx(ctx, tx, organizationID); err != nil {
				return nil, fmt.Errorf("provision PAYG Skills grants: %w", err)
			}
		}
		enabled = append(enabled, feature)
	}

	return enabled, nil
}

// EnableSkillsTx provisions the built-in Skills grants and enables the
// org-level Skills feature in the caller's transaction. Existing grants and
// exclusions are preserved.
func EnableSkillsTx(ctx context.Context, dbtx repo.DBTX, organizationID string) error {
	q := repo.New(dbtx)
	if _, err := q.LockOrganizationMetadata(ctx, organizationID); err != nil {
		return fmt.Errorf("lock organization for Skills enable: %w", err)
	}

	if err := provisionSkillsSystemRoleGrantsTx(ctx, dbtx, organizationID); err != nil {
		return err
	}

	if _, err := q.EnableFeature(ctx, repo.EnableFeatureParams{
		OrganizationID: organizationID,
		FeatureName:    string(FeatureSkills),
	}); err != nil {
		return fmt.Errorf("enable Skills feature flag: %w", err)
	}

	return nil
}
