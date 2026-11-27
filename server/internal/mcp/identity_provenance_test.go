// Regression coverage for DNO-979 trusted provenance classification: the
// serving surfaces stamp mcpidentity at credential validation, and no
// non-user credential may ever read as an authoritative acting user.
package mcp_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	keys_gen "github.com/speakeasy-api/gram/server/gen/keys"
	"github.com/speakeasy-api/gram/server/internal/auth/chatsessions"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/keys"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcproute"
	"github.com/speakeasy-api/gram/server/internal/mcpidentity"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func mintForgedAssistantRuntimeJWT(t *testing.T, authCtx *contextvalues.AuthContext, assistantID uuid.UUID) string {
	t.Helper()
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"iss":          "gram-assistants",
		"sub":          assistantID.String(),
		"org_id":       authCtx.ActiveOrganizationID,
		"project_id":   authCtx.ProjectID.String(),
		"user_id":      authCtx.UserID,
		"assistant_id": assistantID.String(),
		"exp":          time.Now().Add(time.Hour).Unix(),
	}).SignedString([]byte("test-jwt-secret"))
	require.NoError(t, err)
	return token
}

// TestApplyIssuerGate_RejectsAssistantRuntimeJWT proves that an HS256
// gram-assistants JWT is rejected at the issuer gate and leaves the context
// unattributed.
func TestApplyIssuerGate_RejectsAssistantRuntimeJWT(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	assistantID := createAssistant(t, ti, authCtx, "Provenance")
	token := mintForgedAssistantRuntimeJWT(t, authCtx, assistantID)

	endpoint := &mcp.ResolvedMcpEndpoint{
		AudienceURN:         urn.NewUserSessionIssuer(uuid.New()).String(),
		OrganizationID:      authCtx.ActiveOrganizationID,
		ProjectID:           *authCtx.ProjectID,
		RouteBase:           mcproute.MCP,
		Slug:                "provenance-gate-reject",
		UserSessionIssuerID: uuid.New(),
	}

	w := httptest.NewRecorder()
	newCtx, _, _, err := ti.service.ApplyIssuerGate(t.Context(), w, token, "http://0.0.0.0", endpoint)
	require.Error(t, err)

	_, stamped := mcpidentity.FromContext(newCtx)
	require.False(t, stamped, "a rejected credential must leave the context unattributed")
}

// TestTryPublicIdentityAuth_StampsCredentialProvenance pins the provenance
// class each authenticateToken strategy stamps at credential validation: API
// keys of either accepted scope and chat-session tokens. None of them may
// claim an acting user, and a token every strategy rejects leaves the context
// unattributed.
func TestTryPublicIdentityAuth_StampsCredentialProvenance(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectSlug)

	authorize := func(t *testing.T, token string) (mcpidentity.Identity, bool) {
		t.Helper()
		r := httptest.NewRequest(http.MethodPost, "/mcp/provenance", nil)
		r.Header.Set("Authorization", "Bearer "+token)
		authedCtx, err := ti.service.TryPublicIdentityAuth(t.Context(), r, false, uuid.Nil)
		require.NoError(t, err)
		return mcpidentity.FromContext(authedCtx)
	}

	t.Run("assistant runtime JWT is rejected", func(t *testing.T) {
		t.Parallel()
		assistantID := createAssistant(t, ti, authCtx, "RejectedAuth")
		r := httptest.NewRequest(http.MethodPost, "/mcp/provenance", nil)
		r.Header.Set("Authorization", "Bearer "+mintForgedAssistantRuntimeJWT(t, authCtx, assistantID))
		authedCtx, err := ti.service.TryPublicIdentityAuth(t.Context(), r, false, uuid.Nil)
		require.Error(t, err)
		_, stamped := mcpidentity.FromContext(authedCtx)
		require.False(t, stamped)
	})

	t.Run("consumer-scope API key stamps api_key", func(t *testing.T) {
		t.Parallel()
		identity, stamped := authorize(t, ti.createTestAPIKey(ctx, t))
		require.True(t, stamped)
		require.Equal(t, mcpidentity.KindAPIKey, identity.Kind())
		require.Empty(t, identity.UserID())
	})

	t.Run("chat-scope API key stamps api_key", func(t *testing.T) {
		t.Parallel()
		keysService := keys.NewService(ti.logger, ti.tracerProvider, ti.conn, ti.sessionManager, "local", ti.authzEngine, ti.audit)
		key, err := keysService.CreateKey(ctx, &keys_gen.CreateKeyPayload{
			Name:   "chat-scope-key",
			Scopes: []string{"chat"},
		})
		require.NoError(t, err)

		identity, stamped := authorize(t, *key.Key)
		require.True(t, stamped)
		require.Equal(t, mcpidentity.KindAPIKey, identity.Kind())
		require.Empty(t, identity.UserID())
	})

	t.Run("chat-session token stamps chat_session", func(t *testing.T) {
		t.Parallel()
		token, _, err := ti.chatSessionsManager.GenerateToken(t.Context(), chatsessions.ChatSessionClaims{
			OrgID:            authCtx.ActiveOrganizationID,
			ProjectID:        authCtx.ProjectID.String(),
			OrganizationSlug: authCtx.OrganizationSlug,
			ProjectSlug:      *authCtx.ProjectSlug,
		}, "http://localhost", 3600)
		require.NoError(t, err)

		identity, stamped := authorize(t, token)
		require.True(t, stamped)
		require.Equal(t, mcpidentity.KindChatSession, identity.Kind())
		require.Empty(t, identity.UserID())
		require.Empty(t, identity.ChatSessionMinterUserID())
	})

	t.Run("dashboard-minted chat-session token records its minter", func(t *testing.T) {
		t.Parallel()
		sessionID := "dashboard-session"
		token, _, err := ti.chatSessionsManager.GenerateToken(t.Context(), chatsessions.ChatSessionClaims{
			OrgID:            authCtx.ActiveOrganizationID,
			ProjectID:        authCtx.ProjectID.String(),
			OrganizationSlug: authCtx.OrganizationSlug,
			ProjectSlug:      *authCtx.ProjectSlug,
			UserID:           authCtx.UserID,
			SessionID:        &sessionID,
		}, "http://localhost", 3600)
		require.NoError(t, err)

		identity, stamped := authorize(t, token)
		require.True(t, stamped)
		require.Equal(t, mcpidentity.KindChatSession, identity.Kind())
		require.Empty(t, identity.UserID(), "a chat session is never a user session")
		require.Equal(t, authCtx.UserID, identity.ChatSessionMinterUserID())
	})

	t.Run("key-minted chat-session token records no minter", func(t *testing.T) {
		t.Parallel()
		sessionID := "not-a-dashboard-session"
		token, _, err := ti.chatSessionsManager.GenerateToken(t.Context(), chatsessions.ChatSessionClaims{
			OrgID:            authCtx.ActiveOrganizationID,
			ProjectID:        authCtx.ProjectID.String(),
			OrganizationSlug: authCtx.OrganizationSlug,
			ProjectSlug:      *authCtx.ProjectSlug,
			APIKeyID:         uuid.NewString(),
			UserID:           authCtx.UserID,
			SessionID:        &sessionID,
		}, "http://localhost", 3600)
		require.NoError(t, err)

		identity, stamped := authorize(t, token)
		require.True(t, stamped)
		require.Equal(t, mcpidentity.KindChatSession, identity.Kind())
		require.Empty(t, identity.ChatSessionMinterUserID())
	})

	t.Run("rejected token stays unattributed", func(t *testing.T) {
		t.Parallel()
		r := httptest.NewRequest(http.MethodPost, "/mcp/provenance", nil)
		r.Header.Set("Authorization", "Bearer not-a-real-credential")
		authedCtx, err := ti.service.TryPublicIdentityAuth(t.Context(), r, false, uuid.Nil)
		require.Error(t, err)

		_, stamped := mcpidentity.FromContext(authedCtx)
		require.False(t, stamped)
	})
}
