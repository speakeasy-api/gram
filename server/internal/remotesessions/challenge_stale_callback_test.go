// A remote-login callback that lands after its user session issuer was
// deleted must be rejected: storing the exchanged tokens would resurrect a
// grant the orphan-revoke cascade can no longer reach or revoke. What it must
// not do is strand them — the pair was minted moments earlier and only the
// callback still holds it.

package remotesessions_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

type callbackFixture struct {
	ti             *testInstance
	mgr            *remotesessions.ChallengeManager
	projectID      uuid.UUID
	organizationID string
	userIssuer     uuid.UUID
	clientID       uuid.UUID
	subject        urn.SessionSubject
	state          string
	// exchanges counts code-for-token exchanges the fake provider served.
	exchanges *atomic.Int64
}

// seedRemoteLoginInFlight leaves a login parked at the provider: the client is
// registered and bound, the authorization URL is minted, and nothing has come
// back yet. Revocations land on the shared revocation spy, so a test can tell
// a stored grant from one handed straight back.
func seedRemoteLoginInFlight(t *testing.T, slug string, spy *revocationSpy) (context.Context, callbackFixture) {
	t.Helper()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	revokeServer := newRevocationUpstream(t, spy)

	var exchanges atomic.Int64
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		exchanges.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"cb-access","token_type":"Bearer","expires_in":3600,"refresh_token":"cb-refresh"}`))
	}))
	t.Cleanup(tokenServer.Close)

	enc := testenv.NewEncryptionClient(t)
	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), []string{})
	require.NoError(t, err)
	mgr := remotesessions.NewChallengeManager(
		testenv.NewLogger(t),
		testenv.NewTracerProvider(t),
		testenv.NewMeterProvider(t),
		ti.conn,
		enc,
		policy,
		ti.redisCache,
		mustURL(t, "http://localhost"),
	)

	q := repo.New(ti.conn)
	issuer, err := q.CreateRemoteSessionIssuer(ctx, repo.CreateRemoteSessionIssuerParams{
		ProjectID:                         conv.ToNullUUID(*authCtx.ProjectID),
		Slug:                              slug + "-remote",
		Issuer:                            tokenServer.URL,
		AuthorizationEndpoint:             conv.ToPGText(tokenServer.URL + "/authorize"),
		TokenEndpoint:                     conv.ToPGText(tokenServer.URL + "/token"),
		RevocationEndpoint:                conv.ToPGText(revokeServer.URL + "/revoke"),
		ScopesSupported:                   []string{"openid"},
		GrantTypesSupported:               []string{"authorization_code", "refresh_token"},
		ResponseTypesSupported:            []string{"code"},
		TokenEndpointAuthMethodsSupported: []string{"client_secret_post"},
	})
	require.NoError(t, err)

	userIssuer := createUserSessionIssuer(t, ctx, ti.conn, slug+"-usi")

	secretCiphertext, err := enc.Encrypt([]byte(slug + "-secret"))
	require.NoError(t, err)
	client, err := q.CreateRemoteSessionClient(ctx, repo.CreateRemoteSessionClientParams{
		ProjectID:               conv.ToNullUUID(*authCtx.ProjectID),
		OrganizationID:          conv.ToPGTextEmpty(authCtx.ActiveOrganizationID),
		RemoteSessionIssuerID:   issuer.ID,
		ClientID:                slug + "-cid",
		ClientSecretEncrypted:   conv.ToPGText(secretCiphertext),
		ClientIDIssuedAt:        conv.ToPGTimestamptz(time.Now()),
		ClientSecretExpiresAt:   pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false},
		TokenEndpointAuthMethod: conv.ToPGText("client_secret_post"),
	})
	require.NoError(t, err)
	require.NoError(t, q.AttachRemoteSessionClientToUserSessionIssuer(ctx, repo.AttachRemoteSessionClientToUserSessionIssuerParams{
		RemoteSessionClientID: client.ID,
		UserSessionIssuerID:   userIssuer,
	}))

	clients, err := mgr.ListClients(ctx, *authCtx.ProjectID, authCtx.ActiveOrganizationID, userIssuer)
	require.NoError(t, err)
	require.Len(t, clients, 1)

	subject := urn.NewUserSubject(slug + "-subject")
	authURL, err := mgr.BuildAuthorizationUrl(ctx, remotesessions.ParentChallenge{
		ID:                  slug + "-parent",
		ProjectID:           *authCtx.ProjectID,
		OrganizationID:      authCtx.ActiveOrganizationID,
		UserSessionIssuerID: userIssuer,
		Subject:             &subject,
		McpSlug:             slug,
		RouteBase:           "mcp",
	}, clients[0])
	require.NoError(t, err)

	parsed, err := url.Parse(authURL)
	require.NoError(t, err)
	state := parsed.Query().Get("state")
	require.NotEmpty(t, state)

	return ctx, callbackFixture{
		ti:             ti,
		mgr:            mgr,
		projectID:      *authCtx.ProjectID,
		organizationID: authCtx.ActiveOrganizationID,
		userIssuer:     userIssuer,
		clientID:       client.ID,
		subject:        subject,
		state:          state,
		exchanges:      &exchanges,
	}
}

func callbackRequest(ctx context.Context, fx callbackFixture, code string) *http.Request {
	req := httptest.NewRequest(http.MethodGet,
		"/mcp/remote_login_callback?code="+code+"&state="+url.QueryEscape(fx.state), nil)
	return req.WithContext(ctx)
}

func TestHandleRemoteLoginCallback_RejectsStateWhoseIssuerWasDeleted(t *testing.T) {
	t.Parallel()

	spy := &revocationSpy{}
	ctx, fx := seedRemoteLoginInFlight(t, "stale-cb", spy)

	// The issuer dies while the user is off at the upstream provider. The
	// binding row survives here (unlike the full delete handler) so the
	// callback's rejection is provably keyed on issuer liveness, not on the
	// link row's absence.
	_, err := usersessionsrepo.New(fx.ti.conn).DeleteUserSessionIssuer(ctx, usersessionsrepo.DeleteUserSessionIssuerParams{
		ID:        fx.userIssuer,
		ProjectID: fx.projectID,
	})
	require.NoError(t, err)

	err = fx.mgr.HandleRemoteLoginCallback(httptest.NewRecorder(), callbackRequest(ctx, fx, "stale-code"))
	require.ErrorContains(t, err, "no longer exists", "the stale callback is rejected instead of stored")
	require.Equal(t, int64(1), fx.exchanges.Load(), "rejection happens at persist time, after the code exchange")

	_, err = repo.New(fx.ti.conn).GetActiveRemoteSession(ctx, repo.GetActiveRemoteSessionParams{
		SubjectUrn:            fx.subject,
		RemoteSessionClientID: fx.clientID,
	})
	require.ErrorIs(t, err, pgx.ErrNoRows, "no remote_sessions row is resurrected for the dead issuer")

	require.Equal(t, []string{"cb-refresh"}, spy.revokedTokens(), "the pair Gram refused to store is handed back to the provider")
}

// The callback and the issuer delete take the client row and the issuer row in
// opposite orders, so the issuer lock has to stay compatible with the foreign
// key check the session insert performs. Neither outcome may leave a live
// upstream grant behind: here the write wins the race and the cascade sweeps it.
func TestHandleRemoteLoginCallback_ConcurrentIssuerDeleteSweepsTheStoredGrant(t *testing.T) {
	t.Parallel()

	spy := &revocationSpy{}
	ctx, fx := seedRemoteLoginInFlight(t, "cb-race", spy)

	tx, err := fx.ti.conn.Begin(ctx) //nolint:glint // the raw-SQL rule catches tx.Exec with a query string; this transaction only ever runs SQLc-generated methods, and it exists to replay an issuer delete around the live callback
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback(context.Background()) })

	// The issuer delete's first lock, taken while the user is still at the
	// provider and held for the whole callback.
	txIssuers := usersessionsrepo.New(tx)
	_, err = txIssuers.LockUserSessionIssuer(ctx, usersessionsrepo.LockUserSessionIssuerParams{
		ID:        fx.userIssuer,
		ProjectID: fx.projectID,
	})
	require.NoError(t, err)

	done := make(chan error, 1)
	go func() {
		done <- fx.mgr.HandleRemoteLoginCallback(httptest.NewRecorder(), callbackRequest(ctx, fx, "race-code"))
	}()

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(30 * time.Second):
		t.Fatal("the callback is stuck behind the issuer-delete lock: the two would deadlock in production")
	}

	stored, err := repo.New(fx.ti.conn).GetActiveRemoteSession(ctx, repo.GetActiveRemoteSessionParams{
		SubjectUrn:            fx.subject,
		RemoteSessionClientID: fx.clientID,
	})
	require.NoError(t, err, "the callback committed while the delete held the issuer lock")

	// The rest of the delete, now that the callback has released the client row.
	_, err = txIssuers.DeleteUserSessionIssuer(ctx, usersessionsrepo.DeleteUserSessionIssuerParams{
		ID:        fx.userIssuer,
		ProjectID: fx.projectID,
	})
	require.NoError(t, err)

	revoker := newTestUpstreamRevoker(t, fx.ti)
	creds, err := revoker.DetachUserSessionIssuerFromClients(ctx, tx, fx.userIssuer, fx.projectID, fx.organizationID)
	require.NoError(t, err)
	require.Len(t, creds, 1, "the grant written mid-delete is picked up by the orphan scan")
	require.Equal(t, stored.RemoteSessionClientID, creds[0].RemoteSessionClientID)
	require.NoError(t, tx.Commit(ctx))

	revoker.RevokeAllDetached(ctx, creds)

	_, err = repo.New(fx.ti.conn).GetActiveRemoteSession(ctx, repo.GetActiveRemoteSessionParams{
		SubjectUrn:            fx.subject,
		RemoteSessionClientID: fx.clientID,
	})
	require.ErrorIs(t, err, pgx.ErrNoRows, "no grant survives the delete it raced")

	require.Equal(t, []string{"cb-refresh"}, spy.revokedTokens())
}
