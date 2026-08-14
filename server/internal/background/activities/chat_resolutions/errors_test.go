package resolution_activities

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/temporal"

	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

func TestIsInsufficientCredits_FromOpenRouterError(t *testing.T) {
	t.Parallel()

	wrapped := fmt.Errorf("failed to analyze segment with LLM: %w", openrouter.ErrInsufficientCredits)
	assert.True(t, IsInsufficientCredits(wrapped))
}

func TestIsInsufficientCredits_FromTemporalApplicationError(t *testing.T) {
	t.Parallel()

	original := fmt.Errorf("openrouter 402: %w", openrouter.ErrInsufficientCredits)
	tempErr := newInsufficientCreditsError(original)
	assert.True(t, IsInsufficientCredits(tempErr))
}

func TestExpectedChatResolutionErrorsAreBenign(t *testing.T) {
	t.Parallel()

	creditsErr := newInsufficientCreditsError(openrouter.ErrInsufficientCredits)
	var creditsApp *temporal.ApplicationError
	require.ErrorAs(t, creditsErr, &creditsApp)
	require.Equal(t, temporal.ApplicationErrorCategoryBenign, creditsApp.Category())

	disabledErr := newInferenceDisabledError(errors.New("key locked"))
	var disabledApp *temporal.ApplicationError
	require.ErrorAs(t, disabledErr, &disabledApp)
	require.Equal(t, temporal.ApplicationErrorCategoryBenign, disabledApp.Category())

	bumpedErr := newGenerationBumpedError(1, 2)
	var bumpedApp *temporal.ApplicationError
	require.ErrorAs(t, bumpedErr, &bumpedApp)
	require.Equal(t, temporal.ApplicationErrorCategoryBenign, bumpedApp.Category())
}

func TestIsInsufficientCredits_OtherErrorsReturnFalse(t *testing.T) {
	t.Parallel()

	assert.False(t, IsInsufficientCredits(errors.New("network blip")))
	assert.False(t, IsInsufficientCredits(nil))
}
