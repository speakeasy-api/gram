// Package classifier defines provider-independent batch classification contracts.
// Callers translate domain definitions into questions and map answers back to
// their domain. Implementations own token accounting, request partitioning,
// provider transport, and response validation.
package classifier

import (
	"context"
	"errors"
)

// ErrRequestTooLarge means the provider rejected the complete input with a
// single indivisible question as too large. Classify returns known outcomes
// alongside this error, including successes for other questions.
var ErrRequestTooLarge = errors.New("classifier: input and a single question exceed provider request limits")

// ErrInvalidRequest means the input or questions cannot be classified without
// changing the request. Retrying the same request cannot succeed.
var ErrInvalidRequest = errors.New("classifier: invalid request")

// Classifier evaluates independent questions against a shared input.
// The provider, model, limits, and concurrency are configured on implementations,
// rather than selected by individual requests. Implementations must be safe for
// concurrent use and must not mutate or retain mutable caller-owned request data
// after a call returns.
type Classifier interface {
	// Classify evaluates every question against the same input. Implementations
	// partition whole questions, duplicate the input across partitions as needed,
	// and bound concurrent provider requests. Choice options and Score levels within
	// one question must never be split into separate classifications.
	//
	// A nil Result.Err() guarantees one outcome per submitted question, in submission
	// order. Question-level failures, including failures shared by a partition,
	// are outcomes rather than operation errors, except an unsplittable size rejection
	// also sets Result.Err() to match ErrRequestTooLarge. Callers may retry only those marked
	// retryable; successful answers must not be discarded because another fails.
	//
	// Disabled implementations return ErrDisabled without validating the request.
	// Otherwise, invalid request structure, including a nil request, sets
	// Result.Err() to match ErrInvalidRequest before any provider execution.
	// Cancellation or an operation-wide failure may return
	// partial outcomes and a non-nil Result.Err(). A partial result contains only known outcomes, in submission
	// order; absence of an outcome does not prove a provider request was not sent.
	//
	// Input fit and request packing are handled internally. Implementations
	// must not silently truncate input or question content to make it fit.
	// Execution completes before returning. Result.Iter walks collected outcomes;
	// callers should process them and check Result.Err() even after early iteration exit.
	Classify(ctx context.Context, request *Request) Result
}

// Request is one logical batch, independent of provider request boundaries.
// Callers must not mutate a request while classification is in progress.
type Request struct {
	// Input is evaluated independently against each question. Content extraction,
	// asset resolution, and domain-specific framing belong to callers.
	Input Entry

	// Questions is non-empty and has unique, non-empty keys within this batch.
	Questions []Question
}

// Metadata identifies the implementation contract used for a run.
type Metadata struct {
	// Provider identifies the inference transport (for example, openrouter),
	// independently of the model's vendor or family.
	Provider string

	// Model is the configured model identifier. Concrete versions are preferred
	// over moving aliases when repeatable results are required.
	Model string

	// CompilerVersion identifies the question-to-provider translation rules.
	CompilerVersion string

	// AccountingVersion identifies token counting, overhead, and packing rules.
	AccountingVersion string
}
