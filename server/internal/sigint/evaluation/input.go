package evaluation

import (
	"context"
	"errors"

	sigintv1 "github.com/speakeasy-api/gram/infra/gen/gram/sigint/v1"
	"github.com/speakeasy-api/gram/server/internal/classifier"
	"github.com/speakeasy-api/gram/server/internal/sigint/matching"
)

// ErrInvalidInput marks source content that cannot succeed on redelivery.
// Adapters wrap this sentinel for malformed or unsupported source payloads.
var ErrInvalidInput = errors.New("invalid evaluation input")

// maxContentBytes caps resolved classifier input at 16 MiB to bound per-event memory.
const maxContentBytes = 16 << 20

// Event carries tenant scope, stable source identity, and observed attribution.
// Adapters must not infer billing attribution from actor or account identity.
type Event struct {
	// OrganizationID owns the event and the sensor definitions.
	OrganizationID string

	// ProjectID is the Gram project UUID owning the event.
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

	// AssistantID identifies a Gram assistant when supplied by the producer.
	AssistantID *string

	// Replayed marks historical ingestion, not transport redelivery.
	Replayed *bool
}

// Input adapts a source event into immutable metadata and classifier content.
// Event must be I/O-free. Resolve is called only after entitlement and sensor
// selection, avoiding asset reads for disabled tenants or inapplicable events.
// Inputs and their metadata must not be mutated during Evaluate.
type Input interface {
	// Event returns the normalized tenant, event identity, and attribution.
	Event() Event

	// MatchingMessage returns persisted role metadata without I/O. Nil means this
	// source has no message context; predicates requiring it fail evaluation.
	MatchingMessage() *matching.Message

	// Resolve returns complete source-specific text or structured JSON. A
	// wrapped ErrInvalidInput acknowledges invalid content. The conversation adapter's
	// asset-validation permanentError is also terminal; other errors trigger retry.
	Resolve(context.Context) (classifier.Entry, error)
}
