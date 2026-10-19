package mcp_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestServeConsentMCP_ConfigurationChangeRequiresReload(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	toolset, issuer, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
	slug := "consent-configuration-" + uuid.NewString()
	server := createToolsetMcpEndpoint(t, ctx, ti.conn, toolset.ProjectID, toolset.ID, slug, "public", uuid.NullUUID{}, issuer.ID)
	state, csrf := seedModernConsentChallenge(t, ctx, ti, issuer.ID, client, server.ID, slug)
	endpoint, err := ti.service.LoadResolvedMcpEndpointBySlug(ctx, ti.logger, slug, "x/mcp")
	require.NoError(t, err)
	attempt := hydrateConsentInventory(t, ctx, ti, endpoint, state, csrf)
	// A subsequent request resolves a changed upstream resource, but still has
	// the page's original inventory attempt. It must not combine both configs.
	endpoint.UpstreamResource = "https://changed.example.com/mcp"
	req := httptest.NewRequest(http.MethodPost, "/x/mcp/"+slug+"/connect/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)).WithContext(ctx)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Gram-Consent-State", state)
	req.Header.Set("Gram-Consent-Csrf", csrf)
	req.Header.Set("Gram-Consent-Inventory-Attempt", attempt)
	err = ti.service.ServeConsentMCP(httptest.NewRecorder(), req, endpoint)
	var shared *oops.ShareableError
	require.ErrorAs(t, err, &shared)
	require.Equal(t, oops.CodeFailedPrecondition, shared.Code)
	require.Contains(t, shared.Error(), "reload this page")
	require.NotEqual(t, attempt, hydrateConsentInventory(t, ctx, ti, endpoint, state, csrf))
}
