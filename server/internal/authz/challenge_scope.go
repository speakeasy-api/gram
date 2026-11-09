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

// recordedChallenges remembers which decisions a request has recorded.
type recordedChallenges struct {
	mu sync.Mutex
	// decided holds every scope and resource with a recorded decision.
	decided map[challengeResource]struct{}
	// denied holds every recorded denial by the tool and disposition it
	// refused, so each distinct refusal on a resource is recorded once.
	denied map[challengeDenial]struct{}
}

type challengeResource struct {
	scope      Scope
	resourceID string
}

// challengeDenial is the refusal of one tool and disposition on a resource. A
// check that names no tool, such as connecting to the server, has both empty.
type challengeDenial struct {
	resource    challengeResource
	tool        string
	disposition string
}

// WithOneChallengePerResource returns a context under which the engine records
// at most one check per scope and resource for the rest of the request: the
// first decision, and later each distinct denial (by tool and disposition) not
// recorded yet. A gateway request checks the server it names several times
// (the server, then the tool it calls), and every one of those would otherwise
// be its own row.
func WithOneChallengePerResource(ctx context.Context) context.Context {
	if scope, ok := ctx.Value(challengeScopeKey{}).(*challengeScope); ok && scope.recorded != nil {
		return ctx
	}
	return context.WithValue(ctx, challengeScopeKey{}, &challengeScope{
		silent:   false,
		recorded: &recordedChallenges{mu: sync.Mutex{}, decided: map[challengeResource]struct{}{}, denied: map[challengeDenial]struct{}{}},
	})
}

// WithoutChallenges returns a context under which the engine records no
// checks. Use it where a request filters or lists resources it did not name,
// so a caller is not shown a denial for every resource it simply cannot see.
// Evaluate is the single-check form of the same rule.
func WithoutChallenges(ctx context.Context) context.Context {
	return context.WithValue(ctx, challengeScopeKey{}, &challengeScope{silent: true, recorded: nil})
}

// challengesSilenced reports a context that records no checks. It is read
// before anything else a recorded check costs, such as the feature flag.
func challengesSilenced(ctx context.Context) bool {
	scope, ok := ctx.Value(challengeScopeKey{}).(*challengeScope)
	return ok && scope.silent
}

// claimChallenge reports whether a decision is recorded under ctx's scope, and
// marks it recorded. Call it only once the decision is about to be written.
func claimChallenge(ctx context.Context, l challengeLogger) bool {
	scope, ok := ctx.Value(challengeScopeKey{}).(*challengeScope)
	if !ok {
		return true
	}
	if scope.recorded == nil {
		return false
	}

	// A batch (Filter, FindMatched) names no one resource. It never takes a
	// resource's slot, so a later decision about that resource still records,
	// but it is skipped once that resource has a recorded decision.
	batch := l.Focus == nil
	focus := l.Focus
	if batch && len(l.Checks) > 0 {
		focus = &l.Checks[0]
	}
	if focus == nil {
		return true
	}
	key := challengeResource{scope: focus.Scope, resourceID: focus.ResourceID}

	scope.recorded.mu.Lock()
	defer scope.recorded.mu.Unlock()
	_, decided := scope.recorded.decided[key]
	if batch {
		return !decided
	}
	switch l.Outcome {
	case authzrepo.OutcomeAllow:
		if decided {
			return false
		}
	case authzrepo.OutcomeDeny:
		// A denial after an allow is still recorded: connecting was allowed
		// but this tool or disposition was not, and that refusal is real. Two
		// different refused tools on one server are two refusals.
		denial := challengeDenial{
			resource:    key,
			tool:        focus.Dimensions[SelectorKeyTool],
			disposition: focus.Dimensions[SelectorKeyDisposition],
		}
		if _, seen := scope.recorded.denied[denial]; seen {
			return false
		}
		scope.recorded.denied[denial] = struct{}{}
	default:
		// Errors are always recorded: they are not a decision about access.
		return true
	}
	scope.recorded.decided[key] = struct{}{}
	return true
}
