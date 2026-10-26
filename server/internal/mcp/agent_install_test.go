package mcp_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/requestorigin"
)

func mintInstallCode(t *testing.T, ti *testInstance, agentID, token string) (*httptest.ResponseRecorder, error) {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, "/agent-mcp/"+agentID+"/install-code", nil)
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("agentID", agentID)
	r = r.WithContext(context.WithValue(t.Context(), chi.RouteCtxKey, rctx))
	r = r.WithContext(requestorigin.WithContext(r.Context(), requestorigin.Origin{BaseURL: "https://app.example.test"}))

	w := httptest.NewRecorder()
	err := ti.service.HandleAgentInstallCode(w, r)
	if err != nil {
		return w, fmt.Errorf("mint install code: %w", err)
	}
	return w, nil
}

func fetchInstallScript(t *testing.T, ti *testInstance, code string) (*httptest.ResponseRecorder, error) {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, "/agent-mcp/install/"+code, nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("code", code)
	r = r.WithContext(context.WithValue(t.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	err := ti.service.HandleAgentInstallScript(w, r)
	if err != nil {
		return w, fmt.Errorf("fetch install script: %w", err)
	}
	return w, nil
}

func codeFrom(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var minted struct {
		Code string `json:"code"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &minted))
	require.NotEmpty(t, minted.Code)
	return minted.Code
}

// The install command carries a short-lived credential redeemable for the
// agent key, rather than placing the long-lived key directly in the command.
func TestAgentInstall_CodeExchangesForAScriptCarryingTheKey(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	fx := seedAgentGateway(t, ctx, ti)

	minted, err := mintInstallCode(t, ti, fx.agent.ID.String(), fx.token)
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, minted.Code)
	code := codeFrom(t, minted)
	require.NotContains(t, code, fx.token, "the code must not embed the key")

	script, err := fetchInstallScript(t, ti, code)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, script.Code)
	require.Contains(t, script.Body.String(), fx.token)
	require.Contains(t, script.Body.String(), "/agent-mcp/"+fx.agent.ID.String())
	// A response carrying a live credential must not be stored on the way back.
	require.Equal(t, "no-store", script.Result().Header.Get("Cache-Control"))
}

// A code can be redeemed once: the second fetch gets nothing, and an unknown
// code is indistinguishable from a spent one.
func TestAgentInstall_CodeIsSingleUse(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	fx := seedAgentGateway(t, ctx, ti)

	minted, err := mintInstallCode(t, ti, fx.agent.ID.String(), fx.token)
	require.NoError(t, err)
	code := codeFrom(t, minted)

	_, err = fetchInstallScript(t, ti, code)
	require.NoError(t, err)

	_, err = fetchInstallScript(t, ti, code)
	requireAgentGatewayCode(t, err, oops.CodeNotFound)

	_, err = fetchInstallScript(t, ti, "never-minted")
	requireAgentGatewayCode(t, err, oops.CodeNotFound)
}

// Minting is gated on the key, so only the holder of a live key for that agent
// can turn it into an install command.
func TestAgentInstall_MintingRequiresThatAgentsKey(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	fx := seedAgentGateway(t, ctx, ti)
	other := seedAgentGateway(t, ctx, ti)

	_, err := mintInstallCode(t, ti, fx.agent.ID.String(), "")
	requireAgentGatewayCode(t, err, oops.CodeUnauthorized)

	_, err = mintInstallCode(t, ti, fx.agent.ID.String(), other.token)
	requireAgentGatewayCode(t, err, oops.CodeNotFound)
}
