package shadowmcp

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	redisCache "github.com/go-redis/cache/v9"

	"github.com/speakeasy-api/gram/server/internal/cache"
)

// ShadowMCPApproval marks a single (project, policy, URL) tuple as exempt
// from the shadow-MCP block path. Stored in Redis as a temporary
// allowlist; intended to be replaced by a real database-backed table once
// the feature graduates from experimental.
type ShadowMCPApproval struct {
	URL        string    `json:"url"`
	ServerName string    `json:"server_name,omitempty"`
	ApprovedBy string    `json:"approved_by,omitempty"`
	ApprovedAt time.Time `json:"approved_at"`
}

// approvalCacheTTL is intentionally long — these aren't ephemeral
// cache entries, they're persistence with a safety valve. Each write
// refreshes the TTL so an active allowlist effectively survives until
// the user removes the entry or Redis is wiped.
const approvalCacheTTL = 90 * 24 * time.Hour

// shadowMCPApprovalCacheKey is the Redis key for the approval list of a
// given (project, policy) pair. Storing as a single JSON list keeps
// reads and writes to one Redis round-trip; the expected size (a handful
// of approved URLs per policy) makes per-URL keys overkill.
func shadowMCPApprovalCacheKey(projectID, policyID string) string {
	return fmt.Sprintf("shadow-mcp-allow:%s:%s", projectID, policyID)
}

// CanonicalizeApprovalURL normalizes a URL so lookups don't miss due to
// trailing slashes, casing on host, or default ports. Returns the input
// unchanged when it doesn't parse — the cache then stores whatever the
// caller passed, which is preferable to silently dropping the approval.
func CanonicalizeApprovalURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return raw
	}
	u.Host = strings.ToLower(u.Host)
	u.Scheme = strings.ToLower(u.Scheme)
	if u.Path == "/" {
		u.Path = ""
	}
	u.Path = strings.TrimRight(u.Path, "/")
	return u.String()
}

// ListShadowMCPApprovals returns all approvals stored for (project, policy).
// Returns an empty slice (not an error) when no approvals exist.
func ListShadowMCPApprovals(ctx context.Context, c cache.Cache, projectID, policyID string) ([]ShadowMCPApproval, error) {
	if c == nil {
		return nil, errors.New("cache is not configured")
	}
	var approvals []ShadowMCPApproval
	err := c.Get(ctx, shadowMCPApprovalCacheKey(projectID, policyID), &approvals)
	if err != nil {
		// Treat a missing key as no approvals — the cache layer wraps
		// "not found" as an error, but callers want a clean empty slice.
		if isCacheMiss(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("get shadow-mcp approvals: %w", err)
	}
	return approvals, nil
}

// IsShadowMCPURLApproved reports whether the given URL is on the allowlist
// for (project, policy). URL comparison is canonicalized so equivalent
// representations (trailing slashes, host casing) match.
func IsShadowMCPURLApproved(ctx context.Context, c cache.Cache, projectID, policyID, rawURL string) (bool, error) {
	approvals, err := ListShadowMCPApprovals(ctx, c, projectID, policyID)
	if err != nil {
		return false, err
	}
	target := CanonicalizeApprovalURL(rawURL)
	if target == "" {
		return false, nil
	}
	for _, a := range approvals {
		if CanonicalizeApprovalURL(a.URL) == target {
			return true, nil
		}
	}
	return false, nil
}

// AddShadowMCPApproval upserts an approval for (project, policy, URL).
// Calling twice with the same URL is idempotent; the second call refreshes
// the approval metadata (approver, timestamp) and the TTL.
func AddShadowMCPApproval(ctx context.Context, c cache.Cache, projectID, policyID string, approval ShadowMCPApproval) error {
	if c == nil {
		return errors.New("cache is not configured")
	}
	approval.URL = CanonicalizeApprovalURL(approval.URL)
	if approval.URL == "" {
		return errors.New("approval URL is empty")
	}
	if approval.ApprovedAt.IsZero() {
		approval.ApprovedAt = time.Now().UTC()
	}
	existing, err := ListShadowMCPApprovals(ctx, c, projectID, policyID)
	if err != nil {
		return err
	}
	out := existing[:0:0]
	for _, a := range existing {
		if CanonicalizeApprovalURL(a.URL) != approval.URL {
			out = append(out, a)
		}
	}
	out = append(out, approval)
	if err := c.Set(ctx, shadowMCPApprovalCacheKey(projectID, policyID), out, approvalCacheTTL); err != nil {
		return fmt.Errorf("store shadow-mcp approvals: %w", err)
	}
	return nil
}

// RemoveShadowMCPApproval drops the approval for (project, policy, URL).
// Returns nil when the URL was not on the list — revocation is idempotent.
func RemoveShadowMCPApproval(ctx context.Context, c cache.Cache, projectID, policyID, rawURL string) error {
	if c == nil {
		return errors.New("cache is not configured")
	}
	target := CanonicalizeApprovalURL(rawURL)
	if target == "" {
		return errors.New("approval URL is empty")
	}
	existing, err := ListShadowMCPApprovals(ctx, c, projectID, policyID)
	if err != nil {
		return err
	}
	out := existing[:0:0]
	for _, a := range existing {
		if CanonicalizeApprovalURL(a.URL) != target {
			out = append(out, a)
		}
	}
	if len(out) == len(existing) {
		return nil
	}
	if len(out) == 0 {
		if err := c.Delete(ctx, shadowMCPApprovalCacheKey(projectID, policyID)); err != nil {
			return fmt.Errorf("delete shadow-mcp approvals: %w", err)
		}
		return nil
	}
	if err := c.Set(ctx, shadowMCPApprovalCacheKey(projectID, policyID), out, approvalCacheTTL); err != nil {
		return fmt.Errorf("store shadow-mcp approvals: %w", err)
	}
	return nil
}

// isCacheMiss distinguishes "no approvals yet" from a real Redis failure
// so a freshly-asked policy returns an empty list rather than an error.
func isCacheMiss(err error) bool {
	return errors.Is(err, redisCache.ErrCacheMiss)
}
