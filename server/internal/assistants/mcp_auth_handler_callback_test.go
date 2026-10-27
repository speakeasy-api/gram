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
	"strings"
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
	service := newCIMDAuthTestService(t, nil)
	service.core.assistantTokens = assistanttokens.New("test-secret", nil, nil, nil, nil)
	mux := goahttp.NewMuxer()
	Attach(mux, service)
	for _, token := range []string{"", "invalid-token"} {
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/rpc/assistantMcpAuth/flow/oauth/callback?state="+token, nil))
		require.NotEmpty(t, oauthtest.RequireAuthorizationErrorRecovery(t, response, http.StatusBadRequest, "https://app.getgram.ai/@self/projects/default/assistants").Code)
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
	seedAssistantOrgMetadata(t, conn)
	service := newCIMDAuthTestService(t, conn)
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
	require.NotEmpty(t, oauthtest.RequireAuthorizationErrorRecovery(t, response, http.StatusBadRequest, "https://app.getgram.ai/acme/projects/project/assistants/"+assistantID.String()+"?tab=sessions").Code)
	require.Contains(t, response.Body.String(), "Sign-in was canceled")
	require.NotContains(t, response.Body.String(), "private-provider-detail")
	require.NotContains(t, response.Body.String(), "Authentication complete")
	replay := httptest.NewRecorder()
	mux.ServeHTTP(replay, httptest.NewRequest(http.MethodGet, target, nil))
	require.NotEmpty(t, oauthtest.RequireAuthorizationErrorRecovery(t, replay, http.StatusBadRequest, "https://app.getgram.ai/acme/projects/project/assistants/"+assistantID.String()+"?tab=sessions").Code)
	require.Contains(t, replay.Body.String(), "already processed")
	require.Contains(t, replay.Body.String(), "expired or was already used")
}

func TestMCPAuthCallbackRejectedGrantRendersFailure(t *testing.T) {
	t.Parallel()
	conn, err := assistantsInfra.CloneTestDatabase(t, "assistant_callback_rejected_grant")
	require.NoError(t, err)
	projectID, assistantID, _, threadID := insertAssistantFixture(t, conn)
	seedAssistantOrgMetadata(t, conn)
	service := newCIMDAuthTestService(t, conn)
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
	require.NotEmpty(t, oauthtest.RequireAuthorizationErrorRecovery(t, response, http.StatusBadRequest, "https://app.getgram.ai/acme/projects/project/assistants/"+assistantID.String()+"?tab=sessions").Code)
	require.NotContains(t, response.Body.String(), "Authentication complete")
	require.NotContains(t, response.Body.String(), "private-provider-detail")
	event, err := assistantrepo.New(conn).GetLatestAssistantThreadEventByThreadID(t.Context(), assistantrepo.GetLatestAssistantThreadEventByThreadIDParams{AssistantThreadID: threadID, ProjectID: projectID})
	require.NoError(t, err)
	var payload mcpAuthEventPayload
	require.NoError(t, json.Unmarshal(event.NormalizedPayloadJson, &payload))
	require.Equal(t, mcpAuthStatusFailed, payload.Status)
	require.Equal(t, "invalid_grant", payload.Error)
}

func TestMCPAuthCallbackRecoveryDestinations(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"setup", "dashboard", "external", "unmanaged", "wrong-assistant", "wrong-project", "missing-thread", "deleted-assistant", "wrong-flow"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			conn, err := assistantsInfra.CloneTestDatabase(t, "callback_recovery_"+strings.ReplaceAll(scenario, "-", "_"))
			require.NoError(t, err)
			projectID, assistantID, chatID, threadID := insertAssistantFixture(t, conn)
			seedAssistantOrgMetadata(t, conn)
			service := newCIMDAuthTestService(t, conn)
			service.core.assistantTokens = assistanttokens.New("test-secret", conn, nil, nil, nil)
			service.signaler = &stubWorkflowSignaler{signalledThreads: nil}
			destination := "https://app.getgram.ai/acme/projects/project/assistants/" + assistantID.String()
			label := "Open assistant"
			status := http.StatusBadRequest
			flowID := "flow"
			switch scenario {
			case "setup":
				err = assistantrepo.New(conn).SetKitThreadSourceFixture(t.Context(), assistantrepo.SetKitThreadSourceFixtureParams{ProjectID: projectID, ThreadID: threadID, SourceKind: sourceKindSetup})
				require.NoError(t, err)
				destination += "?threadId=" + chatID.String()
				label = "Return to conversation"
			case "dashboard", "unmanaged":
				err = assistantrepo.New(conn).SetKitThreadSourceFixture(t.Context(), assistantrepo.SetKitThreadSourceFixtureParams{ProjectID: projectID, ThreadID: threadID, SourceKind: sourceKindDashboard})
				require.NoError(t, err)
				if scenario == "dashboard" {
					require.NoError(t, assistantrepo.New(conn).CreateProjectManagedAssistant(t.Context(), assistantrepo.CreateProjectManagedAssistantParams{ProjectID: projectID, AssistantID: assistantID}))
					destination = "https://app.getgram.ai/acme/projects/project/chat/" + chatID.String()
					label = "Return to conversation"
				}
			case "external":
				destination += "?tab=sessions"
			default:
				destination = "https://app.getgram.ai/@self/projects/default/assistants"
				label = "Open assistants"
				switch scenario {
				case "wrong-assistant":
					assistantID = uuid.New()
					status = http.StatusForbidden
				case "wrong-project":
					projectID = uuid.New()
					status = http.StatusNotFound
				case "missing-thread":
					threadID = uuid.New()
					status = http.StatusNotFound
				case "deleted-assistant":
					require.NoError(t, assistantrepo.New(conn).DeleteAssistant(t.Context(), assistantrepo.DeleteAssistantParams{AssistantID: assistantID, ProjectID: projectID}))
				case "wrong-flow":
					flowID = "another-flow"
				}
			}
			state, err := service.core.assistantTokens.GenerateMCPAuthFlow(assistanttokens.MCPAuthFlowInput{
				ProjectID: projectID, AssistantID: assistantID, ThreadID: threadID,
				FlowID: flowID, AttemptID: uuid.NewString(), UserID: "test-user",
				McpURL: "https://example.test/mcp/example", TTL: time.Minute,
			})
			require.NoError(t, err)
			mux := goahttp.NewMuxer()
			Attach(mux, service)
			response := httptest.NewRecorder()
			mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/rpc/assistantMcpAuth/flow/oauth/callback?state="+url.QueryEscape(state)+"&error=access_denied&redirect_uri=https://untrusted.example/", nil))
			require.NotEmpty(t, oauthtest.RequireAuthorizationErrorRecovery(t, response, status, destination).Code)
			require.Contains(t, response.Body.String(), label)
		})
	}
}
