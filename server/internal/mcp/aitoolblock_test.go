package mcp_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	agentrepo "github.com/speakeasy-api/gram/server/internal/agent/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	usersessions_repo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// claudeCodeCIMDClientID is a real entry in the CIMD admission catalog, which
// is what makes it nameable: blocking is CIMD-only, so a target is reachable
// only through the catalog entry that admitted the caller.
const claudeCodeCIMDClientID = "https://claude.ai/oauth/claude-code-client-metadata"

const cimdLoopbackRedirect = "http://127.0.0.1:9876/callback"

// seedCIMDClient persists a CIMD-resolved client row with a fresh cache
// window, so resolveUserSessionClient serves it from the row and never
// reaches out for the document. Returns the row.
func seedCIMDClient(t *testing.T, ctx context.Context, ti *testInstance, issuerID uuid.UUID, clientID string) usersessions_repo.UserSessionClient {
	t.Helper()

	client, err := usersessions_repo.New(ti.conn).UpsertUserSessionClientFromCIMD(ctx, usersessions_repo.UpsertUserSessionClientFromCIMDParams{
		UserSessionIssuerID:     issuerID,
		ClientID:                clientID,
		ClientName:              "Claude Code",
		RedirectUris:            []string{cimdLoopbackRedirect},
		CacheTtlSeconds:         3600,
		ClientIDMetadataEtag:    conv.ToPGTextEmpty(""),
		TokenEndpointAuthMethod: "none",
		ClientJwks:              nil,
		ClientJwksUri:           conv.ToPGTextEmpty(""),
	})
	require.NoError(t, err)
	return client
}

// blockTarget records a block for one catalog target id.
func blockTarget(t *testing.T, ctx context.Context, ti *testInstance, organizationID string, targetID string) {
	t.Helper()

	_, err := agentrepo.New(ti.conn).SetAIScanTargetStatus(ctx, agentrepo.SetAIScanTargetStatusParams{
		OrganizationID: organizationID,
		ID:             targetID,
		Status:         "blocked",
		Rationale:      conv.ToPGTextEmpty("not approved"),
	})
	require.NoError(t, err)
}

func authorizeRequest(t *testing.T, mcpSlug string, clientID string, redirectURI string) *http.Request {
	t.Helper()

	q := url.Values{
		"response_type":         {"code"},
		"client_id":             {clientID},
		"redirect_uri":          {redirectURI},
		"code_challenge":        {"E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"},
		"code_challenge_method": {"S256"},
	}
	req := httptest.NewRequest(http.MethodGet, "/mcp/"+mcpSlug+"/authorize?"+q.Encode(), nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("mcpSlug", mcpSlug)
	return req.WithContext(context.WithValue(t.Context(), chi.RouteCtxKey, rctx))
}

// TestHandleAuthorize_BlockedAITool_RefusedBeforeATokenExists is the point of
// the whole enforcement half: the block lands at the OAuth boundary, where
// the caller's identity is a credential the server verified, rather than at
// tools/call where it would only be a name the client claimed.
//
// It runs against the shipped default catalog — claude-code carries the
// "anthropic" vendor key — so a regression in the defaults fails here too.
func TestHandleAuthorize_BlockedAITool_RefusedBeforeATokenExists(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	toolset, issuer, _ := seedPrivateToolsetWithIssuer(t, ctx, ti)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	seedCIMDClient(t, ctx, ti, issuer.ID, claudeCodeCIMDClientID)
	blockTarget(t, ctx, ti, authCtx.ActiveOrganizationID, "claude-code")

	w := httptest.NewRecorder()
	require.NoError(t, ti.service.HandleAuthorize(w, authorizeRequest(t, toolset.McpSlug.String, claudeCodeCIMDClientID, cimdLoopbackRedirect)))

	require.Equal(t, http.StatusUnauthorized, w.Code)
	var body map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Equal(t, "invalid_client", body["error"])
	// The denial names the tool and says an administrator has to act. It is
	// the end user's only clue: their client picked this credential at
	// discovery and will not fall back to registration when authorize refuses
	// it.
	require.Contains(t, body["error_description"], "Claude Code")
	require.Contains(t, body["error_description"], "administrator must approve")
	// Deliberately no request-access link: the only self-service flow grants
	// an MCP role and cannot clear the tool's block, so offering it sent the
	// user to a form whose success changed nothing.
	require.NotContains(t, body["error_description"], "request-access")
}

// TestHandleAuthorize_UnblockedAITool_ConnectsNormally pins the other half of
// the rule: a decision recorded for one tool must not touch any other client.
func TestHandleAuthorize_UnblockedAITool_ConnectsNormally(t *testing.T) {
	t.Parallel()

	idpURL, err := url.Parse("https://idp.example.com/authorize?state=challenge123")
	require.NoError(t, err)
	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{buildAuthURLResult: idpURL})
	toolset, issuer, _ := seedPrivateToolsetWithIssuer(t, ctx, ti)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	seedCIMDClient(t, ctx, ti, issuer.ID, claudeCodeCIMDClientID)
	// A block exists in the organization, for a different tool.
	blockTarget(t, ctx, ti, authCtx.ActiveOrganizationID, "cursor")

	w := httptest.NewRecorder()
	require.NoError(t, ti.service.HandleAuthorize(w, authorizeRequest(t, toolset.McpSlug.String, claudeCodeCIMDClientID, cimdLoopbackRedirect)))

	require.Equal(t, http.StatusFound, w.Code)
	require.Contains(t, w.Header().Get("Location"), "idp.example.com/authorize")
}

// TestHandleAuthorize_BlockedToolDoesNotReachDynamicallyRegisteredClients is
// the CIMD-only decision, pinned. A dynamically registered client presents an
// opaque per-registration id that resolves to no catalog entry, so nothing
// names it and the block cannot apply — even when the organization blocks a
// tool that is, in reality, the same product.
func TestHandleAuthorize_BlockedToolDoesNotReachDynamicallyRegisteredClients(t *testing.T) {
	t.Parallel()

	idpURL, err := url.Parse("https://idp.example.com/authorize?state=challenge123")
	require.NoError(t, err)
	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{buildAuthURLResult: idpURL})
	toolset, _, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	blockTarget(t, ctx, ti, authCtx.ActiveOrganizationID, "claude-code")

	w := httptest.NewRecorder()
	require.NoError(t, ti.service.HandleAuthorize(w, authorizeRequest(t, toolset.McpSlug.String, client.ClientID, client.RedirectUris[0])))

	require.Equal(t, http.StatusFound, w.Code)
}

// errAIToolReadUnavailable stands in for a database the block check cannot
// reach.
var errAIToolReadUnavailable = errors.New("ai scan targets unreachable")

// TestHandleAuthorize_BlockedIDsReadFails_AllowsTheCaller pins fail-open.
// The first read asks only whether the organization blocks anything; when it
// fails there is no evidence a block exists, and refusing every MCP client in
// an organization that may never have used the feature is a worse outage than
// the one it would prevent. The block recorded here is real, so the caller
// gets through only because the read failed.
func TestHandleAuthorize_BlockedIDsReadFails_AllowsTheCaller(t *testing.T) {
	t.Parallel()

	idpURL, err := url.Parse("https://idp.example.com/authorize?state=challenge123")
	require.NoError(t, err)
	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{buildAuthURLResult: idpURL})
	toolset, issuer, _ := seedPrivateToolsetWithIssuer(t, ctx, ti)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	seedCIMDClient(t, ctx, ti, issuer.ID, claudeCodeCIMDClientID)
	blockTarget(t, ctx, ti, authCtx.ActiveOrganizationID, "claude-code")
	ti.service.FailAIToolBlockedIDsRead(errAIToolReadUnavailable)

	w := httptest.NewRecorder()
	require.NoError(t, ti.service.HandleAuthorize(w, authorizeRequest(t, toolset.McpSlug.String, claudeCodeCIMDClientID, cimdLoopbackRedirect)))

	require.Equal(t, http.StatusFound, w.Code, w.Body.String())
	require.Contains(t, w.Header().Get("Location"), "idp.example.com/authorize")
}

// TestHandleAuthorize_CatalogReadFailsWhileBlocked_RefusedAsRetryable pins
// fail-closed. Once the organization is known to block something, a catalog
// that cannot be read leaves the answer unknown rather than absent, so the
// request is refused as retryable instead of quietly bypassing the control.
// The block is for an unrelated tool on purpose: the refusal is about not
// knowing, not about this caller being blocked.
func TestHandleAuthorize_CatalogReadFailsWhileBlocked_RefusedAsRetryable(t *testing.T) {
	t.Parallel()

	// A working IdP, so the only thing standing between this request and a
	// redirect is the unreadable catalog.
	idpURL, err := url.Parse("https://idp.example.com/authorize?state=challenge123")
	require.NoError(t, err)
	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{buildAuthURLResult: idpURL})
	toolset, issuer, _ := seedPrivateToolsetWithIssuer(t, ctx, ti)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	seedCIMDClient(t, ctx, ti, issuer.ID, claudeCodeCIMDClientID)
	blockTarget(t, ctx, ti, authCtx.ActiveOrganizationID, "cursor")
	ti.service.FailAIToolCatalogRead(errAIToolReadUnavailable)

	w := httptest.NewRecorder()
	require.NoError(t, ti.service.HandleAuthorize(w, authorizeRequest(t, toolset.McpSlug.String, claudeCodeCIMDClientID, cimdLoopbackRedirect)))

	requireAuthorizeOAuthError(t, w, http.StatusServiceUnavailable, "temporarily_unavailable")
}

// TestHandleToken_BlockedAITool_RefusedOnItsNextUse is why the token endpoint
// re-runs the check: the grant below was minted before the block and is
// otherwise valid, and an administrator who blocks a tool expects it to stop
// working now rather than when its credentials happen to expire.
func TestHandleToken_BlockedAITool_RefusedOnItsNextUse(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	toolset, issuer, _ := seedPrivateToolsetWithIssuer(t, ctx, ti)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	client := seedCIMDClient(t, ctx, ti, issuer.ID, claudeCodeCIMDClientID)
	code, verifier := seedAuthorizationCode(t, ctx, ti, toolset, client)
	blockTarget(t, ctx, ti, authCtx.ActiveOrganizationID, "claude-code")

	w := postForm(t, ti, toolset.McpSlug.String, "token", codeGrantForm(client, code, verifier))

	require.Equal(t, http.StatusUnauthorized, w.Code, w.Body.String())
	requireTokenOAuthError(t, w, "invalid_client")
	requireAuthorizeErrorDescription(t, w, "administrator must approve")
}

// TestHandleToken_UnblockedAITool_ExchangesNormally: a block recorded for one
// tool must not touch another tool's token exchange.
func TestHandleToken_UnblockedAITool_ExchangesNormally(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	toolset, issuer, _ := seedPrivateToolsetWithIssuer(t, ctx, ti)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	client := seedCIMDClient(t, ctx, ti, issuer.ID, claudeCodeCIMDClientID)
	code, verifier := seedAuthorizationCode(t, ctx, ti, toolset, client)
	blockTarget(t, ctx, ti, authCtx.ActiveOrganizationID, "cursor")

	w := postForm(t, ti, toolset.McpSlug.String, "token", codeGrantForm(client, code, verifier))

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "access_token")
}

// TestHandleToken_CatalogReadFailsWhileBlocked_RefusedAsRetryable: the token
// endpoint renders the unknown answer the same way authorize does, as a
// retryable failure rather than a bypass.
func TestHandleToken_CatalogReadFailsWhileBlocked_RefusedAsRetryable(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	toolset, issuer, _ := seedPrivateToolsetWithIssuer(t, ctx, ti)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	client := seedCIMDClient(t, ctx, ti, issuer.ID, claudeCodeCIMDClientID)
	code, verifier := seedAuthorizationCode(t, ctx, ti, toolset, client)
	blockTarget(t, ctx, ti, authCtx.ActiveOrganizationID, "cursor")
	ti.service.FailAIToolCatalogRead(errAIToolReadUnavailable)

	w := postForm(t, ti, toolset.McpSlug.String, "token", codeGrantForm(client, code, verifier))

	require.Equal(t, http.StatusServiceUnavailable, w.Code, w.Body.String())
	requireTokenOAuthError(t, w, "temporarily_unavailable")
}
