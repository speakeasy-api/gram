package hostedinference

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestAsShareableErrorPreservesOnlyMatchedExternalNote(t *testing.T) {
	t.Parallel()
	note := "Paused by the organization."
	shareable, ok := AsShareableError(&MatchedDenialError{externalNote: note})
	require.True(t, ok)
	require.Equal(t, oops.CodeAIAccessDenied, shareable.Code)
	require.Equal(t, note, shareable.Error())
	require.True(t, shareable.SpanHandled(), "tenant-authored note must not be recorded by outer tracing")
	require.Equal(t, 403, shareable.HTTPStatus(t.Context()))
	require.Equal(t, string(oops.CodeAIAccessDenied), shareable.AsGoa(t.Context()).GoaErrorName())
	require.Equal(t, note, shareable.AsGoa(t.Context()).Error())
	require.NoError(t, shareable.Unwrap(), "matched notes must not be retained as a loggable cause")
}

func TestAsShareableErrorMakesInfrastructureGeneric(t *testing.T) {
	t.Parallel()
	cause := errors.New("database detail")
	shareable, ok := AsShareableError(newInfrastructureUnavailable(cause))
	require.True(t, ok)
	require.Equal(t, oops.CodeUnavailable, shareable.Code)
	require.Equal(t, oops.CodeUnavailable.UserMessage(), shareable.Error())
	require.NotContains(t, shareable.Error(), "match")
	require.NotContains(t, shareable.Error(), cause.Error())
	require.ErrorIs(t, shareable, cause)
}
