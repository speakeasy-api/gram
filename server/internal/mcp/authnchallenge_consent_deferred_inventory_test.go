package mcp_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestConsentPage_ChainedServiceDefersToolInventory(t *testing.T) {
	t.Parallel()

	_, fx, issuer := seedChainedConsentEndpoint(t, "aim524-deferred-page")
	setConsentGovernor(t, fx, issuer, consentChainedUpstream)

	code, page, _ := render(t, fx)
	require.Equal(t, http.StatusOK, code, page)
	require.Contains(t, page, `id="consent-tools-root"`)
	require.Contains(t, page, `data-inventory-deferred="true"`)
	require.Contains(t, page, `data-consent-enabled="true"`)

	// The transport still answers "not connected"; the page flag is what
	// turns that answer into the deferred state.
	req := httptest.NewRequest(http.MethodPost, "/mcp/"+fx.endpoint.Slug+"/connect/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Gram-Consent-State", fx.stateID)
	req.Header.Set("Gram-Consent-Csrf", "csrf-token")
	req.Header.Set("Gram-Consent-Inventory-Attempt", uuid.NewString())
	err := fx.ti.service.ServeConsentMCP(httptest.NewRecorder(), req, fx.endpoint)
	var se *oops.ShareableError
	require.ErrorAs(t, err, &se)
	require.Equal(t, oops.CodeConflict, se.Code)
}

func TestConsentPage_LinkedServiceDoesNotDeferToolInventory(t *testing.T) {
	t.Parallel()

	ctx, fx, issuer := seedChainedConsentEndpoint(t, "aim524-linked-page")
	setConsentGovernor(t, fx, issuer)
	grant(t, ctx, fx, fx.clientA, consentChainedUpstream)

	code, page, _ := render(t, fx)
	require.Equal(t, http.StatusOK, code, page)
	require.Contains(t, page, `id="consent-tools-root"`)
	require.Contains(t, page, `data-inventory-deferred="false"`)
}

func TestConsentPost_DeferredInventoryApprovesAllTools(t *testing.T) {
	t.Parallel()

	ctx, fx, issuer := seedChainedConsentEndpoint(t, "aim524-deferred-approve")
	setConsentGovernor(t, fx, issuer, consentChainedUpstream)

	// What the island submits in the deferred state: no inventory, filtering off.
	form := url.Values{}
	form.Set("state", fx.stateID)
	form.Set("csrf_token", "csrf-token")
	form.Set("action", "approve")
	form.Set("tool_filtering", "off")
	req := httptest.NewRequest(http.MethodPost, "/mcp/"+fx.endpoint.Slug+"/connect", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	require.NoError(t, fx.ti.service.ServeConsent(w, req, fx.endpoint))
	require.Equal(t, http.StatusSeeOther, w.Code, w.Body.String())
	loc, err := url.Parse(w.Header().Get("Location"))
	require.NoError(t, err)
	code := loc.Query().Get("code")
	require.NotEmpty(t, code)

	grantCache := cache.NewTypedObjectCache[mcp.UserSessionGrant](fx.ti.logger, fx.ti.cacheAdapter, cache.SuffixNone)
	stored, err := grantCache.Get(ctx, "userSessionGrant:"+fx.shared.String()+":"+code)
	require.NoError(t, err)
	require.Nil(t, stored.ToolSelection, "no selection is the unrestricted all-tools grant")
}
