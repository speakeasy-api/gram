package mcp_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/mcp/toolfilter"
	"github.com/speakeasy-api/gram/server/internal/oops"
	usersessions_repo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

const (
	inventoryDeferredAttr    = `data-inventory-deferred="true"`
	inventoryNotDeferredAttr = `data-inventory-deferred="false"`
)

func renderToolsIsland(t *testing.T, fx consentActionFixture) string {
	t.Helper()
	code, page, _ := render(t, fx)
	require.Equal(t, http.StatusOK, code, page)
	require.Contains(t, page, `id="consent-tools-root"`)
	return page
}

func TestConsentPage_ChainedServiceDefersToolInventory(t *testing.T) {
	t.Parallel()

	_, fx, issuer := seedChainedConsentEndpoint(t, "aim524-deferred-page")
	setConsentGovernor(t, fx, issuer, consentChainedUpstream)

	page := renderToolsIsland(t, fx)
	require.Contains(t, page, inventoryDeferredAttr)
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

	require.Contains(t, renderToolsIsland(t, fx), inventoryNotDeferredAttr)
}

func TestConsentPage_ConnectedCardBesideChainedCardDoesNotDefer(t *testing.T) {
	t.Parallel()

	ctx, fx, issuer := seedChainedConsentEndpoint(t, "aim524-mixed-page")
	setConsentGovernor(t, fx, issuer, consentChainedUpstream)
	fx.clientB = createConsentRemoteClient(t, ctx, fx.ti.conn, fx.projectID, fx.orgID, "aim524-mixed-page-b", "", []uuid.UUID{fx.shared})
	grant(t, ctx, fx, fx.clientB, consentChainedUpstream)

	page := renderToolsIsland(t, fx)
	require.Contains(t, page, consentManagedCopy, "card A stays chained")
	require.Contains(t, page, inventoryNotDeferredAttr, "a connected card's 409 must not read as deferred")
}

func TestConsentPage_StoredSelectionDoesNotDeferToolInventory(t *testing.T) {
	t.Parallel()

	ctx, fx, issuer := seedChainedConsentEndpoint(t, "aim524-prefill-page")
	setConsentGovernor(t, fx, issuer, consentChainedUpstream)
	seedSubjectToolSelection(t, ctx, fx, []string{"get_thing"})

	page := renderToolsIsland(t, fx)
	require.Contains(t, page, `data-prefill=`)
	require.Contains(t, page, inventoryNotDeferredAttr, "deferring would replace the stored selection with all tools")
}

// TestConsentPost_DeferredInventoryApprovesAllTools pins existing behavior
// the deferred state relies on: filtering off with no inventory approves the
// unrestricted grant.
func TestConsentPost_DeferredInventoryApprovesAllTools(t *testing.T) {
	t.Parallel()

	ctx, fx, issuer := seedChainedConsentEndpoint(t, "aim524-deferred-approve")
	setConsentGovernor(t, fx, issuer, consentChainedUpstream)

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

// seedSubjectToolSelection gives the fixture subject a live session on the
// page's client holding a restrictive selection for this endpoint.
func seedSubjectToolSelection(t *testing.T, ctx context.Context, fx consentActionFixture, tools []string) {
	t.Helper()
	q := usersessions_repo.New(fx.ti.conn)
	client, err := q.GetUserSessionClientByClientID(ctx, usersessions_repo.GetUserSessionClientByClientIDParams{
		UserSessionIssuerID: fx.shared,
		ClientID:            "age3328-mcp-client",
	})
	require.NoError(t, err)

	entries := make([]toolfilter.AllowEntry, 0, len(tools))
	for _, name := range tools {
		entries = append(entries, toolfilter.AllowEntry{Type: toolfilter.AllowTypeTool, Name: name, Mode: nil, Tools: nil})
	}
	sel, err := toolfilter.NewSessionSelection("mcp_server:"+fx.endpoint.McpServerID.UUID.String(), uuid.New(), entries)
	require.NoError(t, err)
	raw, err := json.Marshal(sel)
	require.NoError(t, err)

	sum := sha256.Sum256([]byte("refresh-" + uuid.NewString()))
	_, err = q.CreateUserSession(ctx, usersessions_repo.CreateUserSessionParams{
		UserSessionIssuerID: fx.shared,
		UserSessionClientID: conv.ToNullUUID(client.ID),
		SubjectUrn:          fx.subject,
		Jti:                 uuid.NewString(),
		RefreshTokenHash:    conv.ToPGText(base64.RawURLEncoding.EncodeToString(sum[:])),
		ExpiresAt:           pgtype.Timestamptz{Time: time.Now().Add(time.Hour), InfinityModifier: 0, Valid: true},
		RefreshExpiresAt:    pgtype.Timestamptz{Time: time.Now().Add(24 * time.Hour), InfinityModifier: 0, Valid: true},
		ToolSelection:       raw,
		SignInID:            uuid.NullUUID{UUID: uuid.Nil, Valid: false},
	})
	require.NoError(t, err)
}
