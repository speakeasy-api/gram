package assistanttokens

import (
	"context"
	"errors"
	"fmt"

	"github.com/speakeasy-api/gram/server/internal/assistantidentity"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

type inferenceContextKey struct{}
type inferenceRuntimeContextKey struct{}

// InferenceTranscriptOwned reports a trusted runtime whose ACP transcript owns persistence.
func InferenceTranscriptOwned(ctx context.Context) bool {
	_, ok := ctx.Value(inferenceContextKey{}).(assistantidentity.Identity)
	return ok
}

// Invalid identity is an authentication refusal; infrastructure failures must
// retain their cause and remain server errors rather than prompting re-login.
func kitIdentityValidationError(err error) error {
	if errors.Is(err, assistantidentity.ErrInvalidIdentity) {
		return oops.E(oops.CodeUnauthorized, err, "invalid managed assistant identity")
	}
	return fmt.Errorf("validate managed assistant identity: %w", err)
}
