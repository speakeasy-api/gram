package shadowmcp

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"

	redisCache "github.com/go-redis/cache/v9"

	"github.com/speakeasy-api/gram/server/internal/cache"
)

// ShadowMCPApproval marks a single (project, policy, match) tuple as
// exempt from the shadow-MCP block path. Match is whatever identifier
// the finding surfaced — typically an HTTP/SSE server URL, a local stdio
// command, or (when neither was resolvable) the server-prefix portion of
// the tool name. Callers don't need to distinguish: CanonicalizeMatch
// folds away whitespace, URL casing, and trailing slashes so equivalent
// representations dedupe. Stored in Redis as a temporary allowlist;
// intended to be replaced by a real database-backed table once the
// feature graduates from experimental.
type ShadowMCPApproval struct {
	Match      string    `json:"match"`
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
// of approved matches per policy) makes per-match keys overkill.
func shadowMCPApprovalCacheKey(projectID, policyID string) string {
	return fmt.Sprintf("shadow-mcp-allow:%s:%s", projectID, policyID)
}

// CanonicalizeMatch normalizes a match identifier so lookups don't miss
// due to whitespace, host casing, or trailing slashes. URL-shaped values
// get host/scheme lowercased and trailing slashes trimmed; anything else
// has internal whitespace runs folded to single spaces. Returns the input
// unchanged on parse failure rather than silently dropping the approval.
func CanonicalizeMatch(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if u, err := url.Parse(raw); err == nil && u.Scheme != "" && u.Host != "" {
		u.Host = strings.ToLower(u.Host)
		u.Scheme = strings.ToLower(u.Scheme)
		if u.Path == "/" {
			u.Path = ""
		}
		u.Path = strings.TrimRight(u.Path, "/")
		return u.String()
	}
	return strings.Join(strings.Fields(raw), " ")
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

// IsShadowMCPApproved reports whether any of `candidates` is on the
// allowlist for (project, policy). The hook matcher supplies multiple
// identifiers for the same call — URL, Command, server-prefix — so an
// approval recorded against any one of them allows the call regardless of
// which form the dashboard happened to surface. Empty candidates are
// skipped.
func IsShadowMCPApproved(ctx context.Context, c cache.Cache, projectID, policyID string, candidates ...string) (bool, error) {
	approvals, err := ListShadowMCPApprovals(ctx, c, projectID, policyID)
	if err != nil {
		return false, err
	}
	if len(approvals) == 0 {
		return false, nil
	}
	canon := make([]string, 0, len(candidates))
	for _, c := range candidates {
		if v := CanonicalizeMatch(c); v != "" {
			canon = append(canon, v)
		}
	}
	if len(canon) == 0 {
		return false, nil
	}
	for _, a := range approvals {
		am := CanonicalizeMatch(a.Match)
		if am == "" {
			continue
		}
		if slices.Contains(canon, am) {
			return true, nil
		}
	}
	return false, nil
}

// AddShadowMCPApproval upserts an approval for (project, policy, match).
// Calling twice with the same match is idempotent; the second call
// refreshes the approval metadata (approver, timestamp) and the TTL.
func AddShadowMCPApproval(ctx context.Context, c cache.Cache, projectID, policyID string, approval ShadowMCPApproval) error {
	if c == nil {
		return errors.New("cache is not configured")
	}
	approval.Match = CanonicalizeMatch(approval.Match)
	if approval.Match == "" {
		return errors.New("approval match is empty")
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
		if CanonicalizeMatch(a.Match) == approval.Match {
			continue
		}
		out = append(out, a)
	}
	out = append(out, approval)
	if err := c.Set(ctx, shadowMCPApprovalCacheKey(projectID, policyID), out, approvalCacheTTL); err != nil {
		return fmt.Errorf("store shadow-mcp approvals: %w", err)
	}
	return nil
}

// RemoveShadowMCPApproval drops the approval for (project, policy, match).
// Returns nil when the match was not on the list — revocation is idempotent.
func RemoveShadowMCPApproval(ctx context.Context, c cache.Cache, projectID, policyID, rawMatch string) error {
	if c == nil {
		return errors.New("cache is not configured")
	}
	target := CanonicalizeMatch(rawMatch)
	if target == "" {
		return errors.New("approval match is empty")
	}
	existing, err := ListShadowMCPApprovals(ctx, c, projectID, policyID)
	if err != nil {
		return err
	}
	out := existing[:0:0]
	for _, a := range existing {
		if CanonicalizeMatch(a.Match) == target {
			continue
		}
		out = append(out, a)
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
