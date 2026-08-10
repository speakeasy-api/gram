package openrouter

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/ratelimit"
)

const (
	// judgeRateLimiterName namespaces the shared judge bucket. All judges
	// (riskjudge, pijudge, chat analysis, skill efficacy, skill suggestions)
	// share one limiter — same name, keyed by JudgeRateLimitKey — so their
	// combined call rate is what gets capped, not each judge in isolation.
	judgeRateLimiterName = "openrouter-judge"
	// judgeRatePerMinute and judgeRateBurst keep the rolling-minute peak
	// (rate+burst) at the 300 requests-per-minute ceiling OpenRouter applies
	// per (account, model) to shared-capacity models — the binding constraint
	// for platform traffic, observed on gemini flash — and per (key, model)
	// otherwise. Enforced through the Store, so this is the fleet-wide cap,
	// not the per-replica backstop the in-memory limiters could only manage.
	judgeRatePerMinute = 250
	judgeRateBurst     = 50
)

// PlatformKey is the ResolvedKey a judge bucket treats as platform-provisioned:
// every platform key spends the one platform OpenRouter account, so the key
// material itself does not matter for bucketing, only that Customer is false.
func PlatformKey() ResolvedKey {
	return ResolvedKey{Key: "", Customer: false}
}

// NewJudgeRateLimiter returns the shared rate limiter guarding billable LLM
// judge calls. Pass it to every judge; each keys with JudgeRateLimitKey so
// calls that spend the same OpenRouter capacity draw from one bucket. Build it
// from a Redis Store in production so the cap holds across replicas; a memory
// Store suffices for tests.
func NewJudgeRateLimiter(store ratelimit.Store) *ratelimit.Limiter {
	return ratelimit.New(store, judgeRateLimiterName, ratelimit.PerMinute(judgeRatePerMinute).WithBurst(judgeRateBurst))
}

// JudgeRateLimitKey is the bucket key for a judge call, scoped to the
// OpenRouter capacity the resolved key spends. Every platform-provisioned key
// belongs to the one platform OpenRouter account, and OpenRouter's
// shared-capacity ceilings apply per (account, model) across all of that
// account's keys — so platform-key calls bucket per model, org-independent. A
// customer (BYOK) key spends the customer's own account, where OpenRouter
// accounts per (key, model), and one key may serve any number of orgs and
// projects — so those calls bucket per (key, model). The key is hashed: a
// limiter bucket name must not carry the secret itself. An empty customer key
// falls back to the platform bucket rather than pooling all such calls under
// one hash.
func JudgeRateLimitKey(key ResolvedKey, model string) string {
	if key.Customer && key.Key != "" {
		sum := sha256.Sum256([]byte(key.Key))
		return "key:" + hex.EncodeToString(sum[:8]) + ":model:" + model
	}
	return "model:" + model
}

// ResolveJudgeRateLimitKey resolves the key a platform-initiated judge call
// would spend and returns its rate-limit bucket. A resolution failure is not a
// throttle: it falls back to the platform-scoped bucket — still limited —
// rather than skipping the limiter, and the completion call surfaces the
// resolution error itself if it persists.
func ResolveJudgeRateLimitKey(ctx context.Context, logger *slog.Logger, resolver KeyResolver, orgID string, projectID string, slot billing.ModelUsageSource, model string) string {
	// The completion path normalizes the model through ResolveModel, so bucket
	// under the model that is actually called — otherwise de-listed request ids
	// would each get their own bucket while spending one model's capacity.
	if normalized := ResolveModel(model); normalized != "" {
		model = normalized
	}
	resolved, err := resolver.ResolveKey(ctx, orgID, projectID, slot, KeyTypeInternal)
	if err != nil {
		logger.WarnContext(ctx, "judge key resolution failed, scoping rate limit to platform bucket",
			attr.SlogError(err),
			attr.SlogOrganizationID(orgID),
		)
		resolved = PlatformKey()
	}
	return JudgeRateLimitKey(resolved, model)
}
