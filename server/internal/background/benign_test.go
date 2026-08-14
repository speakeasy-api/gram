package background

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/temporal"
)

func TestAsBenignWorkflowError_Nil(t *testing.T) {
	t.Parallel()

	require.NoError(t, asBenignWorkflowError(nil))
}

func TestAsBenignWorkflowError_PreservesTypeAndMessage(t *testing.T) {
	t.Parallel()

	cause := temporal.NewApplicationErrorWithOptions(
		"DNS record not found for example.com",
		"CustomDomainDNSNotFound",
		temporal.ApplicationErrorOptions{
			NonRetryable: true,
			Category:     temporal.ApplicationErrorCategoryBenign,
			Cause:        errors.New("no such host"),
		},
	)
	wrapped := fmt.Errorf("failed to verify custom domain: %w", cause)

	err := asBenignWorkflowError(wrapped)
	require.Error(t, err)

	var appErr *temporal.ApplicationError
	require.ErrorAs(t, err, &appErr)
	require.Equal(t, "CustomDomainDNSNotFound", appErr.Type())
	require.Equal(t, "DNS record not found for example.com", appErr.Message())
	require.True(t, appErr.NonRetryable())
	require.Equal(t, temporal.ApplicationErrorCategoryBenign, appErr.Category())
	require.ErrorIs(t, err, cause)
}
