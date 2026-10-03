package chat_test

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.temporal.io/api/serviceerror"

	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
)

const completionTitleStream = `data: {"id":"gen-title-test","model":"test-model","choices":[{"index":0,"delta":{"content":"hello"},"finish_reason":"stop"}]}

data: [DONE]

`

func TestHandleCompletion_SchedulesChatTitleGeneration(t *testing.T) {
	t.Parallel()

	for _, stream := range []bool{true, false} {
		t.Run(fmt.Sprintf("stream=%t", stream), func(t *testing.T) {
			t.Parallel()

			chatID := uuid.New()
			ti, req := newCompletionTitleRequest(t, stream, chatID)

			rec := httptest.NewRecorder()
			require.NoError(t, ti.service.HandleCompletion(rec, req))
			require.Equal(t, http.StatusOK, rec.Code)

			desc, err := ti.temporal.Client().DescribeWorkflowExecution(t.Context(), titleWorkflowID(chatID), "")
			require.NoError(t, err, "title generation workflow was not started")
			require.Equal(t, "GenerateChatTitleWorkflow", desc.GetWorkflowExecutionInfo().GetType().GetName())
		})
	}
}

func TestHandleCompletion_SkipCaptureDoesNotScheduleChatTitleGeneration(t *testing.T) {
	t.Parallel()

	chatID := uuid.New()
	ti, req := newCompletionTitleRequest(t, false, chatID)
	req.Header.Set("Gram-Skip-Capture", "1")

	rec := httptest.NewRecorder()
	require.NoError(t, ti.service.HandleCompletion(rec, req))
	require.Equal(t, http.StatusOK, rec.Code)

	_, err := ti.temporal.Client().DescribeWorkflowExecution(t.Context(), titleWorkflowID(chatID), "")
	var notFound *serviceerror.NotFound
	require.ErrorAs(t, err, &notFound)
}

func titleWorkflowID(chatID uuid.UUID) string {
	return "v1:generate-chat-title:" + chatID.String()
}

func newCompletionTitleRequest(t *testing.T, stream bool, chatID uuid.UUID) (*chatTestInstance, *http.Request) {
	t.Helper()

	completion := &mockCompletionClient{}
	completion.On("GetCompletionStream", mock.Anything, mock.Anything).
		Return(io.NopCloser(strings.NewReader(completionTitleStream)), nil)
	ti := newTestChatServiceWithCompletion(t, completion)

	ctx := authztest.InitAuthContext(t, t.Context(), ti.conn, ti.sessions)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.SessionID)
	require.NotNil(t, authCtx.ProjectSlug)

	body := fmt.Sprintf(`{"model":"test-model","stream":%t,"messages":[{"role":"user","content":"hi"}]}`, stream)
	req := httptest.NewRequestWithContext(ctx, http.MethodPost, "/chat/completions", strings.NewReader(body))
	req.Header.Set(constants.SessionHeader, *authCtx.SessionID)
	req.Header.Set(constants.ProjectHeader, *authCtx.ProjectSlug)
	req.Header.Set("Gram-Chat-ID", chatID.String())

	return ti, req
}
