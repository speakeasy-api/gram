package classifier

import (
	"iter"
	"slices"
	"time"
)

// Probability associates an option with its probability.
type Probability struct {
	// Option identifies an option from the submitted question.
	Option OptionKey

	// Value is finite and in the inclusive range [0, 1].
	Value float64
}

// NoulAnswer is an independent positive-outcome probability. Thresholds and
// combining multiple answers into domain decisions are caller responsibilities.
type NoulAnswer struct {
	// Probability is finite and in the inclusive range [0, 1].
	Probability float64
}

// ChoiceAnswer describes a mutually exclusive decision.
type ChoiceAnswer struct {
	// Selected identifies a submitted option with the highest probability.
	Selected OptionKey

	// Distribution contains every option exactly once, in submission order. It
	// preserves provider probabilities without normalization or a sum-to-one check.
	Distribution []Probability

	// Confidence is optional provider-reported question-level confidence in
	// [0, 1]. It is distinct from the selected option's probability.
	Confidence *float64
}

// ScoreAnswer describes a probability-weighted position on an ordered rubric.
type ScoreAnswer struct {
	// ExpectedIndex is the provider-reported expected zero-based level index,
	// finite and within [0, number of levels - 1], not a selected level. It may
	// differ from the mean reconstructed from the returned distribution due to
	// differences in precision.
	ExpectedIndex float64

	// Distribution contains every level exactly once in low-to-high order. It
	// preserves provider probabilities without normalization or a sum-to-one check.
	Distribution []Probability

	// Confidence is optional provider-reported question-level confidence in [0, 1].
	Confidence *float64
}

// Answer contains exactly one variant, matching the submitted question's kind.
// Implementations validate keys, completeness, numeric bounds, and response kind
// before exposing answers. Malformed provider answers become failures.
type Answer struct {
	// Noul is populated only for Noul questions.
	Noul *NoulAnswer

	// Choice is populated only for Choice questions.
	Choice *ChoiceAnswer

	// Score is populated only for Score questions.
	Score *ScoreAnswer
}

// FailureCode identifies why a question could not be evaluated.
type FailureCode string

const (
	// FailureInputTooLarge means the shared input cannot fit with this question.
	FailureInputTooLarge FailureCode = "input_too_large"

	// FailureQuestionTooLarge means the question itself exceeds a model limit.
	FailureQuestionTooLarge FailureCode = "question_too_large"

	// FailureRateLimited means the provider rejected the request due to rate limits.
	FailureRateLimited FailureCode = "rate_limited"

	// FailureProviderUnavailable indicates a temporary transport or provider failure.
	FailureProviderUnavailable FailureCode = "provider_unavailable"

	// FailureProviderRejected means the provider permanently rejected the request.
	FailureProviderRejected FailureCode = "provider_rejected"

	// FailureInvalidResponse means the provider answer violates the question contract.
	FailureInvalidResponse FailureCode = "invalid_response"
)

// QuestionFailure is a failed outcome, potentially shared by a whole partition.
type QuestionFailure struct {
	// Code identifies the failure category.
	Code FailureCode

	// Message provides a safe explanation without raw input or credentials.
	Message string

	// Retryable indicates that resubmitting the unchanged question may succeed.
	// Callers own retry policy; this is not a guarantee of eventual success.
	Retryable bool

	// RetryAfter is an optional non-negative provider-suggested retry delay.
	RetryAfter *time.Duration
}

// QuestionOutcome contains exactly one of Answer or Failure.
type QuestionOutcome struct {
	// Key matches a submitted question key.
	Key QuestionKey

	// Answer contains a validated successful classification.
	Answer *Answer

	// Failure describes why classification was not available.
	Failure *QuestionFailure
}

// Usage aggregates provider-reported token counts across all physical attempts,
// including retries if an implementation performs them. Shared input can be
// charged more than once when questions span provider requests.
type Usage struct {
	// InputTokens is the non-negative sum of known input token usage.
	InputTokens int64

	// OutputTokens is the non-negative sum of known output token usage.
	OutputTokens int64

	// Complete is false if any attempted request's usage is unknown. In that
	// case the counts are partial, not evidence of zero usage for failed requests.
	Complete bool
}

// Result combines collected outcomes independently of provider partition boundaries.
// Execution is complete when Classify returns; iteration does not perform I/O.
// The zero value is an empty successful result.
type Result struct {
	// Metadata identifies the configured model and implementation versions.
	Metadata Metadata

	// Models lists distinct concrete model versions reported by the provider.
	// Empty means no version was reported, not that no execution occurred.
	Models []string

	// Outcomes contains known outcomes in submission order. With a nil Err()
	// it covers every question; with an error it may cover only a subset.
	Outcomes []QuestionOutcome

	// Usage contains known usage across the operation's physical attempts.
	Usage Usage

	err error
}

// NewResult constructs a collected result for providers and test doubles. It
// copies the outcomes slice; nested answers and failures are shared. Metadata,
// Models, and Usage may be populated on the returned value by the provider.
func NewResult(outcomes []QuestionOutcome, err error) Result {
	var result Result
	result.Outcomes = slices.Clone(outcomes)
	result.err = err
	return result
}

// Iter yields known outcomes in submission order, including question failures.
// Iteration is repeatable and stops immediately when the caller breaks. Breaking
// does not cancel execution or change Err: execution has already completed.
func (r Result) Iter() iter.Seq[QuestionOutcome] {
	return slices.Values(r.Outcomes)
}

// Err reports the operation error, independently of iteration. It may be non-nil
// even when Iter yields successful answers. Wrapped errors retain errors.Is/As
// semantics. Individual question failures are available in their outcomes.
func (r Result) Err() error {
	return r.err
}
