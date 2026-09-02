package openrouter

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"

	or "github.com/OpenRouterTeam/go-sdk/models/components"
	"github.com/OpenRouterTeam/go-sdk/models/sdkerrors"
	"github.com/stretchr/testify/require"
)

func TestClassifySDKError_BadRequestIsSanitizedAndPermanent(t *testing.T) {
	t.Parallel()

	const canary = "embedding-input-must-not-reach-error"
	sdkErr := &sdkerrors.BadRequestResponseError{
		Error_: or.BadRequestResponseErrorData{
			Code:     http.StatusBadRequest,
			Message:  canary,
			Metadata: nil,
		},
		UserID: nil,
	}

	err := classifySDKError(context.Background(), fmt.Errorf("generate embedding: %w", sdkErr))
	var httpErr *HTTPError
	require.ErrorAs(t, err, &httpErr)
	require.Equal(t, http.StatusBadRequest, httpErr.StatusCode)
	require.ErrorIs(t, err, ErrBadRequest)
	require.True(t, IsPermanentError(err))
	require.NotContains(t, err.Error(), canary)
}

func TestClassifySDKError_GenericRateLimitRemainsRetryable(t *testing.T) {
	t.Parallel()

	sdkErr := &sdkerrors.APIError{
		Message:     "API error occurred",
		StatusCode:  http.StatusTooManyRequests,
		Body:        "rate-limit-details",
		RawResponse: nil,
	}

	err := classifySDKError(context.Background(), sdkErr)
	require.ErrorIs(t, err, ErrRateLimited)
	require.False(t, IsPermanentError(err))
	require.NotContains(t, err.Error(), sdkErr.Body)
}

func TestClassifySDKError_LeavesTransportErrorsUnchanged(t *testing.T) {
	t.Parallel()

	transportErr := errors.New("connection reset")
	require.ErrorIs(t, classifySDKError(context.Background(), transportErr), transportErr)
}
