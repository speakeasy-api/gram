package mcp_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	agentsrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/agents/runtimepolicy"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/feature"
	keysrepo "github.com/speakeasy-api/gram/server/internal/keys/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/requestorigin"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// mintInstallCode drives the install-code handler. An empty body is the MCP
// flavor; ctx carries the request origin.
func mintInstallCode(t *testing.T, ctx context.Context, ti *testInstance, agentID, token, body string) (*httptest.ResponseRecorder, error) {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	r := httptest.NewRequest(http.MethodPost, "/agent-mcp/"+agentID+"/install-code", reader)
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("agentID", agentID)
	r = r.WithContext(context.WithValue(ctx, chi.RouteCtxKey, rctx))

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

// The point of the code: the install command carries no credential, and the
// script it fetches carries the real one.
func TestAgentInstall_CodeExchangesForAScriptCarryingTheKey(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	fx := seedAgentGateway(t, ctx, ti)

	minted, err := mintInstallCode(t, t.Context(), ti, fx.agent.ID.String(), fx.token, "")
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

// Single use is what makes a leaked command harmless: the second fetch gets
// nothing, and an unknown code is indistinguishable from a spent one.
func TestAgentInstall_CodeIsSingleUse(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	fx := seedAgentGateway(t, ctx, ti)

	minted, err := mintInstallCode(t, t.Context(), ti, fx.agent.ID.String(), fx.token, "")
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

	_, err := mintInstallCode(t, t.Context(), ti, fx.agent.ID.String(), "", "")
	requireAgentGatewayCode(t, err, oops.CodeUnauthorized)

	_, err = mintInstallCode(t, t.Context(), ti, fx.agent.ID.String(), other.token, "")
	requireAgentGatewayCode(t, err, oops.CodeNotFound)
}

// deviceAgentKey is a seeded agent key.
type deviceAgentKey struct {
	agentID string
	token   string
}

// seedDeviceAgentKey stores an agent key whose policy is exactly grants. The
// agent and its owner hold every grant, so admission keeps them all.
func seedDeviceAgentKey(t *testing.T, ctx context.Context, ti *testInstance, grants []authz.Grant) deviceAgentKey {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	ti.features.SetFlag(feature.FlagAgentManagement, authCtx.ActiveOrganizationID, true)
	ti.features.SetFlag(feature.FlagAgentIdentityCredentials, authCtx.ActiveOrganizationID, true)

	agent, err := agentsrepo.New(ti.conn).CreateAgent(ctx, agentsrepo.CreateAgentParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		OwnerUserID:    authCtx.UserID,
		Name:           "Device agent " + uuid.NewString()[:8],
	})
	require.NoError(t, err)
	actor := urn.NewPrincipal(urn.PrincipalTypeAgent, agent.ID.String())

	for _, grant := range grants {
		selectors, err := grant.Selector.MarshalJSON()
		require.NoError(t, err)
		for _, principal := range []urn.Principal{actor, urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID)} {
			_, err = accessrepo.New(ti.conn).UpsertPrincipalGrant(ctx, accessrepo.UpsertPrincipalGrantParams{
				OrganizationID: authCtx.ActiveOrganizationID,
				PrincipalUrn:   principal,
				Scope:          string(grant.Scope),
				Selectors:      selectors,
			})
			require.NoError(t, err)
		}
	}

	policy, err := runtimepolicy.NewDelegatedPolicy(runtimepolicy.CurrentDelegatedPolicyVersion, grants)
	require.NoError(t, err)
	raw, err := runtimepolicy.EncodeDelegatedPolicy(runtimepolicy.CurrentDelegatedPolicyVersion, policy)
	require.NoError(t, err)
	token, hash, prefix, err := auth.GenerateAPIKeyMaterial(auth.APIKeyPrefix("test"))
	require.NoError(t, err)
	_, err = keysrepo.New(ti.conn).CreateAgentAPIKey(ctx, keysrepo.CreateAgentAPIKeyParams{
		OrganizationID:         authCtx.ActiveOrganizationID,
		CreatedByUserID:        authCtx.UserID,
		Name:                   "device agent key " + uuid.NewString()[:8],
		KeyPrefix:              prefix,
		KeyHash:                hash,
		SubjectUrn:             conv.ToPGText(urn.NewAgentSubject(agent.ID).String()),
		DelegatedGrants:        raw,
		DelegatedGrantsVersion: pgtype.Int4{Int32: int32(runtimepolicy.CurrentDelegatedPolicyVersion), Valid: true},
		ExpiresAt:              pgtype.Timestamptz{Time: time.Now().Add(time.Hour), InfinityModifier: pgtype.Finite, Valid: true},
	})
	require.NoError(t, err)
	return deviceAgentKey{agentID: agent.ID.String(), token: token}
}

// deviceAgentGrants are the grants a device agent key is issued.
func deviceAgentGrants(ctx context.Context, t *testing.T) []authz.Grant {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	return []authz.Grant{
		authz.NewGrant(authz.ScopeOrgDeviceAgentSync, authCtx.ActiveOrganizationID),
		authz.NewGrant(authz.ScopeOrgHooksIngest, authCtx.ActiveOrganizationID),
		authz.NewGrant(authz.ScopeProjectRead, authCtx.ProjectID.String()),
	}
}

// httpsOrigin is a request on an HTTPS platform host.
func httpsOrigin(t *testing.T) context.Context {
	t.Helper()
	return requestorigin.WithContext(t.Context(), requestorigin.Origin{
		Surface:          requestorigin.SurfacePlatform,
		BaseURL:          "https://gram.example.test",
		OrganizationID:   "",
		NetworkIngressID: uuid.Nil,
		NetworkIdentity:  nil,
	})
}

func TestAgentInstall_DeviceAgentCodeExchangesForAnEnrollmentScript(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	key := seedDeviceAgentKey(t, ctx, ti, deviceAgentGrants(ctx, t))

	minted, err := mintInstallCode(t, httpsOrigin(t), ti, key.agentID, key.token, `{"flavor":"device_agent","mode":"service"}`)
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, minted.Code)

	script, err := fetchInstallScript(t, ti, codeFrom(t, minted))
	require.NoError(t, err)
	body := script.Body.String()
	require.Contains(t, body, `"agent_key": "`+key.token+`"`)
	require.Contains(t, body, `"_control_plane_url": "https://gram.example.test"`)
	require.Contains(t, body, "-service install")
	require.NotContains(t, body, "claude mcp add", "a device agent key must not configure MCP clients")
	require.Equal(t, "no-store", script.Result().Header.Get("Cache-Control"))
}

// A key that can reach MCP servers must not be written to a host.
func TestAgentInstall_DeviceAgentRefusesAKeyThatCanConnectToMCP(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	grants := append(deviceAgentGrants(ctx, t), authz.NewGrant(authz.ScopeMCPConnect, uuid.NewString()))
	key := seedDeviceAgentKey(t, ctx, ti, grants)

	_, err := mintInstallCode(t, httpsOrigin(t), ti, key.agentID, key.token, `{"flavor":"device_agent","mode":"ephemeral"}`)
	requireAgentGatewayCode(t, err, oops.CodeForbidden)
}

func TestAgentInstall_DeviceAgentRequiresTheSyncGrant(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	key := seedDeviceAgentKey(t, ctx, ti, []authz.Grant{authz.NewGrant(authz.ScopeProjectRead, authCtx.ProjectID.String())})

	_, err := mintInstallCode(t, httpsOrigin(t), ti, key.agentID, key.token, `{"flavor":"device_agent","mode":"ephemeral"}`)
	requireAgentGatewayCode(t, err, oops.CodeForbidden)
}

// The test server is plain HTTP, and there is no loopback exception.
func TestAgentInstall_DeviceAgentRefusesAPlaintextControlPlane(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	key := seedDeviceAgentKey(t, ctx, ti, deviceAgentGrants(ctx, t))

	_, err := mintInstallCode(t, t.Context(), ti, key.agentID, key.token, `{"flavor":"device_agent","mode":"ephemeral"}`)
	requireAgentGatewayCode(t, err, oops.CodeFailedPrecondition)
}

func TestAgentInstall_RejectsMalformedRequests(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	key := seedDeviceAgentKey(t, ctx, ti, deviceAgentGrants(ctx, t))

	for _, tc := range []struct {
		name string
		body string
	}{
		{name: "unknown flavor", body: `{"flavor":"desktop"}`},
		{name: "device agent without mode", body: `{"flavor":"device_agent"}`},
		{name: "unknown mode", body: `{"flavor":"device_agent","mode":"daemon"}`},
		{name: "mode on mcp flavor", body: `{"flavor":"mcp","mode":"service"}`},
		{name: "unknown field", body: `{"flavor":"mcp","extra":true}`},
		{name: "trailing data", body: `{"flavor":"mcp"} {"flavor":"device_agent"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := mintInstallCode(t, httpsOrigin(t), ti, key.agentID, key.token, tc.body)
			requireAgentGatewayCode(t, err, oops.CodeBadRequest)
		})
	}
}
