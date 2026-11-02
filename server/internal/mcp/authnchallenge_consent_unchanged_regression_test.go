// First-party pages still auto-close unless a grant could gain an identity on reconnect.

package mcp_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	remotesessions_repo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	tunneledmcprepo "github.com/speakeasy-api/gram/server/internal/tunneledmcp/repo"
)

// firstPartyConsent re-mints the fixture's consent challenge as a first-party
// one, the shape ServeFirstPartyConnect stores: no client, no redirect.
func firstPartyConsent(t *testing.T, ctx context.Context, fx consentActionFixture) consentActionFixture {
	t.Helper()
	stateID := uuid.NewString()
	subject := fx.subject
	require.NoError(t, fx.ti.authnChallengeCache.Store(ctx, mcp.AuthnChallengeState{
		ID:                  stateID,
		UserSessionIssuerID: fx.shared,
		Endpoint: mcp.EndpointRef{
			McpSlug:        fx.endpoint.Slug,
			RouteBase:      fx.endpoint.RouteBase,
			CustomDomainID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		},
		ClientID:            "",
		RedirectURI:         "",
		CodeChallenge:       "",
		CodeChallengeMethod: "",
		CSRFToken:           "csrf-token",
		Subject:             &subject,
		FirstParty:          true,
		CreatedAt:           time.Now(),
	}))
	fx.stateID = stateID
	return fx
}

// markVerified records a valid verdict on the subject's grant, as a probe
// would: a page waits on an unverified grant its server can check.
func markVerified(t *testing.T, ctx context.Context, fx consentActionFixture, clientID uuid.UUID) {
	t.Helper()
	repo := remotesessions_repo.New(fx.ti.conn)
	session, err := repo.GetActiveRemoteSession(ctx, remotesessions_repo.GetActiveRemoteSessionParams{SubjectUrn: fx.subject, RemoteSessionClientID: clientID})
	require.NoError(t, err)
	rows, err := repo.SetRemoteSessionValidation(ctx, remotesessions_repo.SetRemoteSessionValidationParams{
		LastValidatedAt:       pgtype.Timestamptz{Time: time.Now(), Valid: true, InfinityModifier: pgtype.Finite},
		ValidationStatus:      "valid",
		ValidationReason:      pgtype.Text{String: "", Valid: false},
		ID:                    session.ID,
		SubjectUrn:            fx.subject,
		RemoteSessionClientID: clientID,
		ExpectedUpdatedAt:     session.UpdatedAt,
		ProjectID:             fx.projectID,
		OrganizationID:        fx.orgID,
	})
	require.NoError(t, err)
	require.EqualValues(t, 1, rows)
}

func expectFirstPartyAutoClose(t *testing.T, fx consentActionFixture) {
	t.Helper()
	code, html, _ := render(t, fx)
	require.Equal(t, http.StatusOK, code, html)
	require.Contains(t, html, "1 of 1 connected")
	require.Contains(t, html, "data-auto-close", "a fully connected first-party page closes itself")
	require.Contains(t, html, "Connection complete. This tab will close automatically.")
	require.NotContains(t, html, consentIdentityCopy)
	require.NotContains(t, html, "> Reconnect </button>")
}

func TestServeConsent_Unchanged_FirstPartyAutoClosesWhenGrantAlreadyCarriesOpenID(t *testing.T) {
	t.Parallel()
	ctx, fx, metaID, clientID, issuerID := metaConsent(t, "unchanged-fp-openid")
	fx = focusClaimingMember(t, ctx, firstPartyConsent(t, ctx, fx), metaID, issuerID, "unchanged-fp-openid-member")

	advertise(t, ctx, fx, issuerID, []string{"read", "openid"})
	grantScoped(t, ctx, fx, clientID, []string{"read", "openid"})
	markVerified(t, ctx, fx, clientID)
	expectFirstPartyAutoClose(t, fx)
}

func TestServeConsent_Unchanged_FirstPartyAutoClosesWhenIssuerDoesNotAdvertiseOpenID(t *testing.T) {
	t.Parallel()
	ctx, fx, metaID, clientID, issuerID := metaConsent(t, "unchanged-fp-no-openid")
	fx = focusClaimingMember(t, ctx, firstPartyConsent(t, ctx, fx), metaID, issuerID, "unchanged-fp-no-openid-member")

	advertise(t, ctx, fx, issuerID, []string{"read"})
	grantScoped(t, ctx, fx, clientID, []string{"read"})
	markVerified(t, ctx, fx, clientID)
	expectFirstPartyAutoClose(t, fx)
}

// refreshableConnection gives the fixture subject's verified grant a refresh token
// and the access token deadline expires, against provider.
func refreshableConnection(t *testing.T, ctx context.Context, fx consentActionFixture, clientID uuid.UUID, expires time.Time, provider http.HandlerFunc) {
	t.Helper()
	server := httptest.NewServer(provider)
	t.Cleanup(server.Close)
	rows, err := testrepo.New(fx.ti.conn).ForceRemoteSessionIssuerTokenEndpointFixture(ctx, testrepo.ForceRemoteSessionIssuerTokenEndpointFixtureParams{
		TokenEndpoint: conv.ToPGText(server.URL), RemoteSessionClientID: clientID, ProjectID: fx.projectID, OrganizationID: fx.orgID,
	})
	require.NoError(t, err)
	require.EqualValues(t, 1, rows)

	repo := remotesessions_repo.New(fx.ti.conn)
	session, err := repo.GetActiveRemoteSession(ctx, remotesessions_repo.GetActiveRemoteSessionParams{SubjectUrn: fx.subject, RemoteSessionClientID: clientID})
	require.NoError(t, err)
	refresh, err := fx.ti.enc.Encrypt([]byte("live-refresh"))
	require.NoError(t, err)
	_, err = repo.UpsertRemoteSession(ctx, remotesessions_repo.UpsertRemoteSessionParams{
		SubjectUrn: fx.subject, UserSessionIssuerID: session.UserSessionIssuerID, RemoteSessionClientID: clientID,
		AccessTokenEncrypted: session.AccessTokenEncrypted, AccessExpiresAt: pgtype.Timestamptz{Time: expires, Valid: true, InfinityModifier: pgtype.Finite},
		RefreshTokenEncrypted: conv.ToPGText(refresh), Scopes: session.Scopes, Resource: session.Resource,
	})
	require.NoError(t, err)
	markVerified(t, ctx, fx, clientID)
}

func outageConsentFixture(t *testing.T, name string) (context.Context, consentActionFixture, uuid.UUID) {
	t.Helper()
	ctx, fx, metaID, clientID, issuerID := metaConsent(t, name)
	fx = focusClaimingMember(t, ctx, firstPartyConsent(t, ctx, fx), metaID, issuerID, name+"-member")
	advertise(t, ctx, fx, issuerID, []string{"read"})
	grantScoped(t, ctx, fx, clientID, []string{"read"})
	markVerified(t, ctx, fx, clientID)
	return ctx, fx, clientID
}

func serviceUnavailable(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusServiceUnavailable)
}

func requireUnavailablePage(t *testing.T, page string) {
	t.Helper()
	require.Contains(t, page, "Unavailable · try again later")
	require.Contains(t, page, "> Reconnect </button>")
	require.Contains(t, page, "0 of 1 connected")
	require.NotContains(t, page, "Service connected")
	require.NotContains(t, page, "data-auto-close")
	require.NotContains(t, page, "Connection complete")
}

// pressRefresh is a person's own Refresh on the connect page; it returns the redirect.
func pressRefresh(t *testing.T, fx consentActionFixture, clientID uuid.UUID) (string, error) {
	t.Helper()
	form := url.Values{"state": {fx.stateID}, "csrf_token": {"csrf-token"}, "action": {"refresh"}, "client_id": {clientID.String()}, "server": {fx.server}}
	req := httptest.NewRequest(http.MethodPost, "/mcp/"+fx.endpoint.Slug+"/connect/remote-session", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	err := fx.ti.service.ServeConsentAction(w, req, fx.endpoint)
	return w.Header().Get("Location"), err
}

func visitConsent(t *testing.T, fx consentActionFixture, target string) string {
	t.Helper()
	w := httptest.NewRecorder()
	require.NoError(t, fx.ti.service.ServeConsent(w, httptest.NewRequest(http.MethodGet, target, nil), fx.endpoint))
	require.Equal(t, http.StatusOK, w.Code)
	return strings.Join(strings.Fields(w.Body.String()), " ")
}

// A token that simply expired changes nothing on load: no POST, the verdict stands, the tab closes.
func TestServeConsent_ExpiredHealthyConnectionMakesNoProviderCall(t *testing.T) {
	t.Parallel()
	ctx, fx, clientID := outageConsentFixture(t, "expired-healthy")
	var posts atomic.Int32
	refreshableConnection(t, ctx, fx, clientID, time.Now().Add(-time.Minute), func(w http.ResponseWriter, _ *http.Request) {
		posts.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	})

	for range 2 {
		expectFirstPartyAutoClose(t, fx)
		_, page, _ := render(t, fx)
		require.Contains(t, page, `data-validation="valid"`)
	}
	require.Zero(t, posts.Load())
}

// A failed call records the outage; both pages read it, and a reconnect replaces it.
func TestServeConsent_RecordedOutageTellsTheTruthThenReconnectClears(t *testing.T) {
	t.Parallel()
	ctx, fx, clientID := outageConsentFixture(t, "outage-fp")
	refreshableConnection(t, ctx, fx, clientID, time.Now().Add(-time.Minute), serviceUnavailable)
	_, err := pressRefresh(t, fx, clientID)
	require.NoError(t, err)
	markVerified(t, ctx, fx, clientID)

	_, page, _ := render(t, fx)
	requireUnavailablePage(t, page)

	list := fx
	list.server = ""
	_, listPage, _ := render(t, list)
	require.Contains(t, listPage, "Unavailable · try again later")
	require.Contains(t, listPage, "> Reconnect </button>")
	require.Contains(t, listPage, "0 of 1")
	require.NotContains(t, listPage, "data-auto-close")

	grantScoped(t, ctx, fx, clientID, []string{"read"})
	markVerified(t, ctx, fx, clientID)
	expectFirstPartyAutoClose(t, fx)
}

// A tunnel outage offers no Reconnect: it would go through the same tunnel.
func TestServeConsent_TunnelOutageOffersNoReconnect(t *testing.T) {
	t.Parallel()
	ctx, fx, clientID := outageConsentFixture(t, "outage-tunnel")
	refreshableConnection(t, ctx, fx, clientID, time.Now().Add(-time.Minute), serviceUnavailable)
	session, err := remotesessions_repo.New(fx.ti.conn).GetActiveRemoteSession(ctx, remotesessions_repo.GetActiveRemoteSessionParams{SubjectUrn: fx.subject, RemoteSessionClientID: clientID})
	require.NoError(t, err)
	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)
	require.NoError(t, cache.NewRedisCacheAdapter(redisClient).Set(ctx, remotesessions.RefreshOutageKey(session), remotesessions.RefreshOutageTunnel, time.Minute))

	list := fx
	list.server = ""
	for _, view := range []consentActionFixture{fx, list} {
		_, page, _ := render(t, view)
		require.Contains(t, page, "Unavailable · try again later")
		require.NotContains(t, page, "> Reconnect </button>")
		require.NotContains(t, page, "data-auto-close")
		require.NotContains(t, strings.ToLower(page), "tunnel is", "the provider behind it may be what failed")
		require.NotContains(t, strings.ToLower(page), "your tunnel")
	}
	_, listPage, _ := render(t, list)
	require.Contains(t, listPage, "data-gateway-server-page", "Refresh stays one page away")
}

// A tunnel with no live route reads as down from the route table, with no record and no POST.
func TestServeConsent_TunnelDownWithoutRecordOffersNoReconnect(t *testing.T) {
	t.Parallel()
	ctx, fx, metaID, clientID, issuerID := metaConsent(t, "tunnel-down")
	fx = focusClaimingMember(t, ctx, firstPartyConsent(t, ctx, fx), metaID, issuerID, "tunnel-down-member")
	advertise(t, ctx, fx, issuerID, []string{"read"})
	grantScoped(t, ctx, fx, clientID, []string{"read"})
	markVerified(t, ctx, fx, clientID)
	var posts atomic.Int32
	refreshableConnection(t, ctx, fx, clientID, time.Now().Add(-time.Minute), func(w http.ResponseWriter, _ *http.Request) {
		posts.Add(1)
	})
	tunnel, err := tunneledmcprepo.New(fx.ti.conn).CreateServer(ctx, tunneledmcprepo.CreateServerParams{
		ID: uuid.New(), ProjectID: fx.projectID, Name: "tunnel-down-" + uuid.NewString(), KeyHash: "hash-" + uuid.NewString(), KeyPrefix: "test",
		ResourceIdentifier: pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err)
	_, err = remotesessions_repo.New(fx.ti.conn).UpdateRemoteSessionIssuer(ctx, remotesessions_repo.UpdateRemoteSessionIssuerParams{
		ID: issuerID, ProjectID: conv.ToNullUUID(fx.projectID), TunneledMcpServerID: conv.ToPGText(tunnel.ID.String()),
	})
	require.NoError(t, err)

	_, page, _ := render(t, fx)
	require.Contains(t, page, "Unavailable · try again later")
	require.NotContains(t, page, "> Reconnect </button>")
	require.NotContains(t, page, "data-auto-close")
	require.Zero(t, posts.Load())
}

// A rejected verdict stands on an expired token with nothing on record.
func TestServeConsent_RejectedVerdictStaysOnExpiredToken(t *testing.T) {
	t.Parallel()
	ctx, fx, clientID := outageConsentFixture(t, "expired-rejected")
	var posts atomic.Int32
	refreshableConnection(t, ctx, fx, clientID, time.Now().Add(-time.Minute), func(w http.ResponseWriter, _ *http.Request) {
		posts.Add(1)
	})
	repo := remotesessions_repo.New(fx.ti.conn)
	session, err := repo.GetActiveRemoteSession(ctx, remotesessions_repo.GetActiveRemoteSessionParams{SubjectUrn: fx.subject, RemoteSessionClientID: clientID})
	require.NoError(t, err)
	rows, err := repo.SetRemoteSessionValidation(ctx, remotesessions_repo.SetRemoteSessionValidationParams{
		LastValidatedAt:  pgtype.Timestamptz{Time: time.Now().Add(time.Second), Valid: true, InfinityModifier: pgtype.Finite},
		ValidationStatus: "rejected_by_member", ValidationReason: conv.ToPGText("Rejected by the server"),
		ID: session.ID, SubjectUrn: fx.subject, RemoteSessionClientID: clientID, ExpectedUpdatedAt: session.UpdatedAt, ProjectID: fx.projectID, OrganizationID: fx.orgID,
	})
	require.NoError(t, err)
	require.EqualValues(t, 1, rows)

	_, page, _ := render(t, fx)
	require.Contains(t, page, "Rejected by the server — reconnect to continue")
	require.Contains(t, page, "> Reconnect </button>")
	require.Zero(t, posts.Load())
}

// A refused refresh clears the grant where it happens, so the page asks to reconnect on its own.
func TestServeConsent_RefusedRefreshAsksToReconnect(t *testing.T) {
	t.Parallel()
	ctx, fx, clientID := outageConsentFixture(t, "outage-refused")
	refreshableConnection(t, ctx, fx, clientID, time.Now().Add(-time.Minute), func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
	})
	_, err := pressRefresh(t, fx, clientID)
	require.Error(t, err)

	list := fx
	list.server = ""
	for _, view := range []consentActionFixture{fx, list} {
		_, page, _ := render(t, view)
		require.Contains(t, page, "> Reconnect </button>")
		require.NotContains(t, page, "Service connected")
		require.NotContains(t, page, "Unavailable · try again later")
		require.NotContains(t, page, "data-auto-close")
	}
}

// A person's own Refresh gets the plain message; a later success clears the record,
// and its redirect URL can't keep saying so.
func TestServeConsentAction_ManualRefreshDuringOutageThenSuccessClears(t *testing.T) {
	t.Parallel()
	ctx, fx, clientID := outageConsentFixture(t, "outage-refresh")
	var down atomic.Bool
	down.Store(true)
	refreshableConnection(t, ctx, fx, clientID, time.Now().Add(-time.Minute), func(w http.ResponseWriter, _ *http.Request) {
		if down.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"fresh-access","refresh_token":"fresh-refresh","token_type":"Bearer","expires_in":3600}`))
	})

	redirect, err := pressRefresh(t, fx, clientID)
	require.NoError(t, err)
	during := visitConsent(t, fx, redirect)
	requireUnavailablePage(t, during)
	require.Contains(t, during, "Your last refresh didn&#39;t finish")
	require.NotContains(t, during, "Couldn&#39;t connect")

	down.Store(false)
	_, err = pressRefresh(t, fx, clientID)
	require.NoError(t, err)
	recovered := visitConsent(t, fx, redirect)
	require.NotContains(t, recovered, "Unavailable · try again later")
	require.Contains(t, recovered, "1 of 1 connected")

	spoofed := visitConsent(t, fx, "/mcp/"+fx.endpoint.Slug+"/connect?state="+url.QueryEscape(fx.stateID)+"&server="+url.QueryEscape(fx.server)+"&refresh_unavailable="+uuid.NewString())
	require.NotContains(t, spoofed, "Your last refresh didn&#39;t finish", "only for a client on this page")
}

// The notice after a failed Refresh stays readable: the tab can't close over it.
func TestServeConsentAction_RefreshNoticeKeepsTheTabOpen(t *testing.T) {
	t.Parallel()
	ctx, fx, clientID := outageConsentFixture(t, "outage-notice")
	refreshableConnection(t, ctx, fx, clientID, time.Now().Add(time.Hour), serviceUnavailable)

	redirect, err := pressRefresh(t, fx, clientID)
	require.NoError(t, err)
	page := visitConsent(t, fx, redirect)
	require.Contains(t, page, "Your last refresh didn&#39;t finish")
	require.Contains(t, page, "1 of 1 connected")
	require.NotContains(t, page, "data-auto-close")
}
