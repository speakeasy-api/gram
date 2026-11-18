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
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	authclientsrepo "github.com/speakeasy-api/gram/server/internal/authclients/repo"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	authclientfixtures "github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/authclientbackfill"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	productfeaturesrepo "github.com/speakeasy-api/gram/server/internal/productfeatures/repo"
	remotesessionsrepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/stretchr/testify/require"
)

func seedDirectScopeConsent(t *testing.T, enforced bool) (context.Context, consentActionFixture, *validationMember) {
	t.Helper()
	member := newValidationMember(t)
	ctx, fx := standaloneConsent(t, "direct-scope", member.url, func(ctx context.Context, ti *testInstance, projectID, issuerID uuid.UUID, slug string) uuid.UUID {
		return createPublicRemoteMcpServer(t, ctx, ti.conn, projectID, issuerID, slug, member.url)
	})
	fx.endpoint.IsPublic = true
	fx.ti.service.manual = true
	fx.ti.service.SetAuthClientPermitted(true)
	if enforced {
		_, err := productfeaturesrepo.New(fx.ti.conn).EnableFeature(ctx, productfeaturesrepo.EnableFeatureParams{OrganizationID: fx.orgID, FeatureName: string(productfeatures.FeatureUpstreamAuthEnforced)})
		require.NoError(t, err)
	}
	return ctx, fx, member
}

func addDirectScopePersonal(t *testing.T, ctx context.Context, fx consentActionFixture, tag string, sessionIssuer uuid.UUID) (uuid.UUID, uuid.UUID) {
	t.Helper()
	registration := createConsentRemoteClient(t, ctx, fx.ti.conn, fx.projectID, fx.orgID, tag, "", nil)
	fixtures := authclientfixtures.New(fx.ti.conn)
	id, err := fixtures.SeedAuthClientFixture(ctx, authclientfixtures.SeedAuthClientFixtureParams{OrganizationID: fx.orgID, ProjectID: uuid.NullUUID{UUID: fx.projectID, Valid: true}, ClientType: "remote_session", Source: "admin", RemoteSessionClientID: uuid.NullUUID{UUID: registration, Valid: true}})
	require.NoError(t, err)
	require.NoError(t, fixtures.SeedServerAuthClientLinkFixture(ctx, authclientfixtures.SeedServerAuthClientLinkFixtureParams{ProjectID: fx.projectID, OrganizationID: fx.orgID, McpServerID: fx.endpoint.McpServerID.UUID, AuthClientID: id, Source: "admin"}))
	insertQualifiedRemoteSessionToken(t, ctx, fx.ti, sessionIssuer, registration, fx.subject, tag, fx.endpoint.UpstreamResource)
	return id, registration
}

func previewDirectScope(t *testing.T, ctx context.Context, fx consentActionFixture, id uuid.UUID) string {
	t.Helper()
	attempt := uuid.NewString()
	w := serveConsentMCPRequest(t, ctx, fx.ti, fx.endpoint, fx.stateID, "csrf-token", attempt, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`, map[string]string{"Gram-Consent-Auth-Client": id.String()})
	require.Contains(t, w.Body.String(), `"tools"`)
	return attempt
}

func approveDirectScope(ctx context.Context, fx consentActionFixture, id uuid.UUID, attempt string) (*httptest.ResponseRecorder, error) {
	form := url.Values{"state": {fx.stateID}, "csrf_token": {"csrf-token"}, "action": {"approve"}, "auth_client": {id.String()}, "tool_filtering": {"on"}, "tool_inventory_id": {attempt}}
	req := httptest.NewRequest(http.MethodPost, "/mcp/"+fx.endpoint.Slug+"/connect", strings.NewReader(form.Encode())).WithContext(ctx)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	return w, fx.ti.service.ServeConsent(w, req, fx.endpoint)
}

func TestConsentDirectScope_PersonalSessionIgnoresUnrelatedLegacyClient(t *testing.T) {
	t.Parallel()
	ctx, fx, member := seedDirectScopeConsent(t, true)
	// An expired unrelated registration must not be refreshed to preview the selected client.
	var refreshes atomic.Int32
	legacyAS := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			refreshes.Add(1)
		}
		w.WriteHeader(http.StatusBadRequest)
	}))
	t.Cleanup(legacyAS.Close)
	legacy := createConsentRemoteClient(t, ctx, fx.ti.conn, fx.projectID, fx.orgID, "unrelated-legacy", legacyAS.URL, []uuid.UUID{fx.shared})
	access, err := fx.ti.enc.Encrypt([]byte("expired-legacy-token"))
	require.NoError(t, err)
	refresh, err := fx.ti.enc.Encrypt([]byte("legacy-refresh-token"))
	require.NoError(t, err)
	_, err = remotesessionsrepo.New(fx.ti.conn).UpsertRemoteSession(ctx, remotesessionsrepo.UpsertRemoteSessionParams{
		SubjectUrn: fx.subject, UserSessionIssuerID: fx.shared, RemoteSessionClientID: legacy,
		AccessTokenEncrypted: access, AccessExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(-time.Hour), Valid: true},
		RefreshTokenEncrypted: conv.ToPGText(refresh), RefreshExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true},
		Scopes: []string{}, Resource: conv.ToPGText(fx.endpoint.UpstreamResource),
	})
	require.NoError(t, err)
	legacyIssuer := clientRemoteIssuerID(t, ctx, fx.ti.conn, fx.projectID, fx.orgID, legacy)
	err = authclientfixtures.New(fx.ti.conn).SetServerRemoteSessionIssuerFixture(ctx, authclientfixtures.SetServerRemoteSessionIssuerFixtureParams{RemoteSessionIssuerID: conv.ToNullUUID(legacyIssuer), ID: fx.endpoint.McpServerID.UUID})
	require.NoError(t, err)
	otherIssuer := createUserSessionIssuer(t, ctx, fx.ti.conn, fx.projectID)
	id, registration := addDirectScopePersonal(t, ctx, fx, "selected-personal", otherIssuer)
	// Bind the real consent challenge to PKCE so its resulting code can be redeemed.
	verifier := "verifier-" + uuid.NewString()
	sum := sha256.Sum256([]byte(verifier))
	state, err := fx.ti.authnChallengeCache.Get(ctx, "authnChallenge:"+fx.stateID)
	require.NoError(t, err)
	state.CodeChallenge = base64.RawURLEncoding.EncodeToString(sum[:])
	state.CodeChallengeMethod = "S256"
	require.NoError(t, fx.ti.authnChallengeCache.Store(ctx, state))
	_, err = mcpendpointsrepo.New(fx.ti.conn).CreateMCPEndpoint(ctx, mcpendpointsrepo.CreateMCPEndpointParams{
		ProjectID: fx.projectID, McpServerID: conv.ToNullUUID(fx.endpoint.McpServerID.UUID), Slug: fx.endpoint.Slug,
	})
	require.NoError(t, err)
	attempt := previewDirectScope(t, ctx, fx, id)
	requests := member.drain()
	require.NotEmpty(t, requests)
	for _, request := range requests {
		require.Equal(t, "Bearer selected-personal", request.auth)
	}
	w, err := approveDirectScope(ctx, fx, id, attempt)
	require.NoError(t, err)
	require.Equal(t, http.StatusSeeOther, w.Code, w.Body.String())
	loc, err := url.Parse(w.Header().Get("Location"))
	require.NoError(t, err)
	require.NotEmpty(t, loc.Query().Get("code"))
	grants := cache.NewTypedObjectCache[mcp.UserSessionGrant](fx.ti.logger, fx.ti.cacheAdapter, cache.SuffixNone)
	grant, err := grants.Get(ctx, "userSessionGrant:"+fx.shared.String()+":"+loc.Query().Get("code"))
	require.NoError(t, err)
	require.Equal(t, map[uuid.UUID]uuid.UUID{fx.endpoint.McpServerID.UUID: id}, grant.AuthClientPicks)
	require.Equal(t, map[uuid.UUID]uuid.UUID{fx.endpoint.McpServerID.UUID: registration}, grant.UpstreamClientPicks)
	tokenResponse := postForm(t, fx.ti, fx.endpoint.Slug, "token", url.Values{
		"grant_type": {"authorization_code"}, "code": {grant.Code},
		"client_id": {grant.ClientID}, "redirect_uri": {grant.RedirectURI}, "code_verifier": {verifier},
	})
	require.Equal(t, http.StatusOK, tokenResponse.Code, tokenResponse.Body.String())
	var token struct {
		AccessToken string `json:"access_token"`
	}
	require.NoError(t, json.Unmarshal(tokenResponse.Body.Bytes(), &token))
	require.NotEmpty(t, token.AccessToken)
	member.drain()
	execution, err := servePublicHTTP(t, ctx, fx.ti, fx.endpoint.Slug, []byte(`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`), token.AccessToken, nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, execution.Code, execution.Body.String())
	require.Contains(t, execution.Body.String(), `"tools"`)
	requests = member.drain()
	require.NotEmpty(t, requests, "redeemed token must reach the upstream")
	require.Zero(t, refreshes.Load(), "unrelated expired credentials must never be refreshed")
	for _, request := range requests {
		require.Equal(t, "Bearer selected-personal", request.auth)
	}

}

func TestConsentDirectScope_InventoryCannotApproveAnotherChoice(t *testing.T) {
	t.Parallel()
	ctx, fx, member := seedDirectScopeConsent(t, true)
	first, _ := addDirectScopePersonal(t, ctx, fx, "choice-first", fx.shared)
	second, _ := addDirectScopePersonal(t, ctx, fx, "choice-second", fx.shared)
	attempt := previewDirectScope(t, ctx, fx, first)
	for _, request := range member.drain() {
		require.Equal(t, "Bearer choice-first", request.auth)
	}
	_, err := approveDirectScope(ctx, fx, second, attempt)
	var conflict *oops.ShareableError
	require.ErrorAs(t, err, &conflict)
	require.Equal(t, oops.CodeFailedPrecondition, conflict.Code, err.Error())
	_, err = fx.ti.authnChallengeCache.Get(ctx, "authnChallenge:"+fx.stateID)
	require.NoError(t, err, "a mismatched inventory must not consume the challenge")
	attempt = previewDirectScope(t, ctx, fx, second)
	requests := member.drain()
	require.NotEmpty(t, requests)
	for _, request := range requests {
		require.Equal(t, "Bearer choice-second", request.auth)
	}
	w, err := approveDirectScope(ctx, fx, second, attempt)
	require.NoError(t, err)
	require.Equal(t, http.StatusSeeOther, w.Code, w.Body.String())
}

func TestConsentDirectScope_OffAndShadowKeepLegacyPreview(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"off", "shadow"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			ctx, fx, member := seedDirectScopeConsent(t, false)
			fx.ti.features.SetFlag(feature.FlagAuthClientResolver, fx.orgID, false)
			fx.ti.features.SetFlag(feature.FlagAuthClientResolverShadow, fx.orgID, mode == "shadow")
			legacy := createConsentRemoteClient(t, ctx, fx.ti.conn, fx.projectID, fx.orgID, "legacy-connected", "", []uuid.UUID{fx.shared})
			issuer := clientRemoteIssuerID(t, ctx, fx.ti.conn, fx.projectID, fx.orgID, legacy)
			err := authclientfixtures.New(fx.ti.conn).SetServerRemoteSessionIssuerFixture(ctx, authclientfixtures.SetServerRemoteSessionIssuerFixtureParams{RemoteSessionIssuerID: conv.ToNullUUID(issuer), ID: fx.endpoint.McpServerID.UUID})
			require.NoError(t, err)
			insertQualifiedRemoteSessionToken(t, ctx, fx.ti, fx.shared, legacy, fx.subject, "legacy-token", fx.endpoint.UpstreamResource)
			typed, _ := addDirectScopePersonal(t, ctx, fx, "typed-token", fx.shared)
			previewDirectScope(t, ctx, fx, typed)
			requests := member.drain()
			require.NotEmpty(t, requests)
			for _, request := range requests {
				require.Equal(t, "Bearer legacy-token", request.auth)
			}
		})
	}
}

func TestConsentDirectScope_RejectsUnavailableChoicesBeforeUpstream(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"unknown", "disabled", "detached", "cross-org"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			ctx, fx, member := seedDirectScopeConsent(t, true)
			// Keep a usable offered choice to detect accidental fallback after an invalid selection.
			addDirectScopePersonal(t, ctx, fx, "valid-fallback", fx.shared)
			id := uuid.New()
			fixtures := authclientfixtures.New(fx.ti.conn)
			switch kind {
			case "disabled", "detached":
				id, _ = addDirectScopePersonal(t, ctx, fx, "unavailable", fx.shared)
				if kind == "disabled" {
					require.NoError(t, fixtures.SetAuthClientEnabledFixture(ctx, authclientfixtures.SetAuthClientEnabledFixtureParams{ID: id, Enabled: false}))
				} else {
					_, err := authclientsrepo.New(fx.ti.conn).DetachAuthClientFromServer(ctx, authclientsrepo.DetachAuthClientFromServerParams{AuthClientID: id, McpServerID: fx.endpoint.McpServerID.UUID, OrganizationID: fx.orgID})
					require.NoError(t, err)
				}
			case "cross-org":
				foreignOrg := "org_" + uuid.NewString()
				require.NoError(t, fixtures.SeedOrganizationFixture(ctx, foreignOrg))
				var err error
				id, err = fixtures.SeedAuthClientFixture(ctx, authclientfixtures.SeedAuthClientFixtureParams{OrganizationID: foreignOrg, ClientType: "static_credential", Source: "admin"})
				require.NoError(t, err)
			}
			req := httptest.NewRequest(http.MethodPost, "/mcp/"+fx.endpoint.Slug+"/connect/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)).WithContext(ctx)
			req.Header.Set("Accept", "application/json, text/event-stream")
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Gram-Consent-State", fx.stateID)
			req.Header.Set("Gram-Consent-Csrf", "csrf-token")
			req.Header.Set("Gram-Consent-Inventory-Attempt", uuid.NewString())
			req.Header.Set("Gram-Consent-Auth-Client", id.String())
			err := fx.ti.service.ServeConsentMCP(httptest.NewRecorder(), req, fx.endpoint)
			var rejected *oops.ShareableError
			require.ErrorAs(t, err, &rejected)
			require.Equal(t, oops.CodeFailedPrecondition, rejected.Code)
			require.Empty(t, member.drain(), "invalid selections must not reach upstream or fall back")
		})
	}
}
