package mcp_test

import (
	"context"
	"encoding/json"
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
// reaches out for the document.
func seedCIMDClient(t *testing.T, ctx context.Context, ti *testInstance, issuerID uuid.UUID, clientID string) {
	t.Helper()

	_, err := usersessions_repo.New(ti.conn).UpsertUserSessionClientFromCIMD(ctx, usersessions_repo.UpsertUserSessionClientFromCIMDParams{
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
}

// blockTarget records a block for one catalog target id.
func blockTarget(t *testing.T, ctx context.Context, ti *testInstance, organizationID string, targetID string) {
	t.Helper()

	_, err := agentrepo.New(ti.conn).UpsertAIToolDecision(ctx, agentrepo.UpsertAIToolDecisionParams{
		OrganizationID: organizationID,
		TargetID:       targetID,
		Decision:       "blocked",
		Rationale:      conv.ToPGTextEmpty("not approved"),
		DecidedBy:      conv.ToPGTextEmpty("urn:gram:principal:user:admin"),
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
	// The denial names the tool and points at the way out. It is the end
	// user's only clue: their client picked this credential at discovery and
	// will not fall back to registration when authorize refuses it.
	require.Contains(t, body["error_description"], "Claude Code")
	require.Contains(t, body["error_description"], "request-access")
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
