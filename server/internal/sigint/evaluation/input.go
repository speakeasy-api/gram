package evaluation

import (
	"context"
	"errors"

	sigintv1 "github.com/speakeasy-api/gram/infra/gen/gram/sigint/v1"
	"github.com/speakeasy-api/gram/server/internal/classifier"
	"github.com/speakeasy-api/gram/server/internal/sigint/matching"
	"github.com/speakeasy-api/gram/server/internal/sigint/readings"
)

// ErrInvalidInput marks source content that cannot succeed on redelivery.
// Adapters wrap this sentinel for malformed or unsupported source payloads.
var ErrInvalidInput = errors.New("invalid evaluation input")

// maxContentBytes caps resolved classifier input at 16 MiB. History transfers
// share this payload budget; decoded objects and encoding copies add heap overhead.
const maxContentBytes = 16 << 20

// Event carries tenant scope, stable source identity, and observed attribution.
// Adapters must not infer billing attribution from actor or account identity.
type Event struct {
	// Evaluation identifies the trigger, manual run and immutable request time.
	Evaluation readings.Context

	// OrganizationID owns the event and the sensor definitions.
	OrganizationID string

	// ProjectID is the Speakeasy project UUID owning the event.
	ProjectID string

	// Subject identifies the source event and optional source-specific context.
	Subject *sigintv1.Reading_Event

	// Actor is source-observed attribution, independently of billing allocation.
	Actor *sigintv1.Reading_Actor

	// BillingUserID is explicit producer allocation; nil preserves absence.
	BillingUserID *string

	// Source namespaces external actor identities, not the event kind.
	Source *string

	// Account describes the external AI account used for the source workload.
	Account *sigintv1.Reading_Account

	// AssistantID identifies a Speakeasy assistant when supplied by the producer.
	AssistantID *string

	// Replayed marks historical ingestion, not transport redelivery.
	Replayed *bool

	// Attributes preserves customer-defined string labels from the source event.
	Attributes map[string]string
}

// Input adapts a source event into immutable metadata and classifier content.
// Metadata and buffer creation are I/O-free. Buffer resolution happens only after
// entitlement and sensor selection, avoiding reads for inapplicable events.
// Event metadata must remain immutable during Evaluate.
type Input interface {
	// Event returns the normalized tenant, event identity, and attribution.
	Event() Event

	// MatchingMessage returns persisted role metadata without I/O. Nil means this
	// source has no message context; predicates requiring it fail evaluation.
	MatchingMessage() *matching.Message

	// NewBuffer creates an I/O-free, per-evaluation buffer bounded by the largest
	// window among matched, runnable sensors. Content is loaded lazily on resolution.
	NewBuffer(maxSize int) InputBuffer
}

// InputBuffer owns shared source content for one evaluation. Resolutions are
// sequential; a new evaluation must create a fresh buffer to retry source reads.
type InputBuffer interface {
	// Resolve returns the target alone for size 1, or a window of up to size
	// messages including the target. Sizes cannot exceed the buffer's capacity.
	// Oversized history is truncated to the newest contiguous suffix that fits.
	// Sources without history support only size 1. A wrapped ErrInvalidInput
	// acknowledges invalid content or unsupported sizes. The conversation adapter's
	// asset-validation permanentError is also terminal; other errors trigger retry.
	Resolve(ctx context.Context, size int) (ResolvedInput, error)
}

// ResolvedInput carries classifier state and its effective context size.
type ResolvedInput struct {
	// State is complete source-specific text or structured JSON.
	State classifier.Entry

	// MessageCount includes the target; independent events have a count of one.
	MessageCount int

	// Truncated indicates the content budget excluded available history.
	// A naturally short conversation does not count as truncation.
	Truncated bool
}
