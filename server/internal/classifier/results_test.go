package classifier_test

import (
	"fmt"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/classifier"
	"github.com/speakeasy-api/gram/server/internal/classifier/classifiertest"
)

func TestResultIterationPreservesPartialSuccessAndError(t *testing.T) {
	t.Parallel()
	var answer classifier.Answer
	answer.Noul = &classifier.NoulAnswer{Probability: 0.8}
	outcomes := []classifier.QuestionOutcome{
		{Key: "success", Answer: &answer, Failure: nil},
		{Key: "oversized", Answer: nil, Failure: &classifier.QuestionFailure{
			Code: classifier.FailureInputTooLarge, Message: "Too large", Retryable: false, RetryAfter: nil,
		}},
	}
	result := classifier.NewResult(outcomes, fmt.Errorf("provider: %w", classifier.ErrRequestTooLarge))
	require.ErrorIs(t, result.Err(), classifier.ErrRequestTooLarge)
	count := 0
	for outcome := range result.Iter() {
		require.Equal(t, classifier.QuestionKey("success"), outcome.Key)
		count++
		break
	}
	require.Equal(t, 1, count)
	// A second iteration starts from the beginning even after early termination.
	require.Equal(t, outcomes, slices.Collect(result.Iter()))
	require.ErrorIs(t, result.Err(), classifier.ErrRequestTooLarge)
}

func TestResultZeroValueAndDisabled(t *testing.T) {
	t.Parallel()
	var result classifier.Result
	require.Empty(t, slices.Collect(result.Iter()))
	require.NoError(t, result.Err())
	result = classifier.Noop{}.Classify(t.Context(), nil)
	require.Empty(t, slices.Collect(result.Iter()))
	require.ErrorIs(t, result.Err(), classifier.ErrDisabled)
}

func TestMockReturnsResultWithError(t *testing.T) {
	t.Parallel()
	c := classifiertest.NewMock(t)
	req := classifier.NewRequest(classifier.Text("state"))
	c.On("Classify", t.Context(), req).Return(classifier.NewResult(nil, classifier.ErrRequestTooLarge)).Once()
	result := c.Classify(t.Context(), req)
	require.Empty(t, slices.Collect(result.Iter()))
	require.ErrorIs(t, result.Err(), classifier.ErrRequestTooLarge)
}
