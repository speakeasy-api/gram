package middleware

import (
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/authz"
)

// undecidedLogInterval bounds how often one route is reported, so a busy route
// that never asks for authorization logs a few lines an hour, not one per
// request.
const undecidedLogInterval = 10 * time.Minute

// UndecidedRequests reports authenticated requests that finish without asking
// the authz engine anything, and requests whose every decision was skipped
// because RBAC is not enforced for the caller. It changes no response.
//
// This is detection for the RBAC fail-closed work: a handler that never calls
// the engine is allowed today by omission, and so is every request a legacy
// API key or a session-less caller makes. The log lines name the route and
// the skip reason, never the caller, the resource or any grant.
func UndecidedRequests(logger *slog.Logger) func(http.Handler) http.Handler {
	reporter := &undecidedReporter{
		logger:   logger.With(attr.SlogComponent("authz_coverage")),
		interval: undecidedLogInterval,
		now:      time.Now,
		mu:       sync.Mutex{},
		last:     make(map[undecidedKey]time.Time),
		count:    make(map[undecidedKey]int),
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := authz.WithDecisionRecorder(r.Context())
			next.ServeHTTP(w, r.WithContext(ctx))
			summary, ok := authz.RecordedDecisions(ctx)
			if !ok {
				return
			}
			reporter.observe(r, summary)
		})
	}
}

type undecidedKind string

const (
	// undecidedKindNone is a prepared request with no decision at all.
	undecidedKindNone undecidedKind = "no_decision"
	// undecidedKindSkipped is a request whose every decision was skipped.
	undecidedKindSkipped undecidedKind = "skipped_only"
)

type undecidedKey struct {
	kind   undecidedKind
	route  string
	reason authz.SkipReason
}

type undecidedReporter struct {
	logger   *slog.Logger
	interval time.Duration
	now      func() time.Time

	mu    sync.Mutex
	last  map[undecidedKey]time.Time
	count map[undecidedKey]int
}

func (u *undecidedReporter) observe(r *http.Request, summary authz.DecisionSummary) {
	route := undecidedRoute(r)
	switch {
	case summary.Prepared && summary.Total() == 0:
		u.report(r, undecidedKey{kind: undecidedKindNone, route: route, reason: ""})
	case summary.Enforced == 0 && summary.Skipped > 0:
		for reason := range summary.SkipReasons {
			u.report(r, undecidedKey{kind: undecidedKindSkipped, route: route, reason: reason})
		}
	}
}

func (u *undecidedReporter) report(r *http.Request, key undecidedKey) {
	u.mu.Lock()
	u.count[key]++
	now := u.now()
	last, seen := u.last[key]
	if seen && now.Sub(last) < u.interval {
		u.mu.Unlock()
		return
	}
	count := u.count[key]
	u.count[key] = 0
	u.last[key] = now
	u.mu.Unlock()

	message := "authenticated request made no authorization decision"
	if key.kind == undecidedKindSkipped {
		message = "request authorized only by skipped RBAC evaluation"
	}
	u.logger.InfoContext(r.Context(), message,
		attr.SlogHTTPRequestMethod(r.Method),
		attr.SlogHTTPRoute(key.route),
		attr.SlogAuthzUndecidedKind(string(key.kind)),
		attr.SlogAuthzSkipReason(string(key.reason)),
		attr.SlogAuthzUndecidedCount(count),
	)
}

// undecidedRoute names a request's route without identifiers. Management API
// paths are fixed (/rpc/service.method); anything else keeps its first path
// segment only, since later segments carry slugs and ids.
func undecidedRoute(r *http.Request) string {
	path := r.URL.Path
	if strings.HasPrefix(path, "/rpc/") {
		return path
	}
	trimmed := strings.TrimPrefix(path, "/")
	first, _, hasMore := strings.Cut(trimmed, "/")
	if !hasMore {
		return "/" + first
	}
	return "/" + first + "/*"
}
