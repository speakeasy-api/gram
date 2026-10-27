package assistants

import (
	"encoding/json"
	"github.com/google/uuid"
	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/oauthtest"
	goahttp "goa.design/goa/v3/http"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/auth/assistanttokens"
)

func TestMCPAuthAttemptID(t *testing.T) {
	t.Parallel()

	claims := new(assistanttokens.MCPAuthFlowClaims)
	claims.FlowID = "stable-callback"
	claims.ID = "unique-attempt"
	require.Equal(t, "unique-attempt", mcpAuthAttemptID(claims))

	claims.ID = ""
	require.Equal(t, "stable-callback", mcpAuthAttemptID(claims))
}

func TestMCPAuthCallbackInvalidStateRendersPage(t *testing.T) {
	t.Parallel()
	service := newMCPAuthTestService(t, nil)
	service.core.assistantTokens = assistanttokens.New("test-secret", nil, nil, nil, nil)
	mux := goahttp.NewMuxer()
	Attach(mux, service)
	for _, token := range []string{"", "invalid-token"} {
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/rpc/assistantMcpAuth/flow/oauth/callback?state="+token, nil))
		require.NotEmpty(t, oauthtest.RequireAuthorizationErrorPage(t, response, http.StatusBadRequest).Code)
		require.Contains(t, response.Body.String(), "expired or was already used")
		require.Contains(t, response.Body.String(), "Speakeasy Assistant")
		require.NotContains(t, response.Body.String(), "invalid-token")
	}
}

func TestMCPAuthCallbackCancellationAndReplay(t *testing.T) {
	t.Parallel()
	conn, err := assistantsInfra.CloneTestDatabase(t, "assistant_callback_cancel")
	require.NoError(t, err)
	projectID, assistantID, _, threadID := insertAssistantFixture(t, conn)
	service := newMCPAuthTestService(t, conn)
	service.core.assistantTokens = assistanttokens.New("test-secret", conn, nil, nil, nil)
	signaler := &stubWorkflowSignaler{signalledThreads: nil}
	service.signaler = signaler
	state, err := service.core.assistantTokens.GenerateMCPAuthFlow(assistanttokens.MCPAuthFlowInput{
		ProjectID: projectID, AssistantID: assistantID, ThreadID: threadID,
		FlowID: "flow", AttemptID: uuid.NewString(), UserID: "test-user",
		McpURL: "https://example.test/mcp/example", TTL: time.Minute,
	})
	require.NoError(t, err)
	mux := goahttp.NewMuxer()
	Attach(mux, service)
	target := "/rpc/assistantMcpAuth/flow/oauth/callback?state=" + url.QueryEscape(state) + "&error=access_denied&error_description=private-provider-detail"
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, target, nil))
	require.NotEmpty(t, oauthtest.RequireAuthorizationErrorPage(t, response, http.StatusBadRequest).Code)
	require.Contains(t, response.Body.String(), "Sign-in was canceled")
	require.NotContains(t, response.Body.String(), "private-provider-detail")
	require.NotContains(t, response.Body.String(), "Authentication complete")
	replay := httptest.NewRecorder()
	mux.ServeHTTP(replay, httptest.NewRequest(http.MethodGet, target, nil))
	require.NotEmpty(t, oauthtest.RequireAuthorizationErrorPage(t, replay, http.StatusBadRequest).Code)
	require.Contains(t, replay.Body.String(), "already processed")
	require.Contains(t, replay.Body.String(), "expired or was already used")
}

func TestMCPAuthCallbackRejectedGrantRendersFailure(t *testing.T) {
	t.Parallel()
	conn, err := assistantsInfra.CloneTestDatabase(t, "assistant_callback_rejected_grant")
	require.NoError(t, err)
	projectID, assistantID, _, threadID := insertAssistantFixture(t, conn)
	service := newMCPAuthTestService(t, conn)
	service.core.assistantTokens = assistanttokens.New("test-secret", conn, nil, nil, nil)
	service.signaler = &stubWorkflowSignaler{signalledThreads: nil}
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant","error_description":"private-provider-detail"}`))
	}))
	t.Cleanup(provider.Close)
	verifier, err := service.core.encryptionClient.Encrypt([]byte("test-verifier"))
	require.NoError(t, err)
	state, err := service.core.assistantTokens.GenerateMCPAuthFlow(assistanttokens.MCPAuthFlowInput{
		ProjectID: projectID, AssistantID: assistantID, ThreadID: threadID,
		FlowID: "flow", AttemptID: uuid.NewString(), UserID: "test-user",
		McpURL: "https://example.test/mcp/example", TTL: time.Minute,
		CodeVerifier: verifier, TokenEndpoint: provider.URL, ClientID: "test-client",
		RedirectURI: "https://example.test/rpc/assistantMcpAuth/flow/oauth/callback",
	})
	require.NoError(t, err)
	mux := goahttp.NewMuxer()
	Attach(mux, service)
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/rpc/assistantMcpAuth/flow/oauth/callback?code=test-code&state="+url.QueryEscape(state), nil))
	require.NotEmpty(t, oauthtest.RequireAuthorizationErrorPage(t, response, http.StatusBadRequest).Code)
	require.NotContains(t, response.Body.String(), "Authentication complete")
	require.NotContains(t, response.Body.String(), "private-provider-detail")
	event, err := assistantrepo.New(conn).GetLatestAssistantThreadEventByThreadID(t.Context(), assistantrepo.GetLatestAssistantThreadEventByThreadIDParams{AssistantThreadID: threadID, ProjectID: projectID})
	require.NoError(t, err)
	var payload mcpAuthEventPayload
	require.NoError(t, json.Unmarshal(event.NormalizedPayloadJson, &payload))
	require.Equal(t, mcpAuthStatusFailed, payload.Status)
	require.Equal(t, "invalid_grant", payload.Error)
}
