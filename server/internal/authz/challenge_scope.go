package authz

import (
	"context"
	"sync"

	authzrepo "github.com/speakeasy-api/gram/server/internal/authz/repo"
)

type challengeScopeKey struct{}

// challengeScope decides which decisions of one request are recorded as
// authorization checks. Decisions still count in the decision metrics either
// way; only the recorded check, an outbox row each, is skipped.
type challengeScope struct {
	// silent records nothing: the caller is filtering or listing, not
	// asking for one resource.
	silent bool
	// recorded is shared by every scope derived from one request. Nil for a
	// silent scope.
	recorded *recordedChallenges
}

// recordedChallenges remembers, per scope and resource, whether a request has
// recorded an allowed and a denied check.
type recordedChallenges struct {
	mu      sync.Mutex
	outcome map[challengeResource]challengeOutcomes
}

type challengeResource struct {
	scope      Scope
	resourceID string
}

type challengeOutcomes struct {
	allowed bool
	denied  bool
}

// WithOneChallengePerResource returns a context under which the engine records
// at most one check per scope and resource for the rest of the request: the
// first decision, and later a denial when only an allow was recorded. A gateway
// request checks the server it names several times (the server, then the tool
// it calls), and every one of those would otherwise be its own row.
func WithOneChallengePerResource(ctx context.Context) context.Context {
	if scope, ok := ctx.Value(challengeScopeKey{}).(*challengeScope); ok && scope.recorded != nil {
		return ctx
	}
	return context.WithValue(ctx, challengeScopeKey{}, &challengeScope{
		silent:   false,
		recorded: &recordedChallenges{mu: sync.Mutex{}, outcome: map[challengeResource]challengeOutcomes{}},
	})
}

// WithoutChallenges returns a context under which the engine records no
// checks. Use it where a request filters or lists resources it did not name,
// so a caller is not shown a denial for every resource it simply cannot see.
// Evaluate is the single-check form of the same rule.
func WithoutChallenges(ctx context.Context) context.Context {
	return context.WithValue(ctx, challengeScopeKey{}, &challengeScope{silent: true, recorded: nil})
}

// claimChallenge reports whether a decision is recorded under ctx's scope, and
// marks it recorded.
func claimChallenge(ctx context.Context, l challengeLogger) bool {
	scope, ok := ctx.Value(challengeScopeKey{}).(*challengeScope)
	if !ok {
		return true
	}
	if scope.silent {
		return false
	}

	focus := l.Focus
	if focus == nil && len(l.Checks) > 0 {
		focus = &l.Checks[0]
	}
	if focus == nil {
		return true
	}
	key := challengeResource{scope: focus.Scope, resourceID: focus.ResourceID}

	scope.recorded.mu.Lock()
	defer scope.recorded.mu.Unlock()
	seen := scope.recorded.outcome[key]
	switch l.Outcome {
	case authzrepo.OutcomeAllow:
		if seen.allowed || seen.denied {
			return false
		}
		seen.allowed = true
	case authzrepo.OutcomeDeny:
		if seen.denied {
			return false
		}
		seen.denied = true
	default:
		// Errors are always recorded: they are not a decision about access.
		return true
	}
	scope.recorded.outcome[key] = seen
	return true
}
