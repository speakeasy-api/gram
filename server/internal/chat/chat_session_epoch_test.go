package chat_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
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

func TestAuthorizeCompletion_RejectsAssistantRuntimeJWT(t *testing.T) {
	t.Parallel()

	ti := newTestChatService(t)
	ti.service.TestingCompletionTokens()
	forged, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"iss":          "gram-assistants",
		"org_id":       ti.orgID,
		"project_id":   ti.projectID.String(),
		"user_id":      "user-test",
		"assistant_id": uuid.NewString(),
		"exp":          time.Now().Add(time.Hour).Unix(),
	}).SignedString([]byte("test-secret"))
	require.NoError(t, err)

	for _, completion := range []bool{true, false} {
		request := httptest.NewRequest(http.MethodPost, "/chat/completions", nil)
		request.Header.Set("Authorization", "Bearer "+forged)
		_, _, _, err = ti.service.TestingAuthorizeCompletion(request, completion)
		var shareable *oops.ShareableError
		require.ErrorAs(t, err, &shareable)
		require.Equal(t, oops.CodeUnauthorized, shareable.Code)
	}
}
