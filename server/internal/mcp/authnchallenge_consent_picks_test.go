package mcp_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	usersessions_repo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// seedTwoClientConsentEndpoint is a remote-backed endpoint whose server
// offers two upstream clients.
func seedTwoClientConsentEndpoint(t *testing.T, tag string) (context.Context, consentActionFixture) {
	t.Helper()
	ctx, fx := standaloneConsent(t, tag, consentChainedUpstream, func(ctx context.Context, ti *testInstance, projectID, issuerID uuid.UUID, slug string) uuid.UUID {
		return attachConsentRemoteMcpServer(t, ctx, ti.conn, projectID, issuerID, slug, consentChainedUpstream)
	})
	fx.clientA = createConsentRemoteClient(t, ctx, fx.ti.conn, fx.projectID, fx.orgID, tag+"-a", "", []uuid.UUID{fx.shared})
	fx.clientB = createConsentRemoteClient(t, ctx, fx.ti.conn, fx.projectID, fx.orgID, tag+"-b", "", []uuid.UUID{fx.shared})
	return ctx, fx
}

// approveConsent approves the fixture's consent and returns the grant its
// authorization code redeems.
func approveConsent(t *testing.T, ctx context.Context, fx consentActionFixture) mcp.UserSessionGrant {
	t.Helper()
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
	grant, err := grantCache.Get(ctx, "userSessionGrant:"+fx.shared.String()+":"+code)
	require.NoError(t, err)
	return grant
}

// The card a person goes through is the server's pick, whether they connected
// it interactively or identity chaining serves it; with no such card, or
// several, the server gets no pick.
func TestConsentPost_RecordsTheUpstreamClientPicked(t *testing.T) {
	t.Parallel()

	t.Run("connected card", func(t *testing.T) {
		t.Parallel()
		ctx, fx := seedTwoClientConsentEndpoint(t, "pick-connected")
		insertQualifiedRemoteSessionToken(t, ctx, fx.ti, fx.shared, fx.clientB, fx.subject, "token-b", consentChainedUpstream)

		grant := approveConsent(t, ctx, fx)
		require.Equal(t, map[uuid.UUID]uuid.UUID{fx.endpoint.McpServerID.UUID: fx.clientB}, grant.UpstreamClientPicks)
	})

	t.Run("chained card", func(t *testing.T) {
		t.Parallel()
		ctx, fx := seedTwoClientConsentEndpoint(t, "pick-chained")
		setConsentGovernor(t, fx, clientRemoteIssuerID(t, ctx, fx.ti.conn, fx.projectID, fx.orgID, fx.clientA), consentChainedUpstream)

		grant := approveConsent(t, ctx, fx)
		require.Equal(t, map[uuid.UUID]uuid.UUID{fx.endpoint.McpServerID.UUID: fx.clientA}, grant.UpstreamClientPicks)
	})

	t.Run("no usable card", func(t *testing.T) {
		t.Parallel()
		ctx, fx := seedTwoClientConsentEndpoint(t, "pick-none")

		grant := approveConsent(t, ctx, fx)
		require.Empty(t, grant.UpstreamClientPicks)
	})

	t.Run("several usable cards", func(t *testing.T) {
		t.Parallel()
		ctx, fx := seedTwoClientConsentEndpoint(t, "pick-several")
		insertQualifiedRemoteSessionToken(t, ctx, fx.ti, fx.shared, fx.clientA, fx.subject, "token-a", consentChainedUpstream)
		insertQualifiedRemoteSessionToken(t, ctx, fx.ti, fx.shared, fx.clientB, fx.subject, "token-b", consentChainedUpstream)

		grant := approveConsent(t, ctx, fx)
		require.Empty(t, grant.UpstreamClientPicks)
	})
}

// Redeeming the authorization code records the grant's pick as the new
// credential's choice.
func TestHandleToken_RecordsTheConsentPickAsTheChoice(t *testing.T) {
	t.Parallel()
	ctx, fx := seedTwoClientConsentEndpoint(t, "pick-redeem")
	client, err := usersessions_repo.New(fx.ti.conn).GetUserSessionClientByClientID(ctx, usersessions_repo.GetUserSessionClientByClientIDParams{UserSessionIssuerID: fx.shared, ClientID: "age3328-mcp-client"})
	require.NoError(t, err)
	serverID := fx.endpoint.McpServerID.UUID
	_, err = mcpendpointsrepo.New(fx.ti.conn).CreateMCPEndpoint(ctx, mcpendpointsrepo.CreateMCPEndpointParams{
		ProjectID:       fx.projectID,
		CustomDomainID:  uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		McpServerID:     conv.ToNullUUID(serverID),
		MetaMcpServerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		Slug:            fx.endpoint.Slug,
	})
	require.NoError(t, err)

	verifier := "verifier-" + uuid.NewString()
	sum := sha256.Sum256([]byte(verifier))
	code := "code-" + uuid.NewString()
	grantCache := cache.NewTypedObjectCache[mcp.UserSessionGrant](fx.ti.logger, fx.ti.cacheAdapter, cache.SuffixNone)
	require.NoError(t, grantCache.Store(ctx, mcp.UserSessionGrant{
		Code:                code,
		UserSessionIssuerID: fx.shared,
		UserSessionClientID: client.ID,
		ClientID:            client.ClientID,
		RedirectURI:         client.RedirectUris[0],
		CodeChallenge:       base64.RawURLEncoding.EncodeToString(sum[:]),
		CodeChallengeMethod: "S256",
		Subject:             fx.subject,
		UpstreamClientPicks: map[uuid.UUID]uuid.UUID{serverID: fx.clientB},
		CreatedAt:           time.Now(),
	}))

	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", client.RedirectUris[0])
	form.Set("client_id", client.ClientID)
	form.Set("code_verifier", verifier)
	w := postForm(t, fx.ti, fx.endpoint.Slug, "token", form)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	sessions, err := testrepo.New(fx.ti.conn).ListUserSessionCredentialsForSubjectFixture(ctx, fx.subject)
	require.NoError(t, err)
	require.Len(t, sessions, 1)
	choice, err := usersessions_repo.New(fx.ti.conn).GetUpstreamClientSelection(ctx, usersessions_repo.GetUpstreamClientSelectionParams{
		UserSessionIssuerID: fx.shared,
		CredentialKind:      upstreamclients.CredentialKindUserSession,
		CredentialID:        sessions[0].CredentialID,
		McpServerID:         serverID,
		OrganizationID:      fx.orgID,
	})
	require.NoError(t, err)
	require.Equal(t, uuid.NullUUID{UUID: fx.clientB, Valid: true}, choice.RemoteSessionClientID)
}
