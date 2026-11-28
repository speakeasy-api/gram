package auth

import (
	"context"
	"errors"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/ratelimit"
)

// OrgCreationLimitName namespaces the per-user self-serve organization
// creation rate limiter in Redis and in ratelimit.decisions metrics.
const OrgCreationLimitName = "self-serve-org-create"

// OrgCreationRate bounds how often one user may create self-serve
// organizations across all self-serve entry points. A normal user creates a
// single organization on signup; 3 per hour with a burst of 3 leaves room for
// legitimate retries while blocking rapid bulk creation.
var OrgCreationRate = ratelimit.PerHour(3)

// ErrOrgCreationRateLimited refuses self-serve organization creation when a
// user has exceeded the per-user creation rate limit.
var ErrOrgCreationRateLimited = errors.New("organization creation rate limit exceeded")

const orgCreationRateLimitedMessage = "Too many organization creation attempts. Please try again later."

// WithOrgCreationLimiter sets the per-user rate limiter for self-serve
// organization creation. Nil leaves organization creation unthrottled.
func (s *Service) WithOrgCreationLimiter(limiter *ratelimit.Limiter) *Service {
	s.orgCreationLimiter = limiter
	return s
}

// allowOrgCreation enforces the per-user self-serve organization creation rate
// limit. A limiter store outage fails open and logs a warning so a transient
// Redis blip does not lock out new signups; the database organization cap still
// enforces the durable per-user limit.
func (s *Service) allowOrgCreation(ctx context.Context, userID, creationSource string) error {
	if s.orgCreationLimiter == nil {
		return nil
	}
	res, err := s.orgCreationLimiter.Allow(ctx, userID)
	switch {
	case err != nil:
		s.logger.WarnContext(ctx, "organization creation rate limiter unavailable; allowing",
			attr.SlogError(err),
			attr.SlogUserID(userID),
			attr.SlogReason(creationSource),
		)
		return nil
	case !res.Allowed:
		s.logger.WarnContext(ctx, "self-serve organization creation rate limited",
			attr.SlogUserID(userID),
			attr.SlogReason(creationSource),
			attr.SlogRetryWait(res.RetryAfter),
		)
		return ErrOrgCreationRateLimited
	default:
		return nil
	}
}
