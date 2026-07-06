package openrouter

import (
	"github.com/speakeasy-api/gram/server/internal/ratelimit"
)

const (
	// judgeRateLimiterName namespaces the shared judge bucket. riskjudge and
	// pijudge both spend the same per-org OpenRouter key on the same model, and
	// OpenRouter enforces its requests-per-minute ceiling per (key, model). They
	// therefore share one limiter — same name, keyed by org+model — so their
	// combined call rate is what gets capped, not each judge in isolation.
	judgeRateLimiterName = "openrouter-judge"
	// judgeRatePerMinute and judgeRateBurst keep the rolling-minute peak
	// (rate+burst) at OpenRouter's per-(key, model) ceiling with margin below it.
	// Enforced through the Store, so this is the fleet-wide cap, not the
	// per-replica backstop the in-memory limiters could only manage.
	judgeRatePerMinute = 250
	judgeRateBurst     = 50
)

// NewJudgeRateLimiter returns the shared rate limiter guarding billable LLM
// judge calls. Pass it to every judge (riskjudge, pijudge); each keys with
// JudgeRateLimitKey so calls for the same org and model draw from one bucket.
// Build it from a Redis Store in production so the cap holds across replicas; a
// memory Store suffices for tests.
func NewJudgeRateLimiter(store ratelimit.Store) *ratelimit.Limiter {
	return ratelimit.New(store, judgeRateLimiterName, ratelimit.PerMinute(judgeRatePerMinute).WithBurst(judgeRateBurst))
}

// JudgeRateLimitKey is the bucket key for a judge call: per organization and
// model, matching OpenRouter's per-(key, model) RPM accounting. Both judge
// packages format the key through this function so calls for the same org and
// model land in the same bucket.
func JudgeRateLimitKey(orgID, model string) string {
	return orgID + ":" + model
}
