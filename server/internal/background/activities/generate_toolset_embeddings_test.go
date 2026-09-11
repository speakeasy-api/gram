package activities

import (
	"errors"
	"net/http"
	"testing"

	"go.temporal.io/sdk/temporal"

	"github.com/speakeasy-api/gram/server/internal/rag"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	"github.com/stretchr/testify/require"
)

func TestNewGenerateToolsetEmbeddingsError_PermanentProviderErrorIsNonRetryable(t *testing.T) {
	t.Parallel()

	cause := &openrouter.HTTPError{
		StatusCode: http.StatusBadRequest,
		Err:        openrouter.ErrBadRequest,
	}
	err := newGenerateToolsetEmbeddingsError(cause)

	var applicationErr *temporal.ApplicationError
	require.ErrorAs(t, err, &applicationErr)
	require.True(t, applicationErr.NonRetryable())
	require.Equal(t, GenerateToolsetEmbeddingsPermanentErrorType, applicationErr.Type())
	require.ErrorIs(t, err, openrouter.ErrBadRequest)
}

func TestNewGenerateToolsetEmbeddingsError_TransientErrorRemainsRetryable(t *testing.T) {
	t.Parallel()

	cause := errors.New("connection reset")
	err := newGenerateToolsetEmbeddingsError(cause)

	var applicationErr *temporal.ApplicationError
	require.NotErrorAs(t, err, &applicationErr)
	require.ErrorIs(t, err, cause)
}

func TestNewGenerateToolsetEmbeddingsError_SupersededRevisionIsNonRetryable(t *testing.T) {
	t.Parallel()

	err := newGenerateToolsetEmbeddingsError(rag.ErrToolsetIndexRevisionSuperseded)

	var applicationErr *temporal.ApplicationError
	require.ErrorAs(t, err, &applicationErr)
	require.True(t, applicationErr.NonRetryable())
	require.Equal(t, GenerateToolsetEmbeddingsSupersededErrorType, applicationErr.Type())
	require.ErrorIs(t, err, rag.ErrToolsetIndexRevisionSuperseded)
}
