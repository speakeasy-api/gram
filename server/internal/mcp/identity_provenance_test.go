// Regression coverage for DNO-979 trusted provenance classification: the
// serving surfaces stamp mcpidentity at credential validation, and no
// non-user credential may ever read as an authoritative acting user.
package mcp_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	keys_gen "github.com/speakeasy-api/gram/server/gen/keys"
	"github.com/speakeasy-api/gram/server/internal/auth/assistanttokens"
	"github.com/speakeasy-api/gram/server/internal/auth/chatsessions"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/keys"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/mcpidentity"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// TestApplyIssuerGate_AssistantFallbackStampsAssistantProvenance drives the
// issuer gate's accepted assistant-runtime fallback end to end and proves the
// returned context carries KindAssistant provenance with no user ID — never
// KindUserSession — even though the fallback mints a user-shaped session
// subject and a user-shaped AuthContext for downstream plumbing.
func TestApplyIssuerGate_AssistantFallbackStampsAssistantProvenance(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	assistantID := createAssistant(t, ti, authCtx, "Provenance")
	token := mintAssistantToken(t, ti, authCtx, assistantID)

	endpoint := &mcp.ResolvedMcpEndpoint{
		AudienceURN:         urn.NewUserSessionIssuer(uuid.New()).String(),
		OrganizationID:      authCtx.ActiveOrganizationID,
		ProjectID:           *authCtx.ProjectID,
		RouteBase:           "mcp",
		Slug:                "provenance-gate",
		UserSessionIssuerID: uuid.New(),
	}

	w := httptest.NewRecorder()
	newCtx, _, _, err := ti.service.ApplyIssuerGate(t.Context(), w, token, "http://0.0.0.0", endpoint)
	require.NoError(t, err)

	identity, stamped := mcpidentity.FromContext(newCtx)
	require.True(t, stamped, "the accepted assistant fallback must stamp provenance")
	require.Equal(t, mcpidentity.KindAssistant, identity.Kind)
	require.Empty(t, identity.UserID)

	// The gate's AuthContext deliberately reads as the assistant's owning
	// user so downstream session plumbing works — which is exactly why the
	// provenance stamp, not the subject shape, is the enforcement-grade
	// signal that this caller is not an acting user.
	gateAuthCtx, ok := contextvalues.GetAuthContext(newCtx)
	require.True(t, ok)
	require.Equal(t, authCtx.UserID, gateAuthCtx.UserID)
}

// TestApplyIssuerGate_RejectedAssistantTokenStampsNothing pins that a
// cross-project assistant token is rejected with a 401 challenge and the
// returned context carries no provenance at all.
func TestApplyIssuerGate_RejectedAssistantTokenStampsNothing(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	assistantID := createAssistant(t, ti, authCtx, "CrossProject")
	token, err := assistanttokens.New("test-jwt-secret", ti.conn, ti.authzEngine).Generate(assistanttokens.GenerateInput{
		OrgID:       authCtx.ActiveOrganizationID,
		ProjectID:   uuid.New(),
		UserID:      authCtx.UserID,
		AssistantID: assistantID,
		ThreadID:    uuid.Nil,
		TTL:         time.Hour,
	})
	require.NoError(t, err)

	endpoint := &mcp.ResolvedMcpEndpoint{
		AudienceURN:         urn.NewUserSessionIssuer(uuid.New()).String(),
		OrganizationID:      authCtx.ActiveOrganizationID,
		ProjectID:           *authCtx.ProjectID,
		RouteBase:           "mcp",
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
// class each legacy authenticateToken strategy stamps at credential
// validation: assistant tokens, API keys of either accepted scope, and
// chat-session tokens. None of them may claim an acting user, and a token
// every strategy rejects leaves the context unattributed.
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

	t.Run("assistant token stamps assistant", func(t *testing.T) {
		t.Parallel()
		assistantID := createAssistant(t, ti, authCtx, "LegacyAuth")
		identity, stamped := authorize(t, mintAssistantToken(t, ti, authCtx, assistantID))
		require.True(t, stamped)
		require.Equal(t, mcpidentity.KindAssistant, identity.Kind)
		require.Empty(t, identity.UserID)
	})

	t.Run("consumer-scope API key stamps api_key", func(t *testing.T) {
		t.Parallel()
		identity, stamped := authorize(t, ti.createTestAPIKey(ctx, t))
		require.True(t, stamped)
		require.Equal(t, mcpidentity.KindAPIKey, identity.Kind)
		require.Empty(t, identity.UserID)
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
		require.Equal(t, mcpidentity.KindAPIKey, identity.Kind)
		require.Empty(t, identity.UserID)
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
		require.Equal(t, mcpidentity.KindChatSession, identity.Kind)
		require.Empty(t, identity.UserID)
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
