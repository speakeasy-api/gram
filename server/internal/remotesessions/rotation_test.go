package remotesessions_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/remotesessionmetrics"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	tunneledmcprepo "github.com/speakeasy-api/gram/server/internal/tunneledmcp/repo"
)

// rotationUpstream is a fake issuer whose token endpoint answers refresh
// grants with a configurable OAuth error and whose registration endpoint
// hands out a replacement client. Both live on the one httptest server the
// synthetic login fixture points the issuer's token_endpoint at.
type rotationUpstream struct {
	// refreshStatus and refreshBody answer every refresh_token grant.
	refreshStatus int
	refreshBody   string

	// registrationSecretExpiresAt is the client_secret_expires_at the
	// registration endpoint reports; zero means far in the future.
	registrationSecretExpiresAt int64

	refreshAttempts      atomic.Int32
	registrationAttempts atomic.Int32
	// lastRegistration keeps the most recent RFC 7591 request body.
	lastRegistration atomic.Pointer[map[string]any]
}

func (u *rotationUpstream) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/register" {
			u.registrationAttempts.Add(1)
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			u.lastRegistration.Store(&body)
			expiresAt := u.registrationSecretExpiresAt
			if expiresAt == 0 {
				expiresAt = 4102444800 // 2100-01-01
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_, _ = fmt.Fprintf(w, `{"client_id":"rotated-cid","token_endpoint_auth_method":"none","client_id_issued_at":1700000000,"client_secret_expires_at":%d}`, expiresAt)
			return
		}
		if err := r.ParseForm(); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if r.Form.Get("grant_type") != "refresh_token" {
			_, _ = w.Write([]byte(`{"access_token":"expired-access","refresh_token":"dead-refresh"}`))
			return
		}
		u.refreshAttempts.Add(1)
		w.WriteHeader(u.refreshStatus)
		_, _ = w.Write([]byte(u.refreshBody))
	}
}

const invalidClientBody = `{"error":"invalid_client","error_description":"Client not found"}`

// stageRegistration publishes registrationEndpoint on the fixture client's
// issuer (an empty value stages an issuer that publishes none) and optionally
// marks the client already rejected upstream or past its secret expiry.
func stageRegistration(t *testing.T, env syntheticExpiryEnv, registrationEndpoint string, rejectedAt, secretExpiresAt *time.Time) {
	t.Helper()
	ctx := t.Context()
	n, err := env.q.ForceRemoteSessionIssuerRegistrationEndpointFixture(ctx, repo.ForceRemoteSessionIssuerRegistrationEndpointFixtureParams{
		RegistrationEndpoint: conv.ToPGTextEmpty(registrationEndpoint),
		ClientID:             env.clientID,
	})
	require.NoError(t, err)
	require.EqualValues(t, 1, n)
	n, err = env.q.ForceRemoteSessionClientRegistrationFixture(ctx, repo.ForceRemoteSessionClientRegistrationFixtureParams{
		ClientSecretExpiresAt: conv.PtrToPGTimestamptz(secretExpiresAt),
		UpstreamRejectedAt:    conv.PtrToPGTimestamptz(rejectedAt),
		ID:                    env.clientID,
	})
	require.NoError(t, err)
	require.EqualValues(t, 1, n)
}

// mintLogin runs the consent-screen connect leg again for the fixture client
// and returns the client_id the authorize redirect carries.
func mintLogin(t *testing.T, env syntheticExpiryEnv) string {
	t.Helper()
	return mintLoginFor(t, env, listClient(t, env))
}

// listClient reads the fixture client the way the consent screen does. A test
// that stages a concurrent writer between this read and mintLoginFor holds
// the stale snapshot a login race produces.
func listClient(t *testing.T, env syntheticExpiryEnv) remotesessions.Client {
	t.Helper()
	clients, err := env.mgr.ListClients(t.Context(), env.projectID, env.organizationID, env.session.UserSessionIssuerID)
	require.NoError(t, err)
	require.Len(t, clients, 1)
	return clients[0]
}

// mintLoginFor runs the connect leg for the given client snapshot and returns
// the client_id the authorize redirect carries.
func mintLoginFor(t *testing.T, env syntheticExpiryEnv, client remotesessions.Client) string {
	t.Helper()
	ctx := t.Context()
	authURL, err := env.mgr.BuildAuthorizationUrl(ctx, remotesessions.ParentChallenge{
		ID:                  uuid.NewString(),
		ProjectID:           env.projectID,
		OrganizationID:      env.organizationID,
		UserSessionIssuerID: env.session.UserSessionIssuerID,
		Subject:             &env.subject,
		McpSlug:             "rotation-mcp",
		FinalRedirectURI:    "",
		Resource:            "",
	}, client)
	require.NoError(t, err)
	parsed, err := url.Parse(authURL)
	require.NoError(t, err)
	return parsed.Query().Get("client_id")
}

func loadClient(t *testing.T, env syntheticExpiryEnv) repo.RemoteSessionClient {
	t.Helper()
	row, err := env.q.GetRemoteSessionClientForRotation(t.Context(), env.clientID)
	require.NoError(t, err)
	return row.RemoteSessionClient
}

// issuerTokenEndpoint is the fake issuer's base URL: the fixture points the
// issuer's token_endpoint at the root of the one httptest server.
func issuerTokenEndpoint(t *testing.T, env syntheticExpiryEnv) string {
	t.Helper()
	row, err := env.q.GetRemoteSessionClientForRotation(t.Context(), env.clientID)
	require.NoError(t, err)
	require.True(t, row.IssuerTokenEndpoint.Valid)
	return row.IssuerTokenEndpoint.String
}

// An upstream invalid_client on refresh means the issuer has forgotten the
// client, not just this grant: the attempt is filed as invalid_client, the
// dead grant is cleared like an invalid_grant would be, and the client row is
// marked so the next login re-registers it.
func TestRefreshNow_InvalidClient_MarksClientAndClearsGrant(t *testing.T) {
	t.Parallel()

	upstream := &rotationUpstream{refreshStatus: http.StatusUnauthorized, refreshBody: invalidClientBody}
	ctx, env := newSyntheticExpiryEnv(t, "refresh-invalid-client", upstream.handler())

	_, err := env.refresher.RefreshNow(ctx, env.session, "", remotesessionmetrics.RefreshTriggerScheduled)
	require.Error(t, err)
	var failure *remotesessions.RefreshError
	require.ErrorAs(t, err, &failure)
	require.Equal(t, remotesessionmetrics.RefreshOutcomeInvalidClient, failure.Outcome)

	active, err := env.q.GetActiveRemoteSession(ctx, repo.GetActiveRemoteSessionParams{
		SubjectUrn:            env.subject,
		RemoteSessionClientID: env.clientID,
	})
	require.NoError(t, err)
	require.False(t, active.RefreshTokenEncrypted.Valid, "a grant issued to a forgotten client can never be redeemed")

	client := loadClient(t, env)
	require.True(t, client.UpstreamRejectedAt.Valid, "the rejection is recorded on the client so the next login rotates it")
	require.Equal(t, "synthetic-cid-refresh-invalid-client", client.ClientID, "the refresh path only marks; it never rotates on its own")
}

// A rejected client whose issuer publishes a registration endpoint is replaced
// before the authorize redirect is minted: the issuer is probed to confirm it
// really has forgotten the client, a replacement is registered at the issuer's
// endpoint, the row is updated in place, and the sessions minted against the
// old client_id are revoked.
func TestBuildAuthorizationUrl_RotatesRejectedRegistration(t *testing.T) {
	t.Parallel()

	upstream := &rotationUpstream{refreshStatus: http.StatusUnauthorized, refreshBody: invalidClientBody}
	ctx, env := newSyntheticExpiryEnv(t, "rotate-rejected", upstream.handler())
	rejectedAt := time.Now().Add(-time.Hour)
	stageRegistration(t, env, issuerTokenEndpoint(t, env)+"/register", &rejectedAt, nil)

	require.Equal(t, "rotated-cid", mintLogin(t, env))

	require.EqualValues(t, 1, upstream.refreshAttempts.Load(), "the rejection is confirmed against the token endpoint before anything changes")
	require.EqualValues(t, 1, upstream.registrationAttempts.Load())
	registration := *upstream.lastRegistration.Load()
	require.Equal(t, "none", registration["token_endpoint_auth_method"], "the replacement is registered with the stored auth method")

	client := loadClient(t, env)
	require.Equal(t, "rotated-cid", client.ClientID)
	require.False(t, client.UpstreamRejectedAt.Valid)
	require.True(t, client.ClientSecretExpiresAt.Valid, "the issuer's new expiry is recorded")
	require.False(t, client.LegacyCallbackUrl)

	_, err := env.q.GetActiveRemoteSession(ctx, repo.GetActiveRemoteSessionParams{
		SubjectUrn:            env.subject,
		RemoteSessionClientID: env.clientID,
	})
	require.ErrorIs(t, err, pgx.ErrNoRows, "sessions bound to the old client_id are revoked")
}

// A rejection the issuer does not confirm is not acted on: the probe finds
// the client still authenticates (the issuer answers invalid_grant for the
// made-up refresh token), nothing is registered, and the marker is cleared so
// later logins stop probing.
func TestBuildAuthorizationUrl_KeepsRegistrationTheIssuerStillRecognizes(t *testing.T) {
	t.Parallel()

	upstream := &rotationUpstream{refreshStatus: http.StatusBadRequest, refreshBody: `{"error":"invalid_grant","error_description":"Unknown refresh token"}`}
	_, env := newSyntheticExpiryEnv(t, "rotate-recognized", upstream.handler())
	rejectedAt := time.Now().Add(-time.Hour)
	stageRegistration(t, env, issuerTokenEndpoint(t, env)+"/register", &rejectedAt, nil)

	require.Equal(t, "synthetic-cid-rotate-recognized", mintLogin(t, env))

	require.EqualValues(t, 1, upstream.refreshAttempts.Load())
	require.Zero(t, upstream.registrationAttempts.Load())

	client := loadClient(t, env)
	require.Equal(t, "synthetic-cid-rotate-recognized", client.ClientID)
	require.False(t, client.UpstreamRejectedAt.Valid, "a rejection the issuer does not confirm is cleared")
}

// A secret the issuer said has expired is rotated without a probe: the
// expiry came from the issuer itself, and the stored secret can no longer
// authenticate a probe anyway.
func TestBuildAuthorizationUrl_RotatesExpiredSecretWithoutProbe(t *testing.T) {
	t.Parallel()

	upstream := &rotationUpstream{refreshStatus: http.StatusUnauthorized, refreshBody: invalidClientBody}
	_, env := newSyntheticExpiryEnv(t, "rotate-expired", upstream.handler())
	expiredAt := time.Now().Add(-time.Minute)
	stageRegistration(t, env, issuerTokenEndpoint(t, env)+"/register", nil, &expiredAt)

	require.Equal(t, "rotated-cid", mintLogin(t, env))

	require.Zero(t, upstream.refreshAttempts.Load(), "an issuer-reported expiry needs no confirmation")
	require.EqualValues(t, 1, upstream.registrationAttempts.Load())
	require.Equal(t, "rotated-cid", loadClient(t, env).ClientID)
}

// An issuer that publishes no registration endpoint gives Gram nowhere to
// re-register: a rejected client under it goes to the authorize endpoint
// unchanged, and the issuer is not contacted.
func TestBuildAuthorizationUrl_NeverRotatesWithoutIssuerRegistrationEndpoint(t *testing.T) {
	t.Parallel()

	upstream := &rotationUpstream{refreshStatus: http.StatusUnauthorized, refreshBody: invalidClientBody}
	_, env := newSyntheticExpiryEnv(t, "rotate-no-endpoint", upstream.handler())
	rejectedAt := time.Now().Add(-time.Hour)
	stageRegistration(t, env, "", &rejectedAt, nil)

	require.Equal(t, "synthetic-cid-rotate-no-endpoint", mintLogin(t, env))

	require.Zero(t, upstream.refreshAttempts.Load())
	require.Zero(t, upstream.registrationAttempts.Load())
	client := loadClient(t, env)
	require.True(t, client.UpstreamRejectedAt.Valid, "the marker stays for an administrator to act on")
}

// A registration endpoint that refuses the replacement leaves the client as
// it was, so the login fails at the issuer the way it would have anyway
// rather than half-rotated.
func TestBuildAuthorizationUrl_KeepsClientWhenReRegistrationFails(t *testing.T) {
	t.Parallel()

	upstream := &rotationUpstream{refreshStatus: http.StatusUnauthorized, refreshBody: invalidClientBody}
	_, env := newSyntheticExpiryEnv(t, "rotate-register-fails", upstream.handler())
	rejectedAt := time.Now().Add(-time.Hour)
	// A path the fake issuer does not serve as a registration endpoint.
	stageRegistration(t, env, issuerTokenEndpoint(t, env)+"/missing", &rejectedAt, nil)

	require.Equal(t, "synthetic-cid-rotate-register-fails", mintLogin(t, env))

	require.EqualValues(t, 1, upstream.refreshAttempts.Load(), "the rejection was confirmed before the registration was attempted")
	client := loadClient(t, env)
	require.Equal(t, "synthetic-cid-rotate-register-fails", client.ClientID)
	require.True(t, client.UpstreamRejectedAt.Valid, "the marker stays so the next login tries again")
}

// An issuer that reports an expiry at or before the issuance would otherwise
// leave the replacement "expired" the moment it lands, and every later login
// would rotate again and revoke every session each time. The expiry is
// discarded instead, so a second login reuses the replacement.
func TestBuildAuthorizationUrl_PastExpiryOnReplacementDoesNotRotateAgain(t *testing.T) {
	t.Parallel()

	upstream := &rotationUpstream{refreshStatus: http.StatusUnauthorized, refreshBody: invalidClientBody, registrationSecretExpiresAt: 1700000000}
	_, env := newSyntheticExpiryEnv(t, "rotate-past-expiry", upstream.handler())
	expiredAt := time.Now().Add(-time.Minute)
	stageRegistration(t, env, issuerTokenEndpoint(t, env)+"/register", nil, &expiredAt)

	require.Equal(t, "rotated-cid", mintLogin(t, env))
	require.Equal(t, "rotated-cid", mintLogin(t, env))

	require.EqualValues(t, 1, upstream.registrationAttempts.Load(), "the replacement is registered once")
	client := loadClient(t, env)
	require.False(t, client.ClientSecretExpiresAt.Valid, "an expiry at or before issuance is recorded as no expiry")
}

// A probe the issuer cannot answer (a 5xx) is inconclusive: nothing is
// registered, the marker stays for the next login to try again, and the
// stored client is sent as-is.
func TestBuildAuthorizationUrl_InconclusiveProbeChangesNothing(t *testing.T) {
	t.Parallel()

	upstream := &rotationUpstream{refreshStatus: http.StatusServiceUnavailable, refreshBody: `{"error":"temporarily_unavailable"}`}
	_, env := newSyntheticExpiryEnv(t, "rotate-probe-5xx", upstream.handler())
	rejectedAt := time.Now().Add(-time.Hour)
	stageRegistration(t, env, issuerTokenEndpoint(t, env)+"/register", &rejectedAt, nil)

	require.Equal(t, "synthetic-cid-rotate-probe-5xx", mintLogin(t, env))

	require.EqualValues(t, 1, upstream.refreshAttempts.Load())
	require.Zero(t, upstream.registrationAttempts.Load())
	client := loadClient(t, env)
	require.Equal(t, "synthetic-cid-rotate-probe-5xx", client.ClientID)
	require.True(t, client.UpstreamRejectedAt.Valid)
}

// A refresh the issuer accepts proves the client is still registered, so a
// rejection recorded earlier (a wrong secret an operator has since fixed, or
// an issuer having a bad minute) is cleared rather than left flagging the
// client for re-registration.
func TestRefreshNow_SuccessClearsUpstreamRejection(t *testing.T) {
	t.Parallel()

	upstream := &rotationUpstream{refreshStatus: http.StatusOK, refreshBody: `{"access_token":"fresh-access","refresh_token":"fresh-refresh","expires_in":3600}`}
	ctx, env := newSyntheticExpiryEnv(t, "refresh-clears-rejection", upstream.handler())
	rejectedAt := time.Now().Add(-time.Hour)
	stageRegistration(t, env, "", &rejectedAt, nil)

	result, err := env.refresher.RefreshNow(ctx, env.session, "", remotesessionmetrics.RefreshTriggerScheduled)
	require.NoError(t, err)
	require.Equal(t, remotesessionmetrics.RefreshOutcomeRefreshed, result.Outcome)

	client := loadClient(t, env)
	require.False(t, client.UpstreamRejectedAt.Valid, "a successful refresh clears the rejection marker")
}

// While another login holds the rotation lease, this one waits for the
// winner's replacement instead of sending its user to the authorize endpoint
// with the client being replaced: the redirect carries the winner's client_id
// and nothing is registered twice.
func TestBuildAuthorizationUrl_WaitsForConcurrentRotation(t *testing.T) {
	t.Parallel()

	upstream := &rotationUpstream{refreshStatus: http.StatusUnauthorized, refreshBody: invalidClientBody}
	ctx, env := newSyntheticExpiryEnv(t, "rotate-wait", upstream.handler())
	rejectedAt := time.Now().Add(-time.Hour)
	stageRegistration(t, env, issuerTokenEndpoint(t, env)+"/register", &rejectedAt, nil)

	// Hold the lease the way a concurrent login would, on the same Redis the
	// manager's lease cache uses.
	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)
	locks := cache.NewRedisCacheAdapter(redisClient)
	leaseKey := remotesessions.ClientRotationLeaseKey(env.clientID)
	held, err := locks.Add(ctx, leaseKey, 30*time.Second)
	require.NoError(t, err)
	require.True(t, held)
	t.Cleanup(func() { _ = locks.Delete(context.WithoutCancel(ctx), leaseKey) })

	// The winner lands its replacement shortly after this login starts waiting.
	// The timer stages the concurrent writer; it is not a wait for state, which
	// mintLogin below performs by polling the row.
	before := loadClient(t, env)
	winner := time.NewTimer(500 * time.Millisecond)
	go func() {
		<-winner.C
		_, _ = env.q.ReplaceRemoteSessionClientRegistration(context.WithoutCancel(ctx), repo.ReplaceRemoteSessionClientRegistrationParams{
			ClientID:                "rotated-by-winner",
			ClientSecretEncrypted:   pgtype.Text{String: "", Valid: false},
			ClientIDIssuedAt:        conv.ToPGTimestamptz(time.Now().UTC()),
			ClientSecretExpiresAt:   pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false},
			TokenEndpointAuthMethod: before.TokenEndpointAuthMethod,
			ID:                      before.ID,
			ExpectedClientID:        before.ClientID,
			ExpectedUpdatedAt:       before.UpdatedAt,
		})
	}()

	require.Equal(t, "rotated-by-winner", mintLogin(t, env))
	require.Zero(t, upstream.registrationAttempts.Load(), "the waiting login must not register a second replacement")
	require.Zero(t, upstream.refreshAttempts.Load(), "nor probe while the winner holds the lease")
}

// replaceAsWinner lands the replacement a concurrent rotation would: a new
// client_id, no rejection marker, and an expiry the issuer reported as far in
// the future. It is what a login that lost the race must find and adopt. It
// returns rather than asserts so a test can stage it from a goroutine.
func replaceAsWinner(ctx context.Context, env syntheticExpiryEnv, before repo.RemoteSessionClient) error {
	_, err := env.q.ReplaceRemoteSessionClientRegistration(context.WithoutCancel(ctx), repo.ReplaceRemoteSessionClientRegistrationParams{
		ClientID:                "rotated-by-winner",
		ClientSecretEncrypted:   pgtype.Text{String: "", Valid: false},
		ClientIDIssuedAt:        conv.ToPGTimestamptz(time.Now().UTC()),
		ClientSecretExpiresAt:   conv.ToPGTimestamptz(time.Now().Add(24 * time.Hour).UTC()),
		TokenEndpointAuthMethod: before.TokenEndpointAuthMethod,
		ID:                      before.ID,
		ExpectedClientID:        before.ClientID,
		ExpectedUpdatedAt:       before.UpdatedAt,
	})
	if err != nil {
		return fmt.Errorf("replace registration as winner: %w", err)
	}
	return nil
}

// A probe answer that says nothing about the client (a 429, or an OAuth
// error like temporarily_unavailable or server_error on a 4xx) must not count
// as recognition. The refresh that recorded the rejection already cleared the
// grant, so clearing the marker on such an answer would leave a client with no
// recorded expiry reconnecting with the dead client_id indefinitely.
func TestBuildAuthorizationUrl_InconclusiveProbeErrorKeepsRejection(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		status int
		body   string
	}{
		{name: "rate-limited", status: http.StatusTooManyRequests, body: `{"error":"temporarily_unavailable"}`},
		{name: "server-error-4xx", status: http.StatusBadRequest, body: `{"error":"server_error"}`},
		{name: "unknown-code", status: http.StatusBadRequest, body: `{"error":"try_again_later"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			upstream := &rotationUpstream{refreshStatus: tc.status, refreshBody: tc.body}
			_, env := newSyntheticExpiryEnv(t, "rotate-probe-"+tc.name, upstream.handler())
			rejectedAt := time.Now().Add(-time.Hour)
			stageRegistration(t, env, issuerTokenEndpoint(t, env)+"/register", &rejectedAt, nil)

			require.Equal(t, "synthetic-cid-rotate-probe-"+tc.name, mintLogin(t, env))

			require.EqualValues(t, 1, upstream.refreshAttempts.Load())
			require.Zero(t, upstream.registrationAttempts.Load())
			client := loadClient(t, env)
			require.True(t, client.UpstreamRejectedAt.Valid, "an inconclusive probe must leave the rejection marker for the next login to confirm")
		})
	}
}

// A login that decided to rotate on a stale snapshot, and only got the lease
// after another caller had already replaced the registration, adopts that
// replacement. Rotating again would register a third client and revoke the
// sessions users just re-established.
func TestBuildAuthorizationUrl_AdoptsConcurrentReplacementInsteadOfRotatingExpiredAgain(t *testing.T) {
	t.Parallel()

	upstream := &rotationUpstream{refreshStatus: http.StatusUnauthorized, refreshBody: invalidClientBody}
	_, env := newSyntheticExpiryEnv(t, "rotate-expired-stale", upstream.handler())
	expiredAt := time.Now().Add(-time.Minute)
	stageRegistration(t, env, issuerTokenEndpoint(t, env)+"/register", nil, &expiredAt)

	stale := listClient(t, env)
	require.NoError(t, replaceAsWinner(t.Context(), env, loadClient(t, env)))

	require.Equal(t, "rotated-by-winner", mintLoginFor(t, env, stale))

	require.Zero(t, upstream.registrationAttempts.Load(), "the replacement must not be rotated again")
	require.Zero(t, upstream.refreshAttempts.Load())
	require.Equal(t, "rotated-by-winner", loadClient(t, env).ClientID)
}

// The same race on a rejection: the winner replaced the registration, so the
// loser neither probes the replacement nor falls back to its snapshot, which
// would send its user to the issuer with the dead client_id.
func TestBuildAuthorizationUrl_AdoptsConcurrentReplacementInsteadOfProbingRejected(t *testing.T) {
	t.Parallel()

	upstream := &rotationUpstream{refreshStatus: http.StatusUnauthorized, refreshBody: invalidClientBody}
	_, env := newSyntheticExpiryEnv(t, "rotate-rejected-stale", upstream.handler())
	rejectedAt := time.Now().Add(-time.Hour)
	stageRegistration(t, env, issuerTokenEndpoint(t, env)+"/register", &rejectedAt, nil)

	stale := listClient(t, env)
	require.NoError(t, replaceAsWinner(t.Context(), env, loadClient(t, env)))

	require.Equal(t, "rotated-by-winner", mintLoginFor(t, env, stale))

	require.Zero(t, upstream.refreshAttempts.Load(), "the replacement is not probed")
	require.Zero(t, upstream.registrationAttempts.Load())
}

// An expiry-triggered login that finds the lease held waits for the winner's
// replacement like a rejection-triggered one does. It starts with no rejection
// marker, so the marker's absence must not be read as the rotation having
// finished, which would send the user out with the expired client at once.
func TestBuildAuthorizationUrl_WaitsForConcurrentExpiryRotation(t *testing.T) {
	t.Parallel()

	upstream := &rotationUpstream{refreshStatus: http.StatusUnauthorized, refreshBody: invalidClientBody}
	ctx, env := newSyntheticExpiryEnv(t, "rotate-expiry-wait", upstream.handler())
	expiredAt := time.Now().Add(-time.Minute)
	stageRegistration(t, env, issuerTokenEndpoint(t, env)+"/register", nil, &expiredAt)

	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)
	locks := cache.NewRedisCacheAdapter(redisClient)
	leaseKey := remotesessions.ClientRotationLeaseKey(env.clientID)
	held, err := locks.Add(ctx, leaseKey, 30*time.Second)
	require.NoError(t, err)
	require.True(t, held)
	t.Cleanup(func() { _ = locks.Delete(context.WithoutCancel(ctx), leaseKey) })

	before := loadClient(t, env)
	winner := time.NewTimer(500 * time.Millisecond)
	go func() {
		<-winner.C
		// Failures surface as the login timing out on the stale client below.
		_ = replaceAsWinner(ctx, env, before)
	}()

	started := time.Now()
	require.Equal(t, "rotated-by-winner", mintLogin(t, env))
	require.GreaterOrEqual(t, time.Since(started), 400*time.Millisecond, "the login must wait for the winner rather than return the expired client at once")
	require.Zero(t, upstream.registrationAttempts.Load())
	require.Zero(t, upstream.refreshAttempts.Load())
}

// The adoption check runs before the issuer is required to publish a
// registration endpoint. A loser whose winner already replaced the
// registration adopts it even if the issuer's metadata has since lost the
// endpoint; refusing first would hand the caller its stale snapshot with the
// dead client_id.
func TestBuildAuthorizationUrl_AdoptsConcurrentReplacementWhenIssuerLostRegistrationEndpoint(t *testing.T) {
	t.Parallel()

	upstream := &rotationUpstream{refreshStatus: http.StatusUnauthorized, refreshBody: invalidClientBody}
	ctx, env := newSyntheticExpiryEnv(t, "rotate-endpoint-lost", upstream.handler())
	rejectedAt := time.Now().Add(-time.Hour)
	stageRegistration(t, env, issuerTokenEndpoint(t, env)+"/register", &rejectedAt, nil)

	stale := listClient(t, env)
	require.NoError(t, replaceAsWinner(ctx, env, loadClient(t, env)))
	n, err := env.q.ForceRemoteSessionIssuerRegistrationEndpointFixture(ctx, repo.ForceRemoteSessionIssuerRegistrationEndpointFixtureParams{
		RegistrationEndpoint: pgtype.Text{String: "", Valid: false},
		ClientID:             env.clientID,
	})
	require.NoError(t, err)
	require.EqualValues(t, 1, n)

	require.Equal(t, "rotated-by-winner", mintLoginFor(t, env, stale))

	require.Zero(t, upstream.refreshAttempts.Load())
	require.Zero(t, upstream.registrationAttempts.Load())
}

// bindIssuerToTunnel points the fixture issuer at a tunneled MCP server, the
// way a platform admin does from the issuer settings page.
func bindIssuerToTunnel(t *testing.T, env syntheticExpiryEnv) {
	t.Helper()
	ctx := t.Context()

	tunnel, err := tunneledmcprepo.New(env.db).CreateServer(ctx, tunneledmcprepo.CreateServerParams{
		ID:                 uuid.New(),
		ProjectID:          env.projectID,
		Name:               "rotation tunnel " + uuid.NewString(),
		KeyHash:            "rotation-key-hash-" + uuid.NewString(),
		KeyPrefix:          "rotation-key-prefix",
		ResourceIdentifier: pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err)

	// Every other column keeps its stored value under the query's three-state
	// narg semantics.
	_, err = env.q.UpdateRemoteSessionIssuer(ctx, repo.UpdateRemoteSessionIssuerParams{
		ID:                  env.issuerID,
		ProjectID:           conv.ToNullUUID(env.projectID),
		TunneledMcpServerID: conv.ToPGText(tunnel.ID.String()),
	})
	require.NoError(t, err)
}

// A bound issuer's token and registration endpoints are private addresses, so
// both legs of a rotation ride the tunnel. The fixture manager holds no tunnel
// client, so they fail closed here — what matters is that neither leg is
// dialed over direct egress, which is where they would land if the rotator
// still ignored the binding. The unbound counterpart is
// TestBuildAuthorizationUrl_RotatesRejectedRegistration above.
func TestBuildAuthorizationUrl_RotationOfBoundIssuerNeverUsesDirectEgress(t *testing.T) {
	t.Parallel()

	upstream := &rotationUpstream{refreshStatus: http.StatusUnauthorized, refreshBody: invalidClientBody}
	_, env := newSyntheticExpiryEnv(t, "rotate-tunnel-bound", upstream.handler())
	rejectedAt := time.Now().Add(-time.Hour)
	stageRegistration(t, env, issuerTokenEndpoint(t, env)+"/register", &rejectedAt, nil)
	bindIssuerToTunnel(t, env)

	require.Equal(t, "synthetic-cid-rotate-tunnel-bound", mintLogin(t, env),
		"the stored client is sent onward, unrotated")

	require.Zero(t, upstream.refreshAttempts.Load(),
		"a bound issuer's token endpoint must not be probed over direct egress")
	require.Zero(t, upstream.registrationAttempts.Load(),
		"a bound issuer's registration endpoint must not be reached over direct egress")

	client := loadClient(t, env)
	require.Equal(t, "synthetic-cid-rotate-tunnel-bound", client.ClientID)
	require.True(t, client.UpstreamRejectedAt.Valid, "the marker stays so the next login tries again")
}
