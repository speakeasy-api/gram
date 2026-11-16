package workloadpolicy

import "github.com/speakeasy-api/gram/server/internal/ratelimit"

// SetRegisterLimitersForTest swaps the enrollment limiters so a test can
// observe which buckets a request consumes.
func SetRegisterLimitersForTest(s *Service, fleet, token *ratelimit.Limiter) {
	s.registerLimiter, s.registerTokenLimiter = fleet, token
}
