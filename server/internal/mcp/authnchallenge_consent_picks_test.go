package mcp_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients"
	authclientfixtures "github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/authclientbackfill"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	productfeaturesrepo "github.com/speakeasy-api/gram/server/internal/productfeatures/repo"
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
	fx.clientA = createConsentRemoteClient(t, ctx, fx.ti.conn, fx.projectID, fx.orgID, tag+"-a", "")
	fx.clientB = createConsentRemoteClient(t, ctx, fx.ti.conn, fx.projectID, fx.orgID, tag+"-b", "")
	return ctx, fx
}

// postApproveResult posts the fixture's approval and returns what the handler did.
func postApproveResult(t *testing.T, ctx context.Context, fx consentActionFixture) (*httptest.ResponseRecorder, error) {
	t.Helper()
	form := url.Values{"state": {fx.stateID}, "csrf_token": {"csrf-token"}, "action": {"approve"}, "tool_filtering": {"off"}}
	req := httptest.NewRequest(http.MethodPost, "/mcp/"+fx.endpoint.Slug+"/connect", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	if err := fx.ti.service.ServeConsent(w, req.WithContext(ctx), fx.endpoint); err != nil {
		return w, fmt.Errorf("serve consent approval: %w", err)
	}
	return w, nil
}

func postApprove(t *testing.T, ctx context.Context, fx consentActionFixture) *httptest.ResponseRecorder {
	t.Helper()
	w, err := postApproveResult(t, ctx, fx)
	require.NoError(t, err)
	return w
}

// approveConsent approves the fixture's consent and returns the grant its
// authorization code redeems.
func approveConsent(t *testing.T, ctx context.Context, fx consentActionFixture) mcp.UserSessionGrant {
	t.Helper()
	w := postApprove(t, ctx, fx)
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

// A registration linked to the sign-in is not a typed offer and cannot pick
// upstream authentication for a newly approved credential.
func TestConsentPost_DoesNotInferChoiceFromSignInLink(t *testing.T) {
	t.Parallel()
	ctx, fx := seedTwoClientConsentEndpoint(t, "pick-linked")
	insertQualifiedRemoteSessionToken(t, ctx, fx.ti, fx.shared, fx.clientB, fx.subject, "token-b", consentChainedUpstream)

	w, err := postApproveResult(t, ctx, fx)
	// Legacy setup may need migration. It must never pick the linked client.
	if err != nil {
		require.ErrorContains(t, err, "The credential you chose can no longer be used")
		require.Empty(t, w.Header().Get("Location"))
		return
	}
	location, err := url.Parse(w.Header().Get("Location"))
	require.NoError(t, err)
	code := location.Query().Get("code")
	if code == "" {
		require.Equal(t, "/mcp/"+fx.endpoint.Slug+"/connect", location.Path)
		require.Equal(t, fx.stateID, location.Query().Get("state"))
		return
	}
	grant := cache.NewTypedObjectCache[mcp.UserSessionGrant](fx.ti.logger, fx.ti.cacheAdapter, cache.SuffixNone)
	stored, err := grant.Get(ctx, "userSessionGrant:"+fx.shared.String()+":"+code)
	require.NoError(t, err)
	require.Empty(t, stored.AuthClientPicks)
	require.Empty(t, stored.UpstreamClientPicks)
}

// Redeeming the authorization code records the grant's pick as the new
// credential's choice.
func TestHandleToken_DoesNotInferTypedChoiceFromLegacyConsentPick(t *testing.T) {
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
	_, err = usersessions_repo.New(fx.ti.conn).GetUpstreamClientSelection(ctx, usersessions_repo.GetUpstreamClientSelectionParams{
		UserSessionIssuerID: fx.shared,
		CredentialKind:      upstreamclients.CredentialKindUserSession,
		CredentialID:        sessions[0].CredentialID,
		McpServerID:         serverID,
		OrganizationID:      fx.orgID,
	})
	require.ErrorIs(t, err, pgx.ErrNoRows)
}

// Redemption persists the approved typed client even without an issuer link.
func TestHandleToken_RecordsTypedChoiceAndPreservesItOnRotation(t *testing.T) {
	t.Parallel()
	ctx, fx := seedTwoClientConsentEndpoint(t, "typed-redeem")
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
	registration := createConsentRemoteClient(t, ctx, fx.ti.conn, fx.projectID, fx.orgID, "typed-redeem-unlinked", "")
	fixtures := authclientfixtures.New(fx.ti.conn)
	authClientID, err := fixtures.SeedAuthClientFixture(ctx, authclientfixtures.SeedAuthClientFixtureParams{OrganizationID: fx.orgID, ProjectID: uuid.NullUUID{UUID: fx.projectID, Valid: true}, ClientType: "remote_session", Source: "admin", RemoteSessionClientID: conv.ToNullUUID(registration)})
	require.NoError(t, err)
	require.NoError(t, fixtures.SeedServerAuthClientLinkFixture(ctx, authclientfixtures.SeedServerAuthClientLinkFixtureParams{ProjectID: fx.projectID, OrganizationID: fx.orgID, McpServerID: serverID, AuthClientID: authClientID, Source: "admin"}))

	fx.ti.service.SetAuthClientPermitted(true)
	_, err = productfeaturesrepo.New(fx.ti.conn).EnableFeature(ctx, productfeaturesrepo.EnableFeatureParams{OrganizationID: fx.orgID, FeatureName: string(productfeatures.FeatureUpstreamAuthEnforced)})
	require.NoError(t, err)
	insertQualifiedRemoteSessionToken(t, ctx, fx.ti, fx.shared, registration, fx.subject, "token-typed", consentChainedUpstream)

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
		UpstreamClientPicks: map[uuid.UUID]uuid.UUID{serverID: registration},
		AuthClientPicks:     map[uuid.UUID]uuid.UUID{serverID: authClientID},
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
	choice, err := fixtures.GetSelectionFixture(ctx, authclientfixtures.GetSelectionFixtureParams{CredentialID: sessions[0].CredentialID, McpServerID: serverID})
	require.NoError(t, err)
	require.Equal(t, uuid.NullUUID{UUID: registration, Valid: true}, choice.RemoteSessionClientID)
	require.Equal(t, uuid.NullUUID{UUID: authClientID, Valid: true}, choice.AuthClientID)
	var token struct {
		RefreshToken string `json:"refresh_token"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &token))
	require.NotEmpty(t, token.RefreshToken)
	rotated := performRefreshRequest(ctx, fx.ti, fx.endpoint.Slug, client.ClientID, token.RefreshToken)
	require.NoError(t, rotated.err)
	require.Equal(t, http.StatusOK, rotated.code, rotated.body)
	rotatedSessions, err := testrepo.New(fx.ti.conn).ListUserSessionCredentialsForSubjectFixture(ctx, fx.subject)
	require.NoError(t, err)
	require.NotEmpty(t, rotatedSessions)
	for _, session := range rotatedSessions {
		require.Equal(t, sessions[0].CredentialID, session.CredentialID)
	}
	after, err := fixtures.GetSelectionFixture(ctx, authclientfixtures.GetSelectionFixtureParams{CredentialID: sessions[0].CredentialID, McpServerID: serverID})
	require.NoError(t, err)
	require.Equal(t, choice, after)
}

// A personal client offered on the server is available without legacy links.
func TestConsentPost_ChoosesAnAdministratorOfferedPersonalClient(t *testing.T) {
	t.Parallel()
	ctx, fx := standaloneConsent(t, "typed-offered", consentChainedUpstream, func(ctx context.Context, ti *testInstance, projectID, issuerID uuid.UUID, slug string) uuid.UUID {
		return attachConsentRemoteMcpServer(t, ctx, ti.conn, projectID, issuerID, slug, consentChainedUpstream)
	})
	fx.ti.service.manual = true
	fx.ti.service.SetAuthClientPermitted(true)
	serverID := fx.endpoint.McpServerID.UUID
	registration := createConsentRemoteClient(t, ctx, fx.ti.conn, fx.projectID, fx.orgID, "typed-offered", "")
	fixtures := authclientfixtures.New(fx.ti.conn)
	authClientID, err := fixtures.SeedAuthClientFixture(ctx, authclientfixtures.SeedAuthClientFixtureParams{OrganizationID: fx.orgID, ProjectID: uuid.NullUUID{UUID: fx.projectID, Valid: true}, ClientType: "remote_session", Source: "admin", RemoteSessionClientID: uuid.NullUUID{UUID: registration, Valid: true}})
	require.NoError(t, err)
	require.NoError(t, fixtures.SeedServerAuthClientLinkFixture(ctx, authclientfixtures.SeedServerAuthClientLinkFixtureParams{ProjectID: fx.projectID, OrganizationID: fx.orgID, McpServerID: serverID, AuthClientID: authClientID, Source: "admin"}))
	insertQualifiedRemoteSessionToken(t, ctx, fx.ti, fx.shared, registration, fx.subject, "token-typed", consentChainedUpstream)

	grant := approveConsent(t, ctx, fx)
	require.Equal(t, map[uuid.UUID]uuid.UUID{serverID: authClientID}, grant.AuthClientPicks)
	require.Equal(t, map[uuid.UUID]uuid.UUID{serverID: registration}, grant.UpstreamClientPicks)
}

// The consent page judges a credential's permission as the consenting
// person, as their requests will be, both when it offers the choice and when
// it approves it.
func TestConsentPost_JudgesCredentialPermissionAsTheConsentingPerson(t *testing.T) {
	t.Parallel()
	ctx, fx := standaloneConsent(t, "judge-person", consentChainedUpstream, func(ctx context.Context, ti *testInstance, projectID, issuerID uuid.UUID, slug string) uuid.UUID {
		return attachConsentRemoteMcpServer(t, ctx, ti.conn, projectID, issuerID, slug, consentChainedUpstream)
	})
	fx.ti.service.manual = true
	callers := fx.ti.service.RecordAuthClientPermitCallers()
	serverID := fx.endpoint.McpServerID.UUID
	fixtures := authclientfixtures.New(fx.ti.conn)
	authClientID, err := fixtures.SeedAuthClientFixture(ctx, authclientfixtures.SeedAuthClientFixtureParams{OrganizationID: fx.orgID, ProjectID: uuid.NullUUID{UUID: fx.projectID, Valid: true}, ClientType: "static_credential", Source: "admin", RemoteSessionClientID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}})
	require.NoError(t, err)
	require.NoError(t, fixtures.SeedAuthClientHeaderFixture(ctx, authclientfixtures.SeedAuthClientHeaderFixtureParams{AuthClientID: authClientID, OrganizationID: fx.orgID, Name: "X-Api-Key", Value: pgtype.Text{String: "key", Valid: true}}))
	require.NoError(t, fixtures.SeedServerAuthClientLinkFixture(ctx, authclientfixtures.SeedServerAuthClientLinkFixtureParams{ProjectID: fx.projectID, OrganizationID: fx.orgID, McpServerID: serverID, AuthClientID: authClientID, Source: "admin"}))
	_, err = productfeaturesrepo.New(fx.ti.conn).EnableFeature(ctx, productfeaturesrepo.EnableFeatureParams{OrganizationID: fx.orgID, FeatureName: string(productfeatures.FeatureUpstreamAuthEnforced)})
	require.NoError(t, err)

	grant := approveConsent(t, ctx, fx)
	require.Equal(t, map[uuid.UUID]uuid.UUID{serverID: authClientID}, grant.AuthClientPicks)
	require.NotEmpty(t, callers())
	for _, user := range callers() {
		require.Equal(t, fx.subject.ID, user)
	}
}
