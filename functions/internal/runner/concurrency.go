package runner

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/speakeasy-api/gram/functions/internal/attr"
)

const (
	// defaultHoldTimeout is how long an incoming request will wait for a free
	// execution slot before the runner sheds it with a 429. The brief hold
	// keeps the request occupying a slot-wait long enough for the Fly proxy to
	// observe sustained soft-concurrency pressure and trigger autostart, rather
	// than freeing the connection instantly and suppressing scale-out.
	defaultHoldTimeout = 100 * time.Millisecond

	// defaultRetryAfter is advertised in the Retry-After header when the runner
	// sheds a request. Calls are short (sub-second p95), so a couple seconds
	// gives in-flight work time to drain before the client retries.
	defaultRetryAfter = 2 * time.Second
)

// limit bounds the number of concurrently executing tool/resource calls to the
// runner's execution capacity. When all slots are busy it holds the request for
// a short window and, if no slot frees up, returns 429 + Retry-After so the
// caller receives a clean retryable signal instead of a dropped connection.
//
// A maxConcurrency of zero disables limiting (the runner accepts unbounded
// concurrency, matching pre-limiter behavior).
func (s *Service) limit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.maxConcurrency <= 0 {
			next.ServeHTTP(w, r)
			return
		}

		ctx := r.Context()

		if !s.slots.TryAcquire(1) {
			// All slots busy: hold briefly before shedding.
			holdCtx, cancel := context.WithTimeout(ctx, s.holdTimeout)
			defer cancel()

			if err := s.slots.Acquire(holdCtx, 1); err != nil {
				// A canceled request context means the client gave up; only a
				// hold-window timeout (request context still live) is genuine
				// saturation worth shedding with a retryable 429.
				if ctx.Err() != nil {
					return
				}
				s.writeSaturated(w, r)
				return
			}
		}

		s.inFlight.Add(1)
		defer func() {
			s.inFlight.Add(-1)
			s.slots.Release(1)
		}()

		next.ServeHTTP(w, r)
	})
}

// writeSaturated sheds a request with 429 + Retry-After when the runner has no
// free execution slot.
func (s *Service) writeSaturated(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	retryAfterSeconds := max(int(math.Ceil(s.retryAfter.Seconds())), 1)

	s.logger.WarnContext(ctx, "runner saturated, shedding request",
		attr.SlogInFlight(int(s.inFlight.Load())),
		attr.SlogMaxConcurrency(s.maxConcurrency),
		attr.SlogRetryAfter(s.retryAfter),
	)

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Retry-After", strconv.Itoa(retryAfterSeconds))
	w.WriteHeader(http.StatusTooManyRequests)

	body := map[string]any{
		"message":   "runner at capacity, retry later",
		"temporary": true,
	}
	if err := json.NewEncoder(w).Encode(body); err != nil {
		s.logger.ErrorContext(ctx, "failed to write saturation response", attr.SlogError(err))
	}
}
