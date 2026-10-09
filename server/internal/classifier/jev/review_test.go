package jev

import (
	"net/http"
	"testing"
	"testing/synctest"
	"time"

	"github.com/speakeasy-api/gram/server/internal/classifier"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestOptionalConfidence(t *testing.T) {
	t.Parallel()
	options := []classifier.Option{classifier.NewOption("a", classifier.Text("a")), classifier.NewOption("b", classifier.Text("b"))}
	req := classifier.NewRequest(classifier.Text("state")).Ask(classifier.Choice("choice", classifier.Text("choose"), options...)).Ask(classifier.Score("score", classifier.Text("score"), options...))

	planned, err := compileRequest(req)
	require.NoError(t, err)

	for _, q := range planned {
		for _, confidence := range []*float64{nil, new(0.0), new(1.0), new(-0.1), new(1.1)} {
			wire := wireAnswer{Type: q.wire.Type, Choice: new("1"), Score: new(0.75), Probabilities: map[string]*float64{"0": new(0.25), "1": new(0.75)}, Confidence: confidence}
			answer := decodeAnswer(q, wire)
			if confidence != nil && (*confidence < 0 || *confidence > 1) {
				require.Nil(t, answer)
				continue
			}
			require.NotNil(t, answer)
			if answer.Choice != nil {
				require.Equal(t, confidence, answer.Choice.Confidence)
			} else {
				require.Equal(t, confidence, answer.Score.Confidence)
			}
		}
	}
}

func TestInvalidRequestIsPermanent(t *testing.T) {
	t.Parallel()
	c := testClient(t, func(_ http.ResponseWriter, _ *http.Request) { t.Error("invalid request reached provider") })
	require.ErrorIs(t, c.Classify(t.Context(), nil).Err(), classifier.ErrInvalidRequest)
}

func TestAdmissionWaitUsesAdapterDeadline(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		c := &Classifier{slots: make(chan struct{}, 1)}
		c.slots <- struct{}{}
		start := time.Now()
		_, failed, _, sent := c.send(t.Context(), classifier.Text("state"), nil)
		require.Equal(t, 60*time.Second, time.Since(start))
		require.Equal(t, classifier.FailureProviderUnavailable, failed.Code)
		require.True(t, failed.Retryable)
		require.False(t, sent)
	})
}

func TestGuardianDenialPreservesRetryHint(t *testing.T) {
	t.Parallel()
	var limiter mockLimiter
	limiter.Test(t)
	t.Cleanup(func() { limiter.AssertExpectations(t) })
	limiter.On("AllowN", mock.Anything, mock.Anything, mock.Anything, uint32(1)).Return(guardian.RateLimitResult{}, &guardian.ResilienceError{Reason: guardian.ErrRateLimited, RetryAfter: 2 * time.Second}).Once()

	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), nil, guardian.WithLimiter(&limiter))
	require.NoError(t, err)

	c, err := New(policy, conv.NewSecret([]byte("test-key")))
	require.NoError(t, err)
	result := c.Classify(t.Context(), classifier.NewRequest(classifier.Text("state")).Ask(classifier.Noul("a", classifier.Text("a"))))
	require.Len(t, result.Outcomes, 1)
	require.Equal(t, classifier.FailureRateLimited, result.Outcomes[0].Failure.Code)
	require.Equal(t, 2*time.Second, *result.Outcomes[0].Failure.RetryAfter)
}
