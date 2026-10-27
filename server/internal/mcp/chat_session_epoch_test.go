package mcp_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/auth/chatsessions"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestChatSessionEpoch_IdentityAuthPreservesServerError(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	ac, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	userID := uuid.NewString()
	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)
	require.NoError(t, redisClient.Set(ctx, "sessions:valid_after:"+userID, "corrupt-marker", time.Minute).Err())

	token, _, err := ti.chatSessionsManager.GenerateToken(ctx, chatsessions.ChatSessionClaims{
		OrgID: ac.ActiveOrganizationID, ProjectID: ac.ProjectID.String(), UserID: userID, SessionID: new(uuid.NewString()),
	}, "https://example.com", 3600)
	require.NoError(t, err)

	request := httptest.NewRequest(http.MethodPost, "/mcp/test", nil)
	request.Header.Set("Authorization", "Bearer "+token)
	_, err = ti.service.TryPublicIdentityAuth(t.Context(), request, false, uuid.Nil)
	var shareable *oops.ShareableError
	require.ErrorAs(t, err, &shareable)
	require.Equal(t, oops.CodeUnavailable, shareable.Code)

	response := httptest.NewRecorder()
	_, err = ti.service.RequirePrivateIdentityAuth(t.Context(), response, request, true, uuid.Nil, "https://example.com/.well-known/oauth-protected-resource")
	require.ErrorAs(t, err, &shareable)
	require.Equal(t, oops.CodeUnavailable, shareable.Code)
	require.Empty(t, response.Header().Get("WWW-Authenticate"))

	request.Header.Set("Authorization", "Bearer invalid-token")
	_, err = ti.service.RequirePrivateIdentityAuth(t.Context(), response, request, true, uuid.Nil, "https://example.com/.well-known/oauth-protected-resource")
	require.ErrorAs(t, err, &shareable)
	require.Equal(t, oops.CodeUnauthorized, shareable.Code)
	require.NotEmpty(t, response.Header().Get("WWW-Authenticate"))
}
