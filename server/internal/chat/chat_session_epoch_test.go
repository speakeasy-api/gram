package chat_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/auth/chatsessions"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestChatSessionEpoch_CompletionPreservesServerError(t *testing.T) {
	t.Parallel()

	ti := newTestChatService(t)
	ti.service.TestingCompletionTokens()
	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)

	userID := uuid.NewString()
	require.NoError(t, redisClient.Set(t.Context(), "sessions:valid_after:"+userID, "corrupt-marker", time.Minute).Err())

	manager := chatsessions.NewManager(testenv.NewLogger(t), redisClient, "test-secret", ti.sessions)
	ti.service.TestingSetChatSessions(manager)
	token, _, err := manager.GenerateToken(t.Context(), chatsessions.ChatSessionClaims{
		OrgID: ti.orgID, ProjectID: ti.projectID.String(), UserID: userID, SessionID: new(uuid.NewString()),
	}, "https://example.com", 3600)
	require.NoError(t, err)

	request := httptest.NewRequest(http.MethodPost, "/rpc/chat.completions", nil)
	request.Header.Set(constants.ChatSessionsTokenHeader, token)
	_, _, _, err = ti.service.TestingAuthorizeCompletion(request, false)
	var shareable *oops.ShareableError
	require.ErrorAs(t, err, &shareable)
	require.Equal(t, oops.CodeUnavailable, shareable.Code)
}
