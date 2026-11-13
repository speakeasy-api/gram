package authz

import (
	"context"
	"fmt"
)

const (
	// MaxGroupRequests bounds total leaf checks across this two-level composition.
	MaxGroupRequests = 100
)

// AlternativeGroup declares one independently sufficient path to an operation.
type AlternativeGroup struct {
	// Mode controls whether exclusions are shared within this path.
	Mode AlternativeMode

	// Requests are the alternatives within the path; empty paths are invalid.
	Requests []Request
}

// EvaluationGroup contains validated alternatives for one domain path.
type EvaluationGroup struct {
	// Mode selects the exclusion semantics within this group.
	Mode AlternativeMode

	// Questions all refer to the same authenticated subject.
	Questions []Evaluation
}

// RequireAnyGroups allows any group, with exclusions local to that group. This
// supports two-level domain composition, not arbitrary recursive expressions.
func (c *Client) RequireAnyGroups(ctx context.Context, subject Subject, groups ...AlternativeGroup) error {
	if len(groups) == 0 || len(groups) > MaxGroupRequests {
		return fmt.Errorf("authorization groups require a bounded nonempty list")
	}
	prepared := make([]EvaluationGroup, 0, len(groups))
	total := 0
	for _, group := range groups {
		if group.Mode != IndependentAlternatives && group.Mode != UnblockedAlternatives {
			return fmt.Errorf("unknown authorization alternative mode")
		}
		total += len(group.Requests)
		if len(group.Requests) == 0 || total > MaxGroupRequests {
			return fmt.Errorf("authorization groups require 1 to %d total requests and no empty groups", MaxGroupRequests)
		}
		questions := make([]Evaluation, 0, len(group.Requests))
		for _, request := range group.Requests {
			question, err := c.prepareEvaluation(subject, request)
			if err != nil {
				return err
			}
			questions = append(questions, question)
		}
		prepared = append(prepared, EvaluationGroup{Mode: group.Mode, Questions: questions})
	}
	decision, err := c.backend.AuthorizeGroups(ctx, subject, prepared)
	if err != nil {
		return err
	}
	if decision.Outcome != Allowed {
		return fmt.Errorf("%w: %s", ErrDenied, decision.Reason)
	}
	return nil
}
