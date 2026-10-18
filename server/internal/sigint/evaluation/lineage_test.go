package evaluation

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/infra/pkg/gcp"
)

func TestCopiedHistoryIsNotAnEvaluationTarget(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"reobserved", "synthetic", "unknown"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			m := message()
			m.SetCaptureKind(kind)
			h := NewConversationHandler(nil, nil, nil)
			require.NoError(t, h.Handle(t.Context(), m, gcp.MessageMetadata{}))
		})
	}
}

func TestConversationLineageIsPreserved(t *testing.T) {
	t.Parallel()
	m := message()
	m.SetCaptureKind("original")
	row := storedMessage(m)
	row.Generation, row.Seq = 3, 57
	event := (&conversationInput{message: m, stored: row}).Event()
	context := event.Subject.GetConversationMessage()
	require.EqualValues(t, 3, context.GetGeneration())
	require.EqualValues(t, 57, context.GetSequence())
	require.Equal(t, "original", context.GetCaptureKind())
}
