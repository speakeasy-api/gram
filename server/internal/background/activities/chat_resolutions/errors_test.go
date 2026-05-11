package resolution_activities

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"

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

func TestIsInsufficientCredits_OtherErrorsReturnFalse(t *testing.T) {
	t.Parallel()

	assert.False(t, IsInsufficientCredits(errors.New("network blip")))
	assert.False(t, IsInsufficientCredits(nil))
}
