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

func provisionMCPApprovalSystemRoleGrantsTx(ctx context.Context, dbtx repo.DBTX, organizationID string) error {
	if _, err := authz.PatchRoleGrantsTx(ctx, dbtx, organizationID, authz.SystemRoleAdmin, "", []*authz.RoleGrant{
		{
			Scope:     string(authz.ScopeMCPApprovalRead),
			Selectors: nil,
		},
		{
			Scope:     string(authz.ScopeMCPApprovalDecide),
			Selectors: nil,
		},
	}, nil); err != nil {
		return fmt.Errorf("provision admin MCP approval grants: %w", err)
	}

	return nil
}

// EnableMCPApprovalTx provisions the built-in MCP approval grants and enables
// the org-level feature in the caller's transaction. Existing grants and
// exclusions are preserved, and the provisioning runs even when the feature is
// already enabled, so re-enabling repairs an organization whose flag predates
// the grants. Members hold no mcp_approval scopes by default: reviewing and
// deciding server access is an admin surface, and the request-access flow
// members use is gated separately.
func EnableMCPApprovalTx(ctx context.Context, dbtx repo.DBTX, organizationID string) error {
	q := repo.New(dbtx)
	if _, err := q.LockOrganizationMetadata(ctx, organizationID); err != nil {
		return fmt.Errorf("lock organization for MCP approval enable: %w", err)
	}

	if err := provisionMCPApprovalSystemRoleGrantsTx(ctx, dbtx, organizationID); err != nil {
		return err
	}

	if _, err := q.EnableFeature(ctx, repo.EnableFeatureParams{
		OrganizationID: organizationID,
		FeatureName:    string(FeatureMCPApproval),
	}); err != nil {
		return fmt.Errorf("enable MCP approval feature flag: %w", err)
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
