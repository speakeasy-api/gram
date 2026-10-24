package assistanttokens

import (
	"context"

	"github.com/speakeasy-api/gram/server/internal/assistantidentity"
)

type inferenceContextKey struct{}
type inferenceRuntimeContextKey struct{}

// InferenceTranscriptOwned reports a trusted runtime whose ACP transcript owns persistence.
func InferenceTranscriptOwned(ctx context.Context) bool {
	_, ok := ctx.Value(inferenceContextKey{}).(assistantidentity.Identity)
	return ok
}
