package mcp_test

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	authclientfixtures "github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/authclientbackfill"
	upstreamclientsrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	productfeaturesrepo "github.com/speakeasy-api/gram/server/internal/productfeatures/repo"
	"github.com/speakeasy-api/gram/server/internal/ratelimit"
	remotesessions_repo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const consentReconnectHint = "Connect or reconnect a service above to give access."

// expireConsentGrant stores a grant for client whose authorization lifetime has ended.
func expireConsentGrant(t *testing.T, ctx context.Context, ti *testInstance, issuer, client uuid.UUID, subject urn.SessionSubject, resource string) {
	t.Helper()
	accessEncrypted, err := ti.enc.Encrypt([]byte("dead-token"))
	require.NoError(t, err)
	_, err = remotesessions_repo.New(ti.conn).UpsertRemoteSession(ctx, remotesessions_repo.UpsertRemoteSessionParams{
		SubjectUrn:             subject,
		UserSessionIssuerID:    issuer,
		RemoteSessionClientID:  client,
		AccessTokenEncrypted:   accessEncrypted,
		AuthorizationExpiresAt: conv.ToPGTimestamptz(time.Now().Add(-time.Second)),
		RefreshTokenEncrypted:  pgtype.Text{},
		RefreshExpiresAt:       pgtype.Timestamptz{},
		AccessExpiresAt:        pgtype.Timestamptz{},
		Scopes:                 []string{},
		Resource:               conv.ToPGTextEmpty(resource),
	})
	require.NoError(t, err)
}

// A refused approval returns to the page, which explains itself.
func TestConsentPost_RefusesWhenNoConnectionCanServe(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		expire bool
	}{
		{name: "expired", expire: true},
		{name: "never connected", expire: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, fx := seedTwoClientConsentEndpoint(t, "no-serving-"+uuid.NewString()[:8])
			if tc.expire {
				expireConsentGrant(t, ctx, fx.ti, fx.shared, fx.clientA, fx.subject, consentChainedUpstream)
			}

			w := postApprove(t, ctx, fx)
			require.Equal(t, http.StatusSeeOther, w.Code, w.Body.String())
			loc, err := url.Parse(w.Header().Get("Location"))
			require.NoError(t, err)
			require.Contains(t, loc.Path, fx.endpoint.Slug, "returns to the consent page, not the client")
			require.Equal(t, fx.stateID, loc.Query().Get("state"))
			require.Empty(t, loc.Query().Get("code"))

			code, page, _ := render(t, fx)
			require.Equal(t, http.StatusOK, code, page)
			require.Contains(t, page, `id="consent-tools-root"`)
			require.Contains(t, page, `data-consent-enabled="false"`)
			require.Contains(t, normalizeSpaces(page), consentReconnectHint)

			insertQualifiedRemoteSessionToken(t, ctx, fx.ti, fx.shared, fx.clientA, fx.subject, "token-a", consentChainedUpstream)
			code, page, _ = render(t, fx)
			require.Equal(t, http.StatusOK, code, page)
			require.Contains(t, page, `data-consent-enabled="true"`)
			require.NotContains(t, normalizeSpaces(page), consentReconnectHint)
			grant := approveConsent(t, ctx, fx)
			require.Equal(t, map[uuid.UUID]uuid.UUID{fx.endpoint.McpServerID.UUID: fx.clientA}, grant.UpstreamClientPicks)
		})
	}
}

func normalizeSpaces(page string) string {
	return strings.Join(strings.Fields(page), " ")
}

func TestConsentPost_ApprovesWhenAnotherConnectionStillWorks(t *testing.T) {
	t.Parallel()

	ctx, fx := seedTwoClientConsentEndpoint(t, "expired-one")
	expireConsentGrant(t, ctx, fx.ti, fx.shared, fx.clientA, fx.subject, consentChainedUpstream)
	insertQualifiedRemoteSessionToken(t, ctx, fx.ti, fx.shared, fx.clientB, fx.subject, "token-b", consentChainedUpstream)

	grant := approveConsent(t, ctx, fx)
	require.Equal(t, map[uuid.UUID]uuid.UUID{fx.endpoint.McpServerID.UUID: fx.clientB}, grant.UpstreamClientPicks)
}

// typedConsentEndpoint enforces typed credentials on a direct server.
func typedConsentEndpoint(t *testing.T, tag string) (context.Context, consentActionFixture) {
	t.Helper()
	ctx, fx := standaloneConsent(t, tag, consentChainedUpstream, func(ctx context.Context, ti *testInstance, projectID, issuerID uuid.UUID, slug string) uuid.UUID {
		return attachConsentRemoteMcpServer(t, ctx, ti.conn, projectID, issuerID, slug, consentChainedUpstream)
	})
	fx.ti.service.SetAuthClientPermitted(true)
	_, err := productfeaturesrepo.New(fx.ti.conn).EnableFeature(ctx, productfeaturesrepo.EnableFeatureParams{OrganizationID: fx.orgID, FeatureName: string(productfeatures.FeatureUpstreamAuthEnforced)})
	require.NoError(t, err)
	return ctx, fx
}

// A typed personal credential that has expired cannot be approved.
func TestConsentPost_TypedChoiceRefusesAnExpiredPersonalClient(t *testing.T) {
	t.Parallel()

	ctx, fx := typedConsentEndpoint(t, "typed-expired")
	serverID := fx.endpoint.McpServerID.UUID
	registration := createConsentRemoteClient(t, ctx, fx.ti.conn, fx.projectID, fx.orgID, "typed-expired", "", nil)
	fixtures := authclientfixtures.New(fx.ti.conn)
	authClientID, err := fixtures.SeedAuthClientFixture(ctx, authclientfixtures.SeedAuthClientFixtureParams{OrganizationID: fx.orgID, ProjectID: uuid.NullUUID{UUID: fx.projectID, Valid: true}, ClientType: "remote_session", Source: "admin", RemoteSessionClientID: uuid.NullUUID{UUID: registration, Valid: true}})
	require.NoError(t, err)
	require.NoError(t, fixtures.SeedServerAuthClientLinkFixture(ctx, authclientfixtures.SeedServerAuthClientLinkFixtureParams{ProjectID: fx.projectID, OrganizationID: fx.orgID, McpServerID: serverID, AuthClientID: authClientID, Source: "admin"}))
	expireConsentGrant(t, ctx, fx.ti, fx.shared, registration, fx.subject, consentChainedUpstream)

	_, err = postApproveResult(t, ctx, fx)
	var shareable *oops.ShareableError
	require.ErrorAs(t, err, &shareable)
	require.Equal(t, oops.CodeFailedPrecondition, shareable.Code)
}

// A shared credential needs no connection, so an expired service card does not block it.
func TestConsentPost_TypedSharedCredentialIgnoresAnExpiredCard(t *testing.T) {
	t.Parallel()

	ctx, fx := typedConsentEndpoint(t, "typed-shared")
	serverID := fx.endpoint.McpServerID.UUID
	fixtures := authclientfixtures.New(fx.ti.conn)
	authClientID, err := fixtures.SeedAuthClientFixture(ctx, authclientfixtures.SeedAuthClientFixtureParams{OrganizationID: fx.orgID, ProjectID: uuid.NullUUID{UUID: fx.projectID, Valid: true}, ClientType: "static_credential", Source: "admin", RemoteSessionClientID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}})
	require.NoError(t, err)
	require.NoError(t, fixtures.SeedAuthClientHeaderFixture(ctx, authclientfixtures.SeedAuthClientHeaderFixtureParams{AuthClientID: authClientID, OrganizationID: fx.orgID, Name: "X-Api-Key", Value: pgtype.Text{String: "key", Valid: true}}))
	require.NoError(t, fixtures.SeedServerAuthClientLinkFixture(ctx, authclientfixtures.SeedServerAuthClientLinkFixtureParams{ProjectID: fx.projectID, OrganizationID: fx.orgID, McpServerID: serverID, AuthClientID: authClientID, Source: "admin"}))
	expired := createConsentRemoteClient(t, ctx, fx.ti.conn, fx.projectID, fx.orgID, "typed-shared", "", []uuid.UUID{fx.shared})
	expireConsentGrant(t, ctx, fx.ti, fx.shared, expired, fx.subject, consentChainedUpstream)

	grant := approveConsent(t, ctx, fx)
	require.Equal(t, map[uuid.UUID]uuid.UUID{serverID: authClientID}, grant.AuthClientPicks)
}

// An attached connection is stored under its owner, so the agent's card reads as not connected.
func TestConsentAgentApproval_AttachedConnectionIsNotJudgedByCards(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	fx := newAgentConsentFixture(t, ctx, ti)
	seedUserMCPConnectGrant(t, ctx, ti.conn, fx.orgID, fx.userID, fx.target.MCPResourceID.String())
	agent := createConsentAgent(t, ctx, ti, fx, "Attached agent")
	seedPrincipalMCPConnectGrant(t, ctx, ti, fx.orgID, urn.NewPrincipal(urn.PrincipalTypeAgent, agent.ID.String()), fx.target.MCPResourceID)
	client := createConsentRemoteClient(t, ctx, ti.conn, fx.target.ProjectID, fx.orgID, "agent-attached", "", []uuid.UUID{fx.target.UserSessionIssuerID})
	owner := urn.NewUserSubject(fx.userID)
	insertQualifiedRemoteSessionToken(t, ctx, ti, fx.target.UserSessionIssuerID, client, owner, "owner-token", "")

	w, err := serveAgentConsentPost(t, ctx, ti, fx, agent.ID)
	require.Error(t, err, "without an attachment the agent's connection check refuses")
	var shareable *oops.ShareableError
	require.ErrorAs(t, err, &shareable)
	require.Equal(t, oops.CodeConflict, shareable.Code)
	require.Empty(t, w.Header().Get("Location"))

	source, err := remotesessions_repo.New(ti.conn).GetActiveRemoteSession(ctx, remotesessions_repo.GetActiveRemoteSessionParams{SubjectUrn: owner, RemoteSessionClientID: client})
	require.NoError(t, err)
	_, err = testrepo.New(ti.conn).InsertAttachmentFixture(ctx, testrepo.InsertAttachmentFixtureParams{ProjectID: fx.target.ProjectID, OrganizationID: fx.orgID, PrincipalID: agent.ID, UserSessionIssuerID: fx.target.UserSessionIssuerID, RemoteSessionClientID: client, RemoteSessionID: source.ID, GrantGeneration: source.GrantGeneration, AttachedBySubjectID: owner.String()})
	require.NoError(t, err)

	w, err = serveAgentConsentPost(t, ctx, ti, fx, agent.ID)
	require.NoError(t, err)
	require.Equal(t, http.StatusSeeOther, w.Code)
	redirect, err := url.Parse(w.Header().Get("Location"))
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(redirect.Query().Get("code"), "agent-v1."))
}

// A gateway member that needs no sign-in can be approved beside an expired member.
func TestGatewayConsentPost_IsNotJudgedByCards(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	projectID, orgID := *authCtx.ProjectID, authCtx.ActiveOrganizationID
	_, issuer, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
	slug := "gateway-expired-" + uuid.NewString()
	gateway := createMetaMcpEndpoint(t, ctx, ti.conn, projectID, orgID, slug, issuer.ID)
	docsSlug := "docs-" + uuid.NewString()[:8]
	seedMetaMemberWithUpstream(t, ctx, ti.conn, projectID, gateway.ID, "Docs", docsSlug, 1, newRecordingUpstream(t, "search_docs").url)
	trackerUpstream := newRecordingUpstream(t, "list_issues")
	trackerID := seedMetaMemberWithUpstream(t, ctx, ti.conn, projectID, gateway.ID, "Tracker", "tracker-"+uuid.NewString()[:8], 2, trackerUpstream.url)
	remoteClient := createConsentRemoteClient(t, ctx, ti.conn, projectID, orgID, "gw-expired-"+uuid.NewString()[:8], "", []uuid.UUID{issuer.ID})
	_, err := upstreamclientsrepo.New(ti.conn).AssociateMCPServerRemoteSessionClient(ctx, upstreamclientsrepo.AssociateMCPServerRemoteSessionClientParams{OrganizationID: orgID, ProjectID: projectID, McpServerID: trackerID, RemoteSessionClientID: remoteClient})
	require.NoError(t, err)
	expireConsentGrant(t, ctx, ti, issuer.ID, remoteClient, urn.NewUserSubject(authCtx.UserID), trackerUpstream.url)

	stateID, csrfToken := seedGatewayConsentChallenge(t, ctx, ti, issuer.ID, client, gateway.ID, slug)
	endpoint, err := ti.service.LoadResolvedMcpEndpointBySlug(ctx, ti.logger, slug, "mcp")
	require.NoError(t, err)
	inventory := gatewayInventoryFromPage(t, consentGetPageWithContext(t, ctx, ti, slug, stateID))

	form := url.Values{"state": {stateID}, "csrf_token": {csrfToken}, "action": {"approve"}, "tool_inventory_id": {inventory.Attempt}, "tool_filtering": {"on"}, "tools": {docsSlug + "--search_docs"}}
	req := httptest.NewRequest(http.MethodPost, "/mcp/"+slug+"/connect", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	require.NoError(t, ti.service.ServeConsent(w, req.WithContext(ctx), endpoint))
	require.Equal(t, http.StatusSeeOther, w.Code, w.Body.String())
	location, err := url.Parse(w.Header().Get("Location"))
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(location.Query().Get("code"), "gateway-v2."))
}

// The verify refusal logs why nothing was probed instead of blaming routing.
func TestServeConsentAction_ValidateExpiredGrantNamesTheCause(t *testing.T) {
	t.Parallel()

	logs := &syncBuffer{mu: sync.Mutex{}, buf: bytes.Buffer{}}
	reader, provider := newValidationMeterProvider()
	ctx, ti := newTestMCPServiceWithPoolConfig(t, slog.New(slog.NewJSONHandler(logs, &slog.HandlerOptions{AddSource: false, Level: slog.LevelDebug, ReplaceAttr: nil})), provider, &mockIdentityResolver{hasAccessOK: true}, mcp.TunnelPublicConfig{
		SessionTTL:         0,
		LiveSessionCap:     0,
		InitializeRate:     ratelimit.Rate{Tokens: 0, Interval: 0, Burst: 0},
		RequestRate:        ratelimit.Rate{Tokens: 0, Interval: 0, Burst: 0},
		MaxRequestLifetime: 0,
	}, nil, nil, mcp.MetaRuntimeConfig{MemberCallTimeout: 0, ValidationTimeout: validationProbeTimeout, AutoVerifyWait: 0, RecheckInterval: recheckTestInterval})
	fx := seedMetaValidationFixtureOn(t, ctx, ti, reader, "aim857-cause")
	projectID, _ := consentTestTenant(t, ctx)
	sess := storedSession(t, ctx, fx)
	require.NoError(t, remotesessions_repo.New(fx.ti.conn).SetRemoteSessionAccessExpiresAt(ctx, remotesessions_repo.SetRemoteSessionAccessExpiresAtParams{
		AccessExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(-time.Hour), Valid: true, InfinityModifier: pgtype.Finite},
		ID:              sess.ID,
		ProjectID:       conv.ToNullUUID(projectID),
	}))

	_, err := postValidate(t, fx, fx.clientID)
	var shareable *oops.ShareableError
	require.ErrorAs(t, err, &shareable)
	require.ErrorContains(t, shareable.Unwrap(), "no usable credential")

	output := logs.String()
	require.Contains(t, output, "access token has expired and the grant has no refresh token")
	require.NotContains(t, output, "routes to no upstream")
}
