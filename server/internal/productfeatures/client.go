package productfeatures

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

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
	db           *pgxpool.Pool
	repo         *repo.Queries
	featureCache cache.TypedCacheObject[FeatureCache]
}

func NewClient(logger *slog.Logger, tracerProvider trace.TracerProvider, db *pgxpool.Pool, redisClient *redis.Client) *Client {
	logger = logger.With(attr.SlogComponent("productfeatures"))

	return &Client{
		tracer:       tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/productfeatures"),
		logger:       logger,
		db:           db,
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

	var enabled bool
	err := c.withFeatureCacheLock(ctx, organizationID, feature, func(conn *pgxpool.Conn) error {
		// A concurrent writer or cache fill may have populated the entry while
		// this request waited for the lock.
		if cached, cacheErr := c.featureCache.Get(ctx, FeatureCacheKey(organizationID, feature)); cacheErr == nil {
			enabled = cached.Enabled
			return nil
		}

		res, queryErr := repo.New(conn).IsFeatureEnabled(ctx, repo.IsFeatureEnabledParams{
			OrganizationID: organizationID,
			FeatureName:    string(feature),
		})
		switch {
		case errors.Is(queryErr, context.Canceled):
			// Do not cache results if the context was canceled, as this likely
			// indicates a timeout or shutdown in progress.
			enabled = false
			return nil
		case errors.Is(queryErr, pgx.ErrNoRows):
			res = false
		case queryErr != nil:
			return oops.E(
				oops.CodeUnexpected, queryErr,
				"failed to get organization feature flag %q", string(feature),
			).LogError(ctx, c.logger, attr.SlogOrganizationID(organizationID))
		}

		enabled = res
		_ = c.storeFeatureCache(ctx, organizationID, feature, enabled, "failed to cache feature flag state")
		return nil
	})
	if err != nil {
		return false, err
	}
	return enabled, nil
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
	return c.withFeatureCacheLock(ctx, organizationID, feature, func(conn *pgxpool.Conn) error {
		if err := setFeatureEnabled(ctx, repo.New(conn), organizationID, feature, enabled); err != nil {
			return err
		}
		_ = c.storeFeatureCache(ctx, organizationID, feature, enabled, "failed to update feature flag cache")
		return nil
	})
}

// SetRemoteSessionAutoRefreshEnabled maps the standalone admin's binary control
// to the existing tri-state policy. Either choice clears the enforced state so
// the displayed value and runtime behavior cannot disagree.
func (c *Client) SetRemoteSessionAutoRefreshEnabled(ctx context.Context, organizationID string, enabled bool) error {
	return c.withFeatureCacheLocks(ctx, organizationID, []Feature{
		FeatureRemoteSessionAutoRefresh, FeatureRemoteSessionAutoRefreshEnforced,
	}, func(conn *pgxpool.Conn) error {
		tx, err := conn.Begin(ctx)
		if err != nil {
			return fmt.Errorf("begin remote session auto-refresh update: %w", err)
		}
		defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()

		queries := repo.New(tx)
		if err := setFeatureEnabled(ctx, queries, organizationID, FeatureRemoteSessionAutoRefreshEnforced, false); err != nil {
			return err
		}
		if err := setFeatureEnabled(ctx, queries, organizationID, FeatureRemoteSessionAutoRefresh, enabled); err != nil {
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit remote session auto-refresh update: %w", err)
		}

		_ = c.storeFeatureCache(ctx, organizationID, FeatureRemoteSessionAutoRefreshEnforced, false, "failed to update feature flag cache")
		_ = c.storeFeatureCache(ctx, organizationID, FeatureRemoteSessionAutoRefresh, enabled, "failed to update feature flag cache")
		return nil
	})
}

// UpdateFeatureCache reloads the durable feature state and refreshes the cache
// under the same lock used by cache fills and writes. Call this after writing
// the feature flag from a code path that bypasses this client.
func (c *Client) UpdateFeatureCache(ctx context.Context, organizationID string, feature Feature, _ bool) {
	if err := c.withFeatureCacheLock(ctx, organizationID, feature, func(conn *pgxpool.Conn) error {
		return c.UpdateFeatureCacheUnderLock(ctx, conn, organizationID, feature)
	}); err != nil {
		c.logger.WarnContext(ctx, "failed to refresh feature flag cache",
			attr.SlogError(err), attr.SlogOrganizationID(organizationID), attr.SlogProductFeatureName(string(feature)),
		)
	}
}

// UpdateFeatureCacheUnderLock refreshes one cache entry using the connection
// that holds its feature lock. Callers must already hold that lock.
func (c *Client) UpdateFeatureCacheUnderLock(ctx context.Context, conn *pgxpool.Conn, organizationID string, feature Feature) error {
	enabled, err := repo.New(conn).IsFeatureEnabled(ctx, repo.IsFeatureEnabledParams{
		OrganizationID: organizationID, FeatureName: string(feature),
	})
	if err != nil {
		return fmt.Errorf("reload feature cache state: %w", err)
	}
	if err := c.storeFeatureCache(ctx, organizationID, feature, enabled, "failed to update feature flag cache"); err != nil {
		return fmt.Errorf("store feature cache state: %w", err)
	}
	return nil
}

func (c *Client) storeFeatureCache(ctx context.Context, organizationID string, feature Feature, enabled bool, message string) error {
	cacheEntry := FeatureCache{
		OrganizationID: organizationID,
		Feature:        feature,
		Enabled:        enabled,
	}
	if err := c.featureCache.Store(ctx, cacheEntry); err != nil {
		c.logger.WarnContext(ctx, message,
			attr.SlogError(err),
			attr.SlogOrganizationID(organizationID),
			attr.SlogProductFeatureName(string(feature)),
		)
		return fmt.Errorf("store feature cache entry: %w", err)
	}
	return nil
}

func setFeatureEnabled(ctx context.Context, queries *repo.Queries, organizationID string, feature Feature, enabled bool) error {
	if enabled {
		if _, err := queries.EnableFeature(ctx, repo.EnableFeatureParams{OrganizationID: organizationID, FeatureName: string(feature)}); err != nil {
			return fmt.Errorf("enable feature: %w", err)
		}
		return nil
	}

	_, err := queries.DeleteFeature(ctx, repo.DeleteFeatureParams{OrganizationID: organizationID, FeatureName: string(feature)})
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("disable feature: %w", err)
	}
	return nil
}

func (c *Client) withFeatureCacheLock(ctx context.Context, organizationID string, feature Feature, fn func(*pgxpool.Conn) error) error {
	return c.withFeatureCacheLocks(ctx, organizationID, []Feature{feature}, fn)
}

func (c *Client) withFeatureCacheLocks(ctx context.Context, organizationID string, features []Feature, fn func(*pgxpool.Conn) error) error {
	conn, release, err := c.acquireFeatureCacheLocks(ctx, organizationID, features)
	if err != nil {
		return err
	}
	defer release()
	return fn(conn)
}

// AcquireFeatureCacheLocks acquires the same canonical, sorted advisory locks
// used by feature mutations. The caller must begin its transaction on the
// returned connection and hold the locks through commit and cache refresh.
func (c *Client) AcquireFeatureCacheLocks(ctx context.Context, organizationID string, features []Feature) (*pgxpool.Conn, func(), error) {
	return c.acquireFeatureCacheLocks(ctx, organizationID, features)
}

func (c *Client) acquireFeatureCacheLocks(ctx context.Context, organizationID string, features []Feature) (*pgxpool.Conn, func(), error) {
	conn, err := c.db.Acquire(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("acquire feature cache lock connection: %w", err)
	}

	queries := repo.New(conn)
	features = slices.Clone(features)
	slices.Sort(features)
	acquired := make([]repo.AcquireFeatureCacheLockParams, 0, len(features))
	release := func() {
		unlockCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		for _, params := range slices.Backward(acquired) {

			unlocked, unlockErr := queries.ReleaseFeatureCacheLock(unlockCtx, repo.ReleaseFeatureCacheLockParams(params))
			if unlockErr != nil || !unlocked {
				c.logger.ErrorContext(unlockCtx, "failed to release feature cache lock",
					attr.SlogError(unlockErr),
					attr.SlogOrganizationID(organizationID),
					attr.SlogProductFeatureName(params.FeatureName),
				)
				_ = conn.Hijack().Close(unlockCtx)
				return
			}
		}
		conn.Release()
	}

	for _, feature := range features {
		params := repo.AcquireFeatureCacheLockParams{OrganizationID: organizationID, FeatureName: string(feature)}
		if err := queries.AcquireFeatureCacheLock(ctx, params); err != nil {
			release()
			return nil, nil, fmt.Errorf("acquire feature cache lock: %w", err)
		}
		acquired = append(acquired, params)
	}

	return conn, release, nil
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

// EnterpriseAccessBundle is the entitlement set an enterprise-level
// organization receives at signup or paid-tier activation. A trial gates only
// on the time window, so identity (SSO, SCIM) is included rather than held back
// as a conversion lever.
//
// FeatureSkills is absent because Skills is generally available. The trial and
// paid-tier seeders separately provision the Skills role grants that access
// requires. FeatureHooksFailOpen and
// FeatureSkillCaptureMetadataOnly are absent because they change how an
// entitlement behaves rather than granting one.
var EnterpriseAccessBundle = []Feature{
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

	for _, feature := range EnterpriseAccessBundle {
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

// SeedEnterpriseAccessEntitlementsTx grants enterprise-level capabilities only
// when an organization has never configured them. A soft-deleted feature remains
// disabled. The returned features are the rows this transaction inserted, so callers can
// update caches after commit without exposing uncommitted state.
func SeedEnterpriseAccessEntitlementsTx(ctx context.Context, tx pgx.Tx, organizationID string) ([]Feature, error) {
	q := repo.New(tx)
	features := make([]Feature, 0, len(EnterpriseAccessBundle)+2)
	features = append(features, FeaturePlatformMCP)
	features = append(features, EnterpriseAccessBundle...)
	features = append(features, FeatureSkills)

	enabled := make([]Feature, 0, len(features))
	for _, feature := range features {
		inserted, err := q.EnableFeatureIfNeverConfigured(ctx, repo.EnableFeatureIfNeverConfiguredParams{
			OrganizationID: organizationID,
			FeatureName:    string(feature),
		})
		if err != nil {
			return nil, fmt.Errorf("enable new enterprise-access entitlement %s: %w", feature, err)
		}
		if inserted == 0 {
			continue
		}

		if feature == FeatureSkills {
			if err := provisionSkillsSystemRoleGrantsTx(ctx, tx, organizationID); err != nil {
				return nil, fmt.Errorf("provision enterprise-access Skills grants: %w", err)
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
