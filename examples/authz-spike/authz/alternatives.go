package authz

import (
	"context"
	"fmt"
)

// ExclusionEvidence distinguishes opposing scopes from absence of an allow.
type ExclusionEvidence string

const (
	// NoExclusion means no applicable opposing grant was found.
	NoExclusion ExclusionEvidence = "none"

	// DefiniteExclusion means an opposing grant definitely matches.
	DefiniteExclusion ExclusionEvidence = "definite"

	// UncertainExclusion means missing facts prevent resolving an opposing grant.
	UncertainExclusion ExclusionEvidence = "uncertain"
)

// AlternativeMode selects how exclusions apply across questions for one operation.
type AlternativeMode string

const (
	// IndependentAlternatives permits any allowed branch despite another's block.
	IndependentAlternatives AlternativeMode = "independent"

	// UnblockedAlternatives requires an allow and no exclusion across branches.
	UnblockedAlternatives AlternativeMode = "unblocked"
)

// RequireAny implements ordinary OR; exclusions remain local to each branch.
func (c *Client) RequireAny(ctx context.Context, subject Subject, requests ...Request) error {
	return c.requireAlternatives(ctx, subject, IndependentAlternatives, requests)
}

// RequireAnyUnblocked implements OR with exclusions shared across alternatives.
func (c *Client) RequireAnyUnblocked(ctx context.Context, subject Subject, requests ...Request) error {
	return c.requireAlternatives(ctx, subject, UnblockedAlternatives, requests)
}

func (c *Client) requireAlternatives(ctx context.Context, subject Subject, mode AlternativeMode, requests []Request) error {
	if len(requests) == 0 {
		return fmt.Errorf("authorization alternatives must not be empty")
	}
	questions := make([]Evaluation, 0, len(requests))
	for _, request := range requests {
		question, err := c.prepareEvaluation(subject, request)
		if err != nil {
			return err
		}
		questions = append(questions, question)
	}
	decision, err := c.backend.AuthorizeAny(ctx, questions, mode)
	if err != nil {
		return err
	}
	if decision.Outcome != Allowed {
		return fmt.Errorf("%w: %s", ErrDenied, decision.Reason)
	}
	return nil
}

// evaluateAlternatives preserves opposing-scope evidence even on branches with
// no allowing grant. Every question uses the same principal grant snapshot.
func evaluateAlternatives(grants []ResolvedGrant, questions []Evaluation, mode AlternativeMode, revision uint64) Decision {
	decisions := make([]Decision, 0, len(questions))
	for _, question := range questions {
		decisions = append(decisions, evaluate(grants, question, revision))
	}
	return combineAlternatives(decisions, mode, revision)
}

func combineAlternatives(decisions []Decision, mode AlternativeMode, revision uint64) Decision {
	allowed, uncertain := false, false
	exclusion := NoExclusion
	for _, decision := range decisions {
		allowed = allowed || decision.Outcome == Allowed
		uncertain = uncertain || decision.Outcome == Unresolved
		if decision.Exclusion == DefiniteExclusion {
			exclusion = DefiniteExclusion
		} else if decision.Exclusion == UncertainExclusion && exclusion == NoExclusion {
			exclusion = UncertainExclusion
		}
	}
	result := Decision{Outcome: Denied, Reason: "no alternative permits the operation", PolicyRevision: revision, Exclusion: exclusion}
	if mode == UnblockedAlternatives && exclusion == DefiniteExclusion {
		result.Reason = "opposing scope on an authorization alternative"
		return result
	}
	if allowed {
		result.Outcome, result.Reason = Allowed, "an authorization alternative permits the operation"
		if mode == UnblockedAlternatives && exclusion == UncertainExclusion {
			result.Outcome, result.Reason = Unresolved, "missing facts affect an alternative's exclusion"
		}
	} else if uncertain {
		result.Outcome, result.Reason = Unresolved, "missing facts affect authorization alternatives"
	}
	return result
}
