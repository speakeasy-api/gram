// Package inference adapts provider completion attempts to risk measurements.
package inference

import (
	"context"
	"fmt"

	meteringv1 "github.com/speakeasy-api/gram/infra/gen/gram/metering/v1"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/riskmeter"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

// Observer records physical provider attempts associated with a logical risk evaluation.
type Observer struct {
	recorder *riskmeter.Recorder
}

var _ openrouter.CompletionAttemptObserver = (*Observer)(nil)

// NewObserver creates an inference observer backed by recorder.
func NewObserver(recorder *riskmeter.Recorder) *Observer {
	return &Observer{recorder: recorder}
}

// ObserveCompletionAttempt records attempt when ctx carries a risk evaluation.
func (o *Observer) ObserveCompletionAttempt(ctx context.Context, attempt openrouter.CompletionAttempt) error {
	if o == nil || o.recorder == nil {
		return nil
	}

	evaluation, ok := riskmeter.EvaluationFromContext(ctx)
	if !ok {
		return nil
	}
	evaluation.OccurredAt = attempt.StartedAt

	outcome, err := mapOutcome(attempt.Status)
	if err != nil {
		return err
	}

	if err := o.recorder.RecordInference(ctx, evaluation, outcome, &riskmeter.Inference{
		Model:             conv.Default(attempt.ReturnedModel, attempt.RequestedModel),
		ProviderRequestID: attempt.ProviderRequestID,
		PromptTokens:      attempt.PromptTokens,
		CompletionTokens:  attempt.CompletionTokens,
		CostUSD:           attempt.CostUSD,
	}); err != nil {
		return fmt.Errorf("record inference attempt: %w", err)
	}
	return nil
}

func mapOutcome(status openrouter.CompletionAttemptStatus) (meteringv1.RiskEvaluation_Outcome, error) {
	switch status {
	case openrouter.CompletionAttemptSucceeded:
		return riskmeter.OutcomeCompleted, nil
	case openrouter.CompletionAttemptFailed:
		return riskmeter.OutcomeFailed, nil
	case openrouter.CompletionAttemptCancelled:
		return riskmeter.OutcomeCancelled, nil
	default:
		return meteringv1.RiskEvaluation_OUTCOME_UNSPECIFIED, fmt.Errorf("unsupported completion attempt status %q", status)
	}
}
