package chat

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

// A locked-down platform key must reach the user as its own code. Falling back
// to a gateway error would tell an expired trial that Gram is broken.
func TestClassifyCompletionError_InferenceDisabled(t *testing.T) {
	t.Parallel()

	svc := newServiceWithBilling(t, &fakeBillingRepo{})
	err := svc.classifyCompletionError(t.Context(), "completion failed",
		fmt.Errorf("resolve OpenRouter key: %w", openrouter.ErrPlatformKeyDisabled))

	var se *oops.ShareableError
	require.ErrorAs(t, err, &se)
	require.Equal(t, oops.CodeInferenceDisabled, se.Code)
	require.Contains(t, se.Error(), "contact support")
}

// An exhausted balance keeps its own code: a top-up clears it, and a
// reinstatement does not.
func TestClassifyCompletionError_InsufficientCreditsUnchanged(t *testing.T) {
	t.Parallel()

	svc := newServiceWithBilling(t, &fakeBillingRepo{})
	err := svc.classifyCompletionError(t.Context(), "completion failed",
		fmt.Errorf("openrouter 402: %w", openrouter.ErrInsufficientCredits))

	var se *oops.ShareableError
	require.ErrorAs(t, err, &se)
	require.Equal(t, oops.CodeInsufficientCredits, se.Code)
}
