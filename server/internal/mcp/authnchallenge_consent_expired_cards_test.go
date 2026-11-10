package mcp_test

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
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
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
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

	setAccessExpiry := func(at pgtype.Timestamptz) {
		require.NoError(t, remotesessions_repo.New(ti.conn).SetRemoteSessionAccessExpiresAt(ctx, remotesessions_repo.SetRemoteSessionAccessExpiresAtParams{AccessExpiresAt: at, ID: source.ID, ProjectID: conv.ToNullUUID(fx.target.ProjectID)}))
	}
	setAccessExpiry(conv.ToPGTimestamptz(time.Now().Add(-time.Hour)))
	_, err = serveAgentConsentPost(t, ctx, ti, fx, agent.ID)
	require.ErrorAs(t, err, &shareable, "an expired attached connection is refused")
	require.Equal(t, oops.CodeConflict, shareable.Code)
	setAccessExpiry(pgtype.Timestamptz{})

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

	requireVerifyNotice(t, fx, fx.clientID, verifyExpiredCard)

	output := logs.String()
	require.Contains(t, output, "access token has expired and the grant has no refresh token")
	require.NotContains(t, output, "routes to no upstream")
}

const (
	verifyNotConnectedCard = ">Not connected</span"
	verifyExpiredCard      = ">Expired — reconnect to continue</span"
	verifyNoSignInNotice   = "data-validation-notice >We could not get a working sign-in for this service. Try again. If it keeps failing, disconnect it and connect again, or ask your administrator.</span"
	verifyUnservedMessage  = "This connection can't be checked here."
	verifyReconnectNotice  = "data-validation-notice >Disconnect this service, then connect it again."
)

// requireVerifyNotice posts Verify and checks it returns to the card showing want and probed nothing.
func requireVerifyNotice(t *testing.T, fx validationFixture, clientID uuid.UUID, want string) {
	t.Helper()
	w, err := postValidate(t, fx, clientID)
	require.NoError(t, err)
	require.Equal(t, http.StatusSeeOther, w.Code)
	location := w.Header().Get("Location")
	base := consentPagePath(fx) + "&consent=required&validate_failed=" + clientID.String()
	require.Contains(t, []string{
		base,
		base + "&validate_notice=" + "reconnect",
		base + "&validate_notice=" + "ours",
	}, location)
	require.Contains(t, renderConsentAt(t, fx, location), want)
	if fx.member != nil {
		require.Empty(t, fx.member.drain(), "no probe reached the member")
	}
}

// storeVerifyGrant replaces the fixture's grant with one whose access token has expired.
func storeVerifyGrant(t *testing.T, ctx context.Context, fx validationFixture, refreshEncrypted pgtype.Text, refreshExpires, authorizationExpires pgtype.Timestamptz) {
	t.Helper()
	accessEncrypted, err := fx.ti.enc.Encrypt([]byte("stale-token"))
	require.NoError(t, err)
	_, err = remotesessions_repo.New(fx.ti.conn).UpsertRemoteSession(ctx, remotesessions_repo.UpsertRemoteSessionParams{
		SubjectUrn:             fx.subject,
		UserSessionIssuerID:    fx.endpoint.UserSessionIssuerID,
		RemoteSessionClientID:  fx.clientID,
		AccessTokenEncrypted:   accessEncrypted,
		AccessExpiresAt:        conv.ToPGTimestamptz(time.Now().Add(-time.Minute)),
		RefreshTokenEncrypted:  refreshEncrypted,
		RefreshExpiresAt:       refreshExpires,
		AuthorizationExpiresAt: authorizationExpires,
		Scopes:                 []string{},
		Resource:               conv.ToPGText(fx.member.url),
	})
	require.NoError(t, err)
}

func pointVerifyAtTokenEndpoint(t *testing.T, ctx context.Context, fx validationFixture, tokenEndpoint string) {
	t.Helper()
	projectID, orgID := consentTestTenant(t, ctx)
	rows, err := testrepo.New(fx.ti.conn).ForceRemoteSessionIssuerTokenEndpointFixture(ctx, testrepo.ForceRemoteSessionIssuerTokenEndpointFixtureParams{
		TokenEndpoint:         conv.ToPGText(tokenEndpoint),
		RemoteSessionClientID: fx.clientID,
		ProjectID:             projectID,
		OrganizationID:        orgID,
	})
	require.NoError(t, err)
	require.EqualValues(t, 1, rows)
}

// Refresh outages keep the grant and retry guidance; client refusals retain corrective actions.
func TestServeConsentAction_VerifyRefreshFailureOnConnectedCard(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		status int
		body   string
	}{
		{name: "provider down", status: http.StatusServiceUnavailable, body: `{}`},
		{name: "provider rate limits", status: http.StatusTooManyRequests, body: `{}`},
		{name: "provider refuses the client", status: http.StatusBadRequest, body: `{"error":"unauthorized_client"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx, fx := seedMetaValidationFixture(t, "verify-fail-"+uuid.NewString()[:8])
			var healthy atomic.Bool
			var tokenRequests atomic.Int64
			tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				tokenRequests.Add(1)
				w.Header().Set("Content-Type", "application/json")
				if healthy.Load() {
					_, _ = io.WriteString(w, `{"access_token":"recovered-token","token_type":"Bearer","expires_in":3600}`)
					return
				}
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			}))
			t.Cleanup(tokenServer.Close)
			pointVerifyAtTokenEndpoint(t, ctx, fx, tokenServer.URL)
			refresh, err := fx.ti.enc.Encrypt([]byte("refresh-token"))
			require.NoError(t, err)
			storeVerifyGrant(t, ctx, fx, conv.ToPGText(refresh), pgtype.Timestamptz{}, pgtype.Timestamptz{})

			if tc.status == http.StatusServiceUnavailable || tc.status == http.StatusTooManyRequests {
				w, err := postValidate(t, fx, fx.clientID)
				require.NoError(t, err)
				require.Equal(t, http.StatusSeeOther, w.Code)
				location := w.Header().Get("Location")
				require.Equal(t, consentPagePath(fx)+"&validate_unfinished="+fx.clientID.String(), location)
				page := renderConsentAt(t, fx, location)
				require.Contains(t, page, "Unavailable · try again shortly")
				require.NotContains(t, page, "data-validate-link")
				require.NotContains(t, page, verifyNoSignInNotice)
				require.Equal(t, refresh, storedSession(t, ctx, fx).RefreshTokenEncrypted.String)
				require.Empty(t, fx.member.drain(), "no probe reached the member")
			} else {
				requireVerifyNotice(t, fx, fx.clientID, verifyNoSignInNotice)
				page := renderConsent(t, fx)
				require.Contains(t, page, `data-validate-link`, "the card still offers Verify, so trying again is possible")
				require.Contains(t, page, `value="disconnect"`)
			}

			require.EqualValues(t, 1, tokenRequests.Load())
			healthy.Store(true)
			if tc.status == http.StatusServiceUnavailable || tc.status == http.StatusTooManyRequests {
				w, err := postValidate(t, fx, fx.clientID)
				require.NoError(t, err)
				require.Equal(t, http.StatusSeeOther, w.Code)
				require.Equal(t, consentPagePath(fx)+"&validate_unfinished="+fx.clientID.String(), w.Header().Get("Location"), "retry waits for the outage cooldown")
				require.EqualValues(t, 1, tokenRequests.Load(), "cooldown prevents another provider request")
				require.Empty(t, fx.member.drain(), "cooldown prevents a member probe")
				// Model the cooldown elapsing without sleeping through it.
				require.NoError(t, fx.ti.cacheAdapter.Delete(ctx, remotesessions.RefreshCooldownKey(storedSession(t, ctx, fx))))
			}
			requireValidated(t, fx)
			require.EqualValues(t, 2, tokenRequests.Load(), "recovery refreshes exactly once")
			recovered := storedSession(t, ctx, fx)
			accessToken, err := fx.ti.enc.Decrypt(recovered.AccessTokenEncrypted)
			require.NoError(t, err)
			require.Equal(t, "recovered-token", accessToken)
			require.True(t, recovered.LastValidatedAt.Valid)
		})
	}
}

// Retrying can't fix these, so the message must go on to name the next steps.
func TestServeConsentAction_VerifyUnfixableByRetryKeepsNextSteps(t *testing.T) {
	t.Parallel()

	t.Run("token endpoint not https", func(t *testing.T) {
		t.Parallel()
		ctx, fx := seedMetaValidationFixture(t, "verify-setup-"+uuid.NewString()[:8])
		pointVerifyAtTokenEndpoint(t, ctx, fx, "http://provider.example.com/token")
		refresh, err := fx.ti.enc.Encrypt([]byte("refresh-token"))
		require.NoError(t, err)
		storeVerifyGrant(t, ctx, fx, conv.ToPGText(refresh), pgtype.Timestamptz{}, pgtype.Timestamptz{})

		requireVerifyNotice(t, fx, fx.clientID, verifyNoSignInNotice)
		requireVerifyNotice(t, fx, fx.clientID, verifyNoSignInNotice)
		require.Contains(t, renderConsent(t, fx), `value="disconnect"`)
	})

	t.Run("stored refresh token unreadable", func(t *testing.T) {
		t.Parallel()
		ctx, fx := seedMetaValidationFixture(t, "verify-unreadable-"+uuid.NewString()[:8])
		storeVerifyGrant(t, ctx, fx, conv.ToPGText("not-ciphertext"), pgtype.Timestamptz{}, pgtype.Timestamptz{})

		// Nothing stored can be read, so the card is not connected and offers Reconnect, which rewrites the row.
		requireVerifyNotice(t, fx, fx.clientID, verifyExpiredCard)
		page := normalizeSpaces(renderConsent(t, fx))
		require.Regexp(t, `data-connect-link > Reconnect`, page)
	})
}

// A lapsed grant's card offers Reconnect, which the message names.
func TestServeConsentAction_VerifyLapsedGrantAsksToReconnect(t *testing.T) {
	t.Parallel()

	past := conv.ToPGTimestamptz(time.Now().Add(-time.Minute))
	future := conv.ToPGTimestamptz(time.Now().Add(time.Hour))
	refreshToken := "refresh-token"
	for _, tc := range []struct {
		name                 string
		refresh              *string
		refreshExpires       pgtype.Timestamptz
		authorizationExpires pgtype.Timestamptz
	}{
		{name: "access token expired, no refresh token"},
		{name: "refresh token lifetime ended", refresh: &refreshToken, refreshExpires: past},
		{name: "authorization lifetime ended", refresh: &refreshToken, refreshExpires: future, authorizationExpires: past},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, fx := seedMetaValidationFixture(t, "verify-lapsed-"+uuid.NewString()[:8])
			var refresh pgtype.Text
			if tc.refresh != nil {
				encrypted, err := fx.ti.enc.Encrypt([]byte(*tc.refresh))
				require.NoError(t, err)
				refresh = conv.ToPGText(encrypted)
			}
			storeVerifyGrant(t, ctx, fx, refresh, tc.refreshExpires, tc.authorizationExpires)

			// A page opened before the grant lapsed still has Verify on it.
			requireVerifyNotice(t, fx, fx.clientID, verifyExpiredCard)
			page := normalizeSpaces(renderConsent(t, fx))
			require.Regexp(t, `data-connect-link > Reconnect`, page)
		})
	}
}

// invalid_grant clears the refresh token during Verify, so the same request sees a lapsed grant.
func TestServeConsentAction_VerifyInvalidGrantAsksToReconnect(t *testing.T) {
	t.Parallel()

	ctx, fx := seedMetaValidationFixture(t, "verify-invalid-grant")
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":"invalid_grant"}`)
	}))
	t.Cleanup(tokenServer.Close)
	pointVerifyAtTokenEndpoint(t, ctx, fx, tokenServer.URL)
	refresh, err := fx.ti.enc.Encrypt([]byte("refresh-token"))
	require.NoError(t, err)
	storeVerifyGrant(t, ctx, fx, conv.ToPGText(refresh), pgtype.Timestamptz{}, pgtype.Timestamptz{})

	requireVerifyNotice(t, fx, fx.clientID, verifyExpiredCard)
	require.Regexp(t, `data-connect-link > Reconnect`, normalizeSpaces(renderConsent(t, fx)))
}

// A service with no grant at all has a Connect button.
func TestServeConsentAction_VerifyNeverConnectedAsksToConnect(t *testing.T) {
	t.Parallel()

	ctx, fx := seedMetaValidationFixture(t, "verify-never")
	projectID, orgID := consentTestTenant(t, ctx)
	client := createConsentRemoteClient(t, ctx, fx.ti.conn, projectID, orgID, "verify-never-client", "", []uuid.UUID{fx.endpoint.UserSessionIssuerID})
	issuer := clientRemoteIssuerID(t, ctx, fx.ti.conn, projectID, orgID, client)
	createMetaMember(t, ctx, fx.ti.conn, projectID, fx.endpoint.MetaMcpServerID.UUID, "verify-never-other", "https://never.example.com/mcp", conv.ToNullUUID(issuer), 1)

	fx.server = "verify-never-other"
	requireVerifyNotice(t, fx, client, verifyNotConnectedCard)
	require.Regexp(t, `data-connect-link > Connect`, normalizeSpaces(renderConsent(t, fx)))
}

// A registration the server offers through a typed auth client is verified and
// disconnected from that offer. Only its own token is presented, whether or not
// the sign-in links another client that holds one.
func TestServeConsentAction_TypedRegistrationCardActionsWithoutASignInLink(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		sibling bool
	}{
		{name: "no linked client"},
		{name: "a linked client holds a token", sibling: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			prefix := "typed-verify-" + uuid.NewString()[:8]
			ctx, fx := seedStandaloneValidationFixture(t, prefix)
			projectID, orgID := consentTestTenant(t, ctx)
			linked, shared := fx.clientID, fx.endpoint.UserSessionIssuerID
			if !tc.sibling {
				require.NoError(t, remotesessions_repo.New(fx.ti.conn).DeleteUserSessionIssuerAttachmentsForRemoteSessionClient(ctx, remotesessions_repo.DeleteUserSessionIssuerAttachmentsForRemoteSessionClientParams{RemoteSessionClientID: linked, ProjectID: conv.ToNullUUID(projectID)}))
			}

			registration := createConsentRemoteClient(t, ctx, fx.ti.conn, projectID, orgID, prefix+"-typed", "", nil)
			fixtures := authclientfixtures.New(fx.ti.conn)
			authClientID, err := fixtures.SeedAuthClientFixture(ctx, authclientfixtures.SeedAuthClientFixtureParams{OrganizationID: orgID, ProjectID: conv.ToNullUUID(projectID), ClientType: "remote_session", Source: "admin", RemoteSessionClientID: conv.ToNullUUID(registration)})
			require.NoError(t, err)
			require.NoError(t, fixtures.SeedServerAuthClientLinkFixture(ctx, authclientfixtures.SeedServerAuthClientLinkFixtureParams{ProjectID: projectID, OrganizationID: orgID, McpServerID: fx.endpoint.McpServerID.UUID, AuthClientID: authClientID, Source: "admin"}))
			fx.ti.service.SetAuthClientPermitted(true)
			_, err = productfeaturesrepo.New(fx.ti.conn).EnableFeature(ctx, productfeaturesrepo.EnableFeatureParams{OrganizationID: orgID, FeatureName: string(productfeatures.FeatureUpstreamAuthEnforced)})
			require.NoError(t, err)
			insertQualifiedRemoteSessionToken(t, ctx, fx.ti, shared, registration, fx.subject, "typed-token", fx.member.url)
			fx.clientID = registration

			require.Contains(t, renderConsent(t, fx), `data-validate-link`, "the connected card offers Verify")

			fx.member.set(memberAccepts)
			requireValidated(t, fx)
			requireProbe(t, fx.member.drain(), "typed-token", true, true)
			require.Equal(t, "valid", storedSession(t, ctx, fx).ValidationStatus.String)

			form := url.Values{"state": {fx.stateID}, "csrf_token": {"csrf-token"}, "action": {"disconnect"}, "client_id": {registration.String()}}
			req := httptest.NewRequest(http.MethodPost, "/mcp/"+fx.endpoint.Slug+"/connect/remote-session", strings.NewReader(form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			w := httptest.NewRecorder()
			require.NoError(t, fx.ti.service.ServeConsentAction(w, req, fx.endpoint))
			require.Equal(t, http.StatusSeeOther, w.Code)

			q := remotesessions_repo.New(fx.ti.conn)
			_, err = q.GetActiveRemoteSession(ctx, remotesessions_repo.GetActiveRemoteSessionParams{SubjectUrn: fx.subject, RemoteSessionClientID: registration})
			require.ErrorIs(t, err, pgx.ErrNoRows, "the card's own grant is gone")
			_, err = q.GetActiveRemoteSession(ctx, remotesessions_repo.GetActiveRemoteSessionParams{SubjectUrn: fx.subject, RemoteSessionClientID: linked})
			require.NoError(t, err, "another client's grant is untouched")
		})
	}
}

// postRefresh posts the card's Refresh and returns where it sends the browser.
func postRefresh(t *testing.T, fx validationFixture, clientID uuid.UUID) string {
	t.Helper()
	form := url.Values{"state": {fx.stateID}, "csrf_token": {"csrf-token"}, "action": {"refresh"}, "client_id": {clientID.String()}}
	if fx.server != "" {
		form.Set("server", fx.server)
	}
	req := httptest.NewRequest(http.MethodPost, "/mcp/"+fx.endpoint.Slug+"/connect/remote-session", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	require.NoError(t, fx.ti.service.ServeConsentAction(w, req, fx.endpoint))
	require.Equal(t, http.StatusSeeOther, w.Code)
	return w.Header().Get("Location")
}

// Refresh on a sign-in whose refresh token can't be read points to the card's own control, not a dead end.
func TestServeConsentAction_RefreshUnreadableSignInPointsToTheCard(t *testing.T) {
	t.Parallel()

	t.Run("expired card offers Reconnect", func(t *testing.T) {
		t.Parallel()
		ctx, fx := seedMetaValidationFixture(t, "refresh-unreadable-"+uuid.NewString()[:8])
		storeVerifyGrant(t, ctx, fx, conv.ToPGText("not-ciphertext"), pgtype.Timestamptz{}, pgtype.Timestamptz{})

		location := postRefresh(t, fx, fx.clientID)
		require.Contains(t, location, "refresh_reconnect="+fx.clientID.String())
		page := strings.ReplaceAll(renderConsentAt(t, fx, location), "&#39;", "'")
		require.Contains(t, page, "Your saved sign-in can't be used anymore. Reconnect this service to fix it.")
		require.NotContains(t, page, "re-link")
	})
	t.Run("connected card offers Disconnect", func(t *testing.T) {
		t.Parallel()
		ctx, fx := seedMetaValidationFixture(t, "refresh-unreadable-live-"+uuid.NewString()[:8])
		access, err := fx.ti.enc.Encrypt([]byte("live-token"))
		require.NoError(t, err)
		_, err = remotesessions_repo.New(fx.ti.conn).UpsertRemoteSession(ctx, remotesessions_repo.UpsertRemoteSessionParams{
			SubjectUrn: fx.subject, UserSessionIssuerID: fx.endpoint.UserSessionIssuerID, RemoteSessionClientID: fx.clientID,
			AccessTokenEncrypted: access, AccessExpiresAt: conv.ToPGTimestamptz(time.Now().Add(time.Hour)),
			RefreshTokenEncrypted: conv.ToPGText("not-ciphertext"), Scopes: []string{}, Resource: conv.ToPGText(fx.member.url),
		})
		require.NoError(t, err)

		location := postRefresh(t, fx, fx.clientID)
		require.Contains(t, location, "refresh_reconnect="+fx.clientID.String())
		require.Contains(t, strings.ReplaceAll(renderConsentAt(t, fx, location), "&#39;", "'"), "Your saved sign-in can't be renewed. Disconnect this service, then connect it again.")
	})
}

// Verify answers a renewal the configuration refused the same way whether or not the access token can be read.
func TestServeConsentAction_VerifyMisconfiguredRenewalMatchesReadableGrant(t *testing.T) {
	t.Parallel()

	refused := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"unauthorized_client"}`))
	}))
	t.Cleanup(refused.Close)
	verify := func(name string, access string, accessExpiresIn time.Duration) url.Values {
		ctx, fx := seedMetaValidationFixture(t, name+"-"+uuid.NewString()[:8])
		pointVerifyAtTokenEndpoint(t, ctx, fx, refused.URL)
		if access != "not-ciphertext" {
			sealed, err := fx.ti.enc.Encrypt([]byte(access))
			require.NoError(t, err)
			access = sealed
		}
		refresh, err := fx.ti.enc.Encrypt([]byte("refresh-token"))
		require.NoError(t, err)
		_, err = remotesessions_repo.New(fx.ti.conn).UpsertRemoteSession(ctx, remotesessions_repo.UpsertRemoteSessionParams{
			SubjectUrn: fx.subject, UserSessionIssuerID: fx.endpoint.UserSessionIssuerID, RemoteSessionClientID: fx.clientID,
			AccessTokenEncrypted: access, AccessExpiresAt: conv.ToPGTimestamptz(time.Now().Add(accessExpiresIn)),
			RefreshTokenEncrypted: conv.ToPGText(refresh), Scopes: []string{}, Resource: conv.ToPGText(fx.member.url),
		})
		require.NoError(t, err)
		w, err := postValidate(t, fx, fx.clientID)
		require.NoError(t, err)
		require.Equal(t, http.StatusSeeOther, w.Code)
		location, err := url.Parse(w.Header().Get("Location"))
		require.NoError(t, err)
		query := location.Query()
		require.Empty(t, query.Get("validate_unfinished"), "retrying can't fix a refused renewal")
		require.Equal(t, fx.clientID.String(), query.Get("validate_failed"))
		return query
	}

	readable := verify("verify-misconfigured-readable", "stale-token", -time.Minute)
	unreadable := verify("verify-misconfigured-unreadable", "not-ciphertext", time.Hour)
	require.Equal(t, readable.Get("validate_notice"), unreadable.Get("validate_notice"))
}
