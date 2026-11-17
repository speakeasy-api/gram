package mcp_test

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	agentsrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	customdomainsrepo "github.com/speakeasy-api/gram/server/internal/customdomains/repo"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/requestorigin"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func mintInstallCode(t *testing.T, ti *testInstance, agentID, token string) (*httptest.ResponseRecorder, error) {
	t.Helper()
	return mintInstallCodeFrom(t, ti, "https://app.example.test", agentID, token)
}

func mintInstallCodeFrom(t *testing.T, ti *testInstance, origin, agentID, token string) (*httptest.ResponseRecorder, error) {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, "/agent-mcp/"+agentID+"/install-code", nil)
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("agentID", agentID)
	r = r.WithContext(context.WithValue(t.Context(), chi.RouteCtxKey, rctx))
	r = r.WithContext(requestorigin.WithContext(r.Context(), requestorigin.Origin{BaseURL: origin}))

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

// Fetching the script does not spend the code, so a second fetch returns the
// same key. An unknown code gets nothing. Expiry is the cache TTL.
func TestAgentInstall_CodeIsReusable(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	fx := seedAgentGateway(t, ctx, ti)

	minted, err := mintInstallCode(t, ti, fx.agent.ID.String(), fx.token)
	require.NoError(t, err)
	code := codeFrom(t, minted)

	first, err := fetchInstallScript(t, ti, code)
	require.NoError(t, err)
	require.Contains(t, first.Body.String(), fx.token)

	second, err := fetchInstallScript(t, ti, code)
	require.NoError(t, err)
	require.Contains(t, second.Body.String(), fx.token)

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

// The command fetches the script from the minting origin, so a plain-HTTP
// origin would send the code, a bearer of the key, in cleartext.
func TestAgentInstall_MintingRequiresAnHTTPSOrigin(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	fx := seedAgentGateway(t, ctx, ti)

	minted, err := mintInstallCodeFrom(t, ti, "http://app.example.test", fx.agent.ID.String(), fx.token)
	requireAgentGatewayCode(t, err, oops.CodeBadRequest)
	require.Empty(t, minted.Body.String(), "no code may be handed out")
}

// An organization locked to its custom domain by an IP allowlist cannot reach
// the gateway on the platform host, so the installed runtime is pointed at the
// domain once it is serving. Minting itself stays on the platform host,
// outside the allowlist.
func TestAgentInstall_AllowlistedCustomDomainIsTheInstalledGateway(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	fx := seedAgentGateway(t, ctx, ti)
	domain := seedAllowlistedDomain(t, ctx, ti, true)

	script := installScriptFor(t, ti, mintInstallCode, fx)
	require.Contains(t, script, "https://"+domain+"/agent-mcp/"+fx.agent.ID.String())
	require.NotContains(t, script, "https://app.example.test/agent-mcp/")
}

// A domain that is allowlisted but not yet verified and activated is refused
// by the custom-domain middleware, so quoting it would hand out an address
// that cannot work. The installed gateway stays on the platform host.
func TestAgentInstall_PendingAllowlistedDomainKeepsThePlatformGateway(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	fx := seedAgentGateway(t, ctx, ti)
	domain := seedAllowlistedDomain(t, ctx, ti, false)

	script := installScriptFor(t, ti, mintInstallCode, fx)
	require.Contains(t, script, "https://app.example.test/agent-mcp/"+fx.agent.ID.String())
	require.NotContains(t, script, domain)
}

// The custom-domain ingress serves /agent-mcp, so a code can be minted there
// too, but only the platform hosts serve /rpc/hooks.*. The hooks plugin must
// keep reporting to the platform.
func TestAgentInstall_CustomDomainMintKeepsHooksOnThePlatform(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPServiceWithPoolConfigAndTemporal(t, testenv.NewLogger(t), testenv.NewMeterProvider(t), &mockIdentityResolver{hasAccessOK: true}, mcp.TunnelPublicConfig{}, nil, nil, false, mcp.MetaRuntimeConfig{}, testenv.NewTracerProvider(t), nil, nil, "https://app.example.test")
	domain := seedAllowlistedDomain(t, ctx, ti, true)
	// The hooks plugin is only installed for an agent that records into a
	// project.
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	agent, err := agentsrepo.New(ti.conn).CreateAgent(ctx, agentsrepo.CreateAgentParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		OwnerUserID:    authCtx.UserID,
		ProjectID:      uuid.NullUUID{UUID: *authCtx.ProjectID, Valid: true},
		Name:           "Hooks agent " + uuid.NewString()[:8],
	})
	require.NoError(t, err)
	fx := agentGatewayFixture{agent: agent, token: mintAgentGatewayKey(t, ctx, ti, agent.ID, uuid.New()), granted: "", grantedProject: "", denied: "", open: ""}

	mintOnDomain := func(t *testing.T, ti *testInstance, agentID, token string) (*httptest.ResponseRecorder, error) {
		t.Helper()
		r := httptest.NewRequest(http.MethodPost, "/agent-mcp/"+agentID+"/install-code", nil)
		r.Header.Set("Authorization", "Bearer "+token)
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("agentID", agentID)
		r = r.WithContext(context.WithValue(t.Context(), chi.RouteCtxKey, rctx))
		r = r.WithContext(requestorigin.WithContext(r.Context(), requestorigin.Origin{Surface: requestorigin.SurfaceCustomDomain, BaseURL: "https://" + domain}))

		w := httptest.NewRecorder()
		if err := ti.service.HandleAgentInstallCode(w, r); err != nil {
			return w, fmt.Errorf("mint install code: %w", err)
		}
		return w, nil
	}

	script := installScriptFor(t, ti, mintOnDomain, fx)
	require.Contains(t, script, "https://"+domain+"/agent-mcp/"+fx.agent.ID.String())
	serverURL := openClawPluginServerURL(t, script)
	require.Equal(t, "https://app.example.test", serverURL)
}

// seedAllowlistedDomain gives the caller's organization a custom domain with an
// IP allowlist, serving or not, and returns its host.
func seedAllowlistedDomain(t *testing.T, ctx context.Context, ti *testInstance, serving bool) string {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	domains := customdomainsrepo.New(ti.conn)
	domain, err := domains.CreateCustomDomain(ctx, customdomainsrepo.CreateCustomDomainParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		Domain:         "agent-install-" + uuid.NewString()[:8] + ".example.com",
		IpAllowlist:    []string{"203.0.113.0/24"},
	})
	require.NoError(t, err)
	if serving {
		_, err = domains.UpdateCustomDomain(ctx, customdomainsrepo.UpdateCustomDomainParams{ID: domain.ID, Verified: true, Activated: true})
		require.NoError(t, err)
	}
	return domain.Domain
}

func installScriptFor(t *testing.T, ti *testInstance, mint func(*testing.T, *testInstance, string, string) (*httptest.ResponseRecorder, error), fx agentGatewayFixture) string {
	t.Helper()
	minted, err := mint(t, ti, fx.agent.ID.String(), fx.token)
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, minted.Code)

	script, err := fetchInstallScript(t, ti, codeFrom(t, minted))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, script.Code)
	return script.Body.String()
}

// openClawPluginServerURL decodes the OpenClaw plugin the script embeds and
// returns the server URL its hooks report to.
func openClawPluginServerURL(t *testing.T, script string) string {
	t.Helper()
	const marker = "cat <<'SPEAKEASY_OPENCLAW_PLUGIN'\n"
	start := strings.Index(script, marker)
	require.NotEqual(t, -1, start, "the script embeds the OpenClaw plugin")
	encoded, _, found := strings.Cut(script[start+len(marker):], "SPEAKEASY_OPENCLAW_PLUGIN")
	require.True(t, found, "the embedded plugin is terminated")
	encoded = strings.ReplaceAll(encoded, "\n", "")
	archive, err := base64.StdEncoding.DecodeString(encoded)
	require.NoError(t, err)

	reader, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	require.NoError(t, err)
	for _, file := range reader.File {
		if !strings.HasSuffix(file.Name, "speakeasy.json") {
			continue
		}
		rc, err := file.Open()
		require.NoError(t, err)
		var config struct {
			ServerURL string `json:"server_url"`
		}
		require.NoError(t, json.NewDecoder(rc).Decode(&config))
		require.NoError(t, rc.Close())
		return config.ServerURL
	}
	require.Fail(t, "the plugin carries speakeasy.json")
	return ""
}
