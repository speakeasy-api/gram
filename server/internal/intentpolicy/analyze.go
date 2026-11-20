package intentpolicy

import (
	"context"
	"fmt"

	"golang.org/x/sync/errgroup"
)

const (
	// MaxActions caps one session analysis, matching the guardrail replay cap.
	MaxActions = 200

	// analyzeConcurrency bounds in-flight evaluator calls per analysis.
	analyzeConcurrency = 4
)

// Totals summarizes one analysis.
type Totals struct {
	// Calls is the number of tool calls in the session.
	Calls int

	// Judged is the number of calls assessed.
	Judged int

	// Blocked counts block decisions, including evaluator failures.
	Blocked int

	// Clarified counts clarify decisions.
	Clarified int

	// Permitted counts permit decisions.
	Permitted int

	// CostUSD sums evaluator cost.
	CostUSD float64

	// LatencyMs sums evaluator latency.
	LatencyMs int64
}

// Analysis is the assessment of a session's tool calls.
type Analysis struct {
	// Guardrail is the guardrail applied.
	Guardrail Guardrail

	// Descriptor identifies the evaluator.
	Descriptor Descriptor

	// Assessments holds one entry per judged call, in session order.
	Assessments []Assessment

	// Totals summarizes the assessments.
	Totals Totals

	// LimitHit reports that the session had more than MaxActions calls.
	LimitHit bool
}

// Analyze assesses up to MaxActions calls with bounded concurrency. Evaluator
// failures become fail-closed assessments rather than errors.
func Analyze(ctx context.Context, g Guardrail, engine *Engine, evaluator Evaluator, actions []Action) (Analysis, error) {
	judged := actions
	if len(judged) > MaxActions {
		judged = judged[:MaxActions]
	}

	descriptor := evaluator.Descriptor()
	assessments := make([]Assessment, len(judged))
	group, groupCtx := errgroup.WithContext(ctx)
	group.SetLimit(analyzeConcurrency)
	for i, action := range judged {
		group.Go(func() error {
			labels, usage, err := evaluator.Label(groupCtx, LabelInput{Action: action, Grounding: g.Statements, Questions: g.Questions})
			if labels == nil {
				labels = LabelSet{}
			}
			decision, decidedBy, outcomes := engine.Evaluate(action, labels)
			a := Assessment{
				Action:         action,
				Labels:         labels,
				Decision:       decision,
				DecidedBy:      decidedBy,
				Outcomes:       outcomes,
				EvaluatorError: "",
				Usage:          usage,
				Descriptor:     descriptor,
			}
			if err != nil {
				a.EvaluatorError = err.Error()
			}
			assessments[i] = a
			return nil
		})
	}
	if err := group.Wait(); err != nil {
		return Analysis{}, fmt.Errorf("analyze actions: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return Analysis{}, fmt.Errorf("analyze actions: %w", err)
	}

	totals := Totals{Calls: len(actions), Judged: len(assessments), Blocked: 0, Clarified: 0, Permitted: 0, CostUSD: 0, LatencyMs: 0}
	for _, a := range assessments {
		switch a.Decision {
		case DecisionBlock:
			totals.Blocked++
		case DecisionClarify:
			totals.Clarified++
		case DecisionPermit:
			totals.Permitted++
		}
		totals.CostUSD += a.Usage.CostUSD
		totals.LatencyMs += a.Usage.LatencyMs
	}

	return Analysis{
		Guardrail:   g,
		Descriptor:  descriptor,
		Assessments: assessments,
		Totals:      totals,
		LimitHit:    len(actions) > len(judged),
	}, nil
}
