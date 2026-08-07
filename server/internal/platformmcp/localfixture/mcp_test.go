package localfixture

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMCPHTTPRequiresLiveBearerAndServesInitializeAndToolsList(t *testing.T) {
	t.Parallel()

	origin, err := url.Parse("https://localhost:8080")
	require.NoError(t, err)
	config, err := NewConfig(origin)
	require.NoError(t, err)
	oauth := NewOAuthHTTP(config)
	oauthHandler := oauth.Handler()
	clientID := registerFixtureClient(t, oauthHandler, config)
	code := authorizeFixtureClient(t, oauthHandler, config, clientID, "fixture-mcp-verifier")
	tokens := exchangeFixtureCode(t, oauthHandler, config, clientID, code, "fixture-mcp-verifier")
	accessToken, ok := tokens["access_token"].(string)
	require.True(t, ok)

	handler := NewMCPHTTP(oauth).Handler()
	unauthorized := mcpRequest(t, handler, "", "", 1, "initialize")
	require.Equal(t, http.StatusUnauthorized, unauthorized.Code)
	require.Equal(t, "no-store", unauthorized.Header().Get("Cache-Control"))
	require.Contains(t, unauthorized.Header().Get("WWW-Authenticate"), "resource_metadata")

	standaloneSSE := httptest.NewRequest(http.MethodGet, "/platform-mcp/local-fixture/mcp", nil)
	standaloneSSE.Header.Set("Authorization", "Bearer "+accessToken)
	standaloneSSE.Header.Set("Accept", "text/event-stream")
	standaloneSSEResponse := httptest.NewRecorder()
	handler.ServeHTTP(standaloneSSEResponse, standaloneSSE)
	require.Equal(t, http.StatusMethodNotAllowed, standaloneSSEResponse.Code)

	initialize := mcpRequest(t, handler, accessToken, "", 1, "initialize")
	require.Equal(t, http.StatusOK, initialize.Code, initialize.Body.String())
	sessionID := initialize.Header().Get(fixtureMCPSessionHeader)
	require.NotEmpty(t, sessionID)
	var initializeResponse struct {
		Result map[string]any `json:"result"`
	}
	require.NoError(t, json.Unmarshal(initialize.Body.Bytes(), &initializeResponse))
	serverInfo, ok := initializeResponse.Result["serverInfo"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, CanonicalRef, serverInfo["name"])

	initialized := httptest.NewRequest(http.MethodPost, "/platform-mcp/local-fixture/mcp", strings.NewReader(`{"jsonrpc":"2.0","method":"notifications/initialized"}`))
	initialized.Header.Set("Authorization", "Bearer "+accessToken)
	initialized.Header.Set(fixtureMCPSessionHeader, sessionID)
	initialized.Header.Set("Content-Type", "application/json")
	initializedResponse := httptest.NewRecorder()
	handler.ServeHTTP(initializedResponse, initialized)
	require.Equal(t, http.StatusAccepted, initializedResponse.Code)

	unknownSession := mcpRequest(t, handler, accessToken, "unknown", 2, "tools/list")
	require.Equal(t, http.StatusBadRequest, unknownSession.Code)

	toolsList := mcpRequest(t, handler, accessToken, sessionID, 2, "tools/list")
	require.Equal(t, http.StatusOK, toolsList.Code, toolsList.Body.String())
	var listResponse struct {
		Result struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		} `json:"result"`
	}
	require.NoError(t, json.Unmarshal(toolsList.Body.Bytes(), &listResponse))
	require.Len(t, listResponse.Result.Tools, 1)
	require.Equal(t, fixtureToolName, listResponse.Result.Tools[0].Name)
}

func TestMCPHTTPRejectsRevokedBearer(t *testing.T) {
	t.Parallel()

	origin, err := url.Parse("https://localhost:8080")
	require.NoError(t, err)
	config, err := NewConfig(origin)
	require.NoError(t, err)
	oauth := NewOAuthHTTP(config)
	oauthHandler := oauth.Handler()
	clientID := registerFixtureClient(t, oauthHandler, config)
	code := authorizeFixtureClient(t, oauthHandler, config, clientID, "fixture-revoke-verifier")
	tokens := exchangeFixtureCode(t, oauthHandler, config, clientID, code, "fixture-revoke-verifier")
	accessToken, ok := tokens["access_token"].(string)
	require.True(t, ok)

	revoke := postForm(t, oauthHandler, "/"+fixtureOAuthPath+"/revoke", url.Values{"token": {accessToken}})
	require.Equal(t, http.StatusOK, revoke.Code)

	response := mcpRequest(t, NewMCPHTTP(oauth).Handler(), accessToken, "", 1, "initialize")
	require.Equal(t, http.StatusUnauthorized, response.Code)
}

func mcpRequest(t *testing.T, handler http.Handler, accessToken, sessionID string, id int, method string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/platform-mcp/local-fixture/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":`+strconv.Itoa(id)+`,"method":"`+method+`"}`))
	request.Header.Set("Content-Type", "application/json")
	if accessToken != "" {
		request.Header.Set("Authorization", "Bearer "+accessToken)
	}
	if sessionID != "" {
		request.Header.Set(fixtureMCPSessionHeader, sessionID)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}
