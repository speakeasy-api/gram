package risk

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"

	redisCache "github.com/go-redis/cache/v9"
	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/cache"
)

// Epic G — "warn"/challenge acknowledgement token (rpak1).
//
// When a `warn` policy matches, enforcement denies the current call and returns
// a link carrying an rpak1 token. The warned user opens it in the dashboard,
// which redeems the token (AcknowledgeRiskPolicyChallenge) — recording the
// acknowledgement in risk_policy_challenges — and then the agent retries, which
// now passes because GetActiveRiskPolicyAck finds a live acknowledged row.
//
// SCOPE: the acknowledgement is keyed on (project, user, policy, tool_name,
// call_fingerprint) with an expiry window, where call_fingerprint is a digest
// of the exact scanned input. So within the window it clears only an IDENTICAL
// retry of the acknowledged call — a different Bash command that trips the same
// policy is challenged again. It is a per-call approval (with a short grace
// window for the retry), not a blanket tool+policy grant.
//
// The token itself only carries a short opaque id (128-bit random bearer
// secret) whose state lives in the cache. The redeem handler re-binds the
// redemption to the session org AND the same user (a leaked link cannot be
// cashed by anyone else), so the token is a one-shot credential to write the
// acknowledgement, NOT the thing the hook checks at retry time (that is the DB).

// errPolicyAckStoreUnavailable distinguishes an unreachable cache (operational
// failure → server error) from a genuine miss (expired/invalid link → client error).
var errPolicyAckStoreUnavailable = errors.New("risk policy ack store unavailable")

const (
	// policyAckTokenPrefix is the rpak1 token format: prefix + opaque cache id.
	policyAckTokenPrefix = "rpak1." // #nosec G101 -- token prefix label, not a credential.
	// policyAckCacheKeyPrefix namespaces rpak1 state in the shared cache.
	policyAckCacheKeyPrefix = "risk:policy-ack:" // #nosec G101 -- cache key namespace, not a credential.
	// defaultPolicyAckTTL is how long a challenge link stays redeemable.
	defaultPolicyAckTTL = 10 * time.Minute
	// maxPolicyAckTTL bounds the link lifetime. The record may carry a
	// ChallengeMessage with secret-adjacent matched values, so its ephemeral
	// (~10 min) window is enforced, not just documented — a caller cannot ask
	// for a longer-lived record.
	maxPolicyAckTTL = 10 * time.Minute
)

// PolicyAckTokenInput is the state a challenge link points at. Deliberately
// carries the log-safe identity needed to record the ack, plus ChallengeMessage.
//
// ChallengeMessage is the human-facing warning shown on the approval page (the
// same text rendered to the operator in the terminal). It MAY contain the
// matched value: this is an ephemeral, token-gated cache record (~10 min TTL) —
// the same scoped exposure as the terminal display, NOT durable persistence. It
// must never be copied into ClickHouse, tool_call_blocks, or audit.
type PolicyAckTokenInput struct {
	OrganizationID string
	ProjectID      string
	UserID         string
	RiskPolicyID   string
	PolicyName     string
	ToolName       *string
	// CallFingerprint scopes the acknowledgement to the concrete call that was
	// challenged (SHA-256 of the scanned input). Redeeming writes it onto the
	// challenge row so only an identical retry — not any same-tool call — clears.
	CallFingerprint  string
	ChallengeMessage string
	// RememberFor is how long the acknowledgement, once granted, suppresses
	// re-challenging that same call. Zero uses the ack window default.
	RememberFor time.Duration
}

// policyAckRecord is the cached server-side state for an rpak1 token.
type policyAckRecord struct {
	OrganizationID string  `json:"organization_id"`
	ProjectID      string  `json:"project_id"`
	UserID         string  `json:"user_id"`
	RiskPolicyID   string  `json:"risk_policy_id"`
	PolicyName     string  `json:"policy_name,omitempty"`
	ToolName       *string `json:"tool_name,omitempty"`
	// CallFingerprint — see PolicyAckTokenInput.
	CallFingerprint string `json:"call_fingerprint,omitempty"`
	// ChallengeMessage — see PolicyAckTokenInput. Ephemeral, token-gated only.
	ChallengeMessage string        `json:"challenge_message,omitempty"`
	RememberFor      time.Duration `json:"remember_for,omitempty"`
	ExpiresAt        time.Time     `json:"expires_at"`
}

func policyAckCacheKey(id string) string {
	return policyAckCacheKeyPrefix + id
}

// newPolicyAckID returns a 128-bit random, URL-safe id used as both the cache
// key and the unguessable bearer secret embedded in the link.
func newPolicyAckID() (string, error) {
	raw := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, raw); err != nil {
		return "", fmt.Errorf("generate risk policy ack id: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// GeneratePolicyAckURL stores the challenge state and returns the dashboard URL
// that redeems it (token in the fragment, never the query — same rationale as
// GeneratePolicyBypassRequestURL) plus the link expiry.
func GeneratePolicyAckURL(ctx context.Context, c cache.Cache, siteURL *url.URL, input PolicyAckTokenInput, ttl time.Duration) (string, time.Time, error) {
	if siteURL == nil {
		return "", time.Time{}, fmt.Errorf("site url is required")
	}
	token, expiry, err := GeneratePolicyAckToken(ctx, c, input, ttl)
	if err != nil {
		return "", time.Time{}, err
	}
	ackURL := siteURL.JoinPath("risk-policy-challenge", "acknowledge")
	query := url.Values{}
	query.Set("ack_token", token)
	ackURL.Fragment = query.Encode()
	return ackURL.String(), expiry, nil
}

// GeneratePolicyAckToken stores the challenge state in the cache and returns the
// rpak1 token (prefix + cache id) plus its expiry.
func GeneratePolicyAckToken(ctx context.Context, c cache.Cache, input PolicyAckTokenInput, ttl time.Duration) (string, time.Time, error) {
	if ttl <= 0 {
		ttl = defaultPolicyAckTTL
	}
	if ttl > maxPolicyAckTTL {
		ttl = maxPolicyAckTTL
	}
	now := time.Now()
	expiry := now.Add(ttl).Truncate(time.Second)
	record := policyAckRecord{
		OrganizationID:   strings.TrimSpace(input.OrganizationID),
		ProjectID:        strings.TrimSpace(input.ProjectID),
		UserID:           strings.TrimSpace(input.UserID),
		RiskPolicyID:     strings.TrimSpace(input.RiskPolicyID),
		PolicyName:       strings.TrimSpace(input.PolicyName),
		ToolName:         normalizeOptionalString(input.ToolName),
		CallFingerprint:  strings.TrimSpace(input.CallFingerprint),
		ChallengeMessage: strings.TrimSpace(input.ChallengeMessage),
		RememberFor:      input.RememberFor,
		ExpiresAt:        expiry,
	}
	if err := validatePolicyAckFields(record.OrganizationID, record.ProjectID, record.UserID, record.RiskPolicyID); err != nil {
		return "", time.Time{}, err
	}
	if c == nil {
		return "", time.Time{}, fmt.Errorf("risk policy ack cache is not configured")
	}
	id, err := newPolicyAckID()
	if err != nil {
		return "", time.Time{}, err
	}
	if err := c.Set(ctx, policyAckCacheKey(id), record, ttl); err != nil {
		return "", time.Time{}, fmt.Errorf("store risk policy ack: %w", err)
	}
	return policyAckTokenPrefix + id, expiry, nil
}

// lookupPolicyAckRecord resolves an rpak1 token to its cached state. A cache
// miss is a client error (expired/invalid); any other error wraps
// errPolicyAckStoreUnavailable so callers can return a server error.
func lookupPolicyAckRecord(ctx context.Context, c cache.Cache, tokenString string) (*policyAckRecord, error) {
	if c == nil {
		return nil, fmt.Errorf("%w: cache is not configured", errPolicyAckStoreUnavailable)
	}
	if !strings.HasPrefix(tokenString, policyAckTokenPrefix) {
		return nil, fmt.Errorf("invalid risk policy ack token format")
	}
	id := strings.TrimPrefix(tokenString, policyAckTokenPrefix)
	if strings.TrimSpace(id) == "" {
		return nil, fmt.Errorf("invalid risk policy ack token format")
	}
	var record policyAckRecord
	if err := c.Get(ctx, policyAckCacheKey(id), &record); err != nil {
		if errors.Is(err, redisCache.ErrCacheMiss) {
			return nil, fmt.Errorf("risk policy ack not found or expired: %w", err)
		}
		return nil, fmt.Errorf("%w: %w", errPolicyAckStoreUnavailable, err)
	}
	if time.Now().After(record.ExpiresAt) {
		return nil, fmt.Errorf("risk policy ack expired")
	}
	if err := validatePolicyAckFields(record.OrganizationID, record.ProjectID, record.UserID, record.RiskPolicyID); err != nil {
		return nil, err
	}
	return &record, nil
}

// invalidatePolicyAckToken best-effort deletes a redeemed token so the one-shot
// link cannot be replayed. Errors are non-fatal (TTL still bounds it).
func invalidatePolicyAckToken(ctx context.Context, c cache.Cache, tokenString string) {
	if c == nil || !strings.HasPrefix(tokenString, policyAckTokenPrefix) {
		return
	}
	id := strings.TrimPrefix(tokenString, policyAckTokenPrefix)
	// Eviction runs after the DB commit, so it must not be skipped if the
	// request context is already cancelled (client disconnect / timeout).
	// Otherwise a declined-but-not-evicted token could later be redeemed. Detach
	// from the request context with a bounded timeout so the delete still runs.
	evictCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	_ = c.Delete(evictCtx, policyAckCacheKey(id))
}

func validatePolicyAckFields(orgID, projectID, userID, policyID string) error {
	if strings.TrimSpace(orgID) == "" {
		return fmt.Errorf("organization_id is required")
	}
	if strings.TrimSpace(userID) == "" {
		return fmt.Errorf("user_id is required")
	}
	if _, err := uuid.Parse(projectID); err != nil {
		return fmt.Errorf("invalid project_id: %w", err)
	}
	if _, err := uuid.Parse(policyID); err != nil {
		return fmt.Errorf("invalid risk_policy_id: %w", err)
	}
	return nil
}
