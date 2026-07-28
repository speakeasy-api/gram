// tokenservice_resolve_test.go covers ResolveAccessTokens, the multi-client
// MCP-runtime resolver that returns one upstream token per
// remote_session_issuer linked to a user_session_issuer (keyed by
// remote_session_issuer_id), and its "any attached session missing/invalid →
// ErrNoValidToken" rule.
//
// The single-client happy path is the only multiplicity reachable today: the
// remote_session_client_user_session_issuers one_per_issuer unique index still
// caps a user_session_issuer at one client. The map-with-many-entries and the
// per-issuer uniqueness invariant become exercisable once AIS-137 drops that
// index.

package remotesessions_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func newResolveManager(t *testing.T, conn *pgxpool.Pool, enc *encryption.Client) *remotesessions.ChallengeManager {
	t.Helper()

	tracerProvider := testenv.NewTracerProvider(t)
	policy, err := guardian.NewUnsafePolicy(tracerProvider, []string{})
	require.NoError(t, err)
	return remotesessions.NewChallengeManager(
		testenv.NewLogger(t),
		conn,
		enc,
		policy,
		cache.NoopCache,
		mustURL(t, "http://localhost"),
	)
}

// seedActiveClient creates a remote_session_issuer + remote_session_client
// (attached to userIssuerID through the join table) and returns the client id
// and its remote_session_issuer id.
func seedActiveClient(t *testing.T, ctx context.Context, conn *pgxpool.Pool, projectID, userIssuerID uuid.UUID, organizationID, slug string) (clientID, remoteIssuerID uuid.UUID) {
	t.Helper()

	q := repo.New(conn)
	issuer, err := q.CreateRemoteSessionIssuer(ctx, repo.CreateRemoteSessionIssuerParams{
		ProjectID:                         uuid.NullUUID{UUID: projectID, Valid: true},
		Slug:                              slug,
		Issuer:                            "https://issuer.example.com/" + slug,
		AuthorizationEndpoint:             conv.ToPGText("https://issuer.example.com/authorize"),
		TokenEndpoint:                     conv.ToPGText("https://issuer.example.com/token"),
		RegistrationEndpoint:              pgtype.Text{String: "", Valid: false},
		JwksUri:                           pgtype.Text{String: "", Valid: false},
		ScopesSupported:                   []string{"openid"},
		GrantTypesSupported:               []string{"authorization_code"},
		ResponseTypesSupported:            []string{"code"},
		TokenEndpointAuthMethodsSupported: []string{"client_secret_post"},
		Oidc:                              false,
		Passthrough:                       false,
	})
	require.NoError(t, err)

	client, err := q.CreateRemoteSessionClient(ctx, repo.CreateRemoteSessionClientParams{
		ProjectID:               conv.ToNullUUID(projectID),
		OrganizationID:          conv.ToPGTextEmpty(organizationID),
		RemoteSessionIssuerID:   issuer.ID,
		ClientID:                "cid-" + slug,
		ClientSecretEncrypted:   pgtype.Text{String: "", Valid: false},
		ClientIDIssuedAt:        conv.ToPGTimestamptz(time.Now()),
		ClientSecretExpiresAt:   pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false},
		TokenEndpointAuthMethod: conv.ToPGText("client_secret_post"),
		Scope:                   nil,
		Audience:                pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err)

	err = q.AttachRemoteSessionClientToUserSessionIssuer(ctx, repo.AttachRemoteSessionClientToUserSessionIssuerParams{
		RemoteSessionClientID: client.ID,
		UserSessionIssuerID:   userIssuerID,
	})
	require.NoError(t, err)

	return client.ID, issuer.ID
}

type runtimeRefreshFixture struct {
	manager  *remotesessions.ChallengeManager
	subject  urn.SessionSubject
	clientID uuid.UUID
	session  repo.RemoteSession
}

func seedRuntimeRefreshFixture(t *testing.T, ctx context.Context, ti *testInstance, slug string, tokenHandler http.HandlerFunc) runtimeRefreshFixture {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	tokenServer := httptest.NewServer(tokenHandler)
	t.Cleanup(tokenServer.Close)

	q := repo.New(ti.conn)
	issuer, err := q.CreateRemoteSessionIssuer(ctx, repo.CreateRemoteSessionIssuerParams{
		ProjectID:                         conv.ToNullUUID(*authCtx.ProjectID),
		OrganizationID:                    conv.ToPGText(authCtx.ActiveOrganizationID),
		Slug:                              slug,
		Issuer:                            tokenServer.URL,
		AuthorizationEndpoint:             conv.ToPGText(tokenServer.URL + "/authorize"),
		TokenEndpoint:                     conv.ToPGText(tokenServer.URL + "/token"),
		ScopesSupported:                   []string{"openid"},
		GrantTypesSupported:               []string{"authorization_code", "refresh_token"},
		ResponseTypesSupported:            []string{"code"},
		TokenEndpointAuthMethodsSupported: []string{"none"},
	})
	require.NoError(t, err)

	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "usi-"+slug)
	client, err := q.CreateRemoteSessionClient(ctx, repo.CreateRemoteSessionClientParams{
		ProjectID:               conv.ToNullUUID(*authCtx.ProjectID),
		OrganizationID:          conv.ToPGText(authCtx.ActiveOrganizationID),
		RemoteSessionIssuerID:   issuer.ID,
		ClientID:                "cid-" + slug,
		ClientSecretEncrypted:   pgtype.Text{},
		ClientIDIssuedAt:        conv.ToPGTimestamptz(time.Now()),
		ClientSecretExpiresAt:   pgtype.Timestamptz{},
		TokenEndpointAuthMethod: conv.ToPGText("none"),
		Scope:                   []string{"openid"},
		Audience:                pgtype.Text{},
	})
	require.NoError(t, err)
	require.NoError(t, q.AttachRemoteSessionClientToUserSessionIssuer(ctx, repo.AttachRemoteSessionClientToUserSessionIssuerParams{
		RemoteSessionClientID: client.ID,
		UserSessionIssuerID:   userIssuerID,
	}))

	enc := testenv.NewEncryptionClient(t)
	accessEncrypted, err := enc.Encrypt([]byte("expired-access"))
	require.NoError(t, err)
	refreshEncrypted, err := enc.Encrypt([]byte("rotating-refresh"))
	require.NoError(t, err)
	subject := urn.NewUserSubject("subject-" + slug)
	session, err := q.UpsertRemoteSession(ctx, repo.UpsertRemoteSessionParams{
		SubjectUrn:            subject,
		UserSessionIssuerID:   userIssuerID,
		RemoteSessionClientID: client.ID,
		AccessTokenEncrypted:  accessEncrypted,
		AccessExpiresAt:       conv.ToPGTimestamptz(time.Now().Add(-time.Minute)),
		RefreshTokenEncrypted: conv.ToPGText(refreshEncrypted),
		RefreshExpiresAt:      pgtype.Timestamptz{},
		Scopes:                []string{"openid"},
	})
	require.NoError(t, err)

	tracerProvider := testenv.NewTracerProvider(t)
	policy, err := guardian.NewUnsafePolicy(tracerProvider, []string{})
	require.NoError(t, err)

	return runtimeRefreshFixture{
		manager: remotesessions.NewChallengeManager(
			testenv.NewLogger(t),
			ti.conn,
			enc,
			policy,
			ti.redisCache,
			mustURL(t, "http://localhost"),
		),
		subject:  subject,
		clientID: client.ID,
		session:  session,
	}
}

func TestResolveAccessToken_ConcurrentRefreshIsSerialized(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	var requests atomic.Int32
	requestStarted := make(chan struct{})
	allowResponse := make(chan struct{})
	fixture := seedRuntimeRefreshFixture(t, ctx, ti, "runtime-refresh-concurrent", func(w http.ResponseWriter, _ *http.Request) {
		if requests.Add(1) == 1 {
			close(requestStarted)
		}
		<-allowResponse
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"rotated-access","refresh_token":"rotated-refresh","expires_in":3600}`))
	})

	type result struct {
		token string
		err   error
	}
	const callers = 8
	results := make(chan result, callers)
	resolve := func() {
		token, err := fixture.manager.ResolveAccessToken(ctx, fixture.clientID, fixture.subject, "")
		results <- result{token: token, err: err}
	}

	go resolve()
	select {
	case <-requestStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("first refresh request did not reach upstream")
	}

	for range callers - 1 {
		go resolve()
	}
	require.Never(t, func() bool {
		return requests.Load() > 1
	}, 100*time.Millisecond, 5*time.Millisecond)
	close(allowResponse)

	for range callers {
		select {
		case got := <-results:
			require.NoError(t, got.err)
			require.Equal(t, "rotated-access", got.token)
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for concurrent token resolution")
		}
	}
	require.EqualValues(t, 1, requests.Load())
}

func TestResolveAccessToken_InvalidGrantRevokesDeadSession(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	var requests atomic.Int32
	fixture := seedRuntimeRefreshFixture(t, ctx, ti, "runtime-refresh-invalid-grant", func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":"invalid_grant","error_description":"Unknown or invalid refresh token."}`))
	})

	token, err := fixture.manager.ResolveAccessToken(ctx, fixture.clientID, fixture.subject, "")
	require.NoError(t, err)
	require.Empty(t, token)

	_, err = repo.New(ti.conn).GetActiveRemoteSession(ctx, repo.GetActiveRemoteSessionParams{
		SubjectUrn:            fixture.subject,
		RemoteSessionClientID: fixture.clientID,
	})
	require.ErrorIs(t, err, pgx.ErrNoRows)

	// A second MCP request sees no active session and does not retry the dead
	// upstream refresh token. This breaks the repeated 401/refresh loop.
	token, err = fixture.manager.ResolveAccessToken(ctx, fixture.clientID, fixture.subject, "")
	require.NoError(t, err)
	require.Empty(t, token)
	require.EqualValues(t, 1, requests.Load())

	// A subsequent upstream OAuth callback can materialize a fresh active row
	// because the dead row no longer participates in the partial unique index.
	freshAccess, err := testenv.NewEncryptionClient(t).Encrypt([]byte("fresh-access"))
	require.NoError(t, err)
	fresh, err := repo.New(ti.conn).UpsertRemoteSession(ctx, repo.UpsertRemoteSessionParams{
		SubjectUrn:            fixture.subject,
		UserSessionIssuerID:   fixture.session.UserSessionIssuerID,
		RemoteSessionClientID: fixture.clientID,
		AccessTokenEncrypted:  freshAccess,
		AccessExpiresAt:       conv.ToPGTimestamptz(time.Now().Add(time.Hour)),
		RefreshTokenEncrypted: pgtype.Text{},
		RefreshExpiresAt:      pgtype.Timestamptz{},
		Scopes:                []string{"openid"},
	})
	require.NoError(t, err)
	require.NotEqual(t, fixture.session.ID, fresh.ID)
}

func TestResolveAccessTokens_SingleClientHappyPath(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	enc := testenv.NewEncryptionClient(t)
	mgr := newResolveManager(t, ti.conn, enc)

	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "usi-resolve-happy")
	clientID, remoteIssuerID := seedActiveClient(t, ctx, ti.conn, *authCtx.ProjectID, userIssuerID, authCtx.ActiveOrganizationID, "rsi-resolve-happy")

	subject := urn.NewUserSubject("resolve-happy-subject")
	accessEnc, err := enc.Encrypt([]byte("upstream-access-token"))
	require.NoError(t, err)
	_, err = repo.New(ti.conn).InsertRemoteSession(ctx, repo.InsertRemoteSessionParams{
		SubjectUrn:            subject,
		UserSessionIssuerID:   userIssuerID,
		RemoteSessionClientID: clientID,
		AccessTokenEncrypted:  accessEnc,
		AccessExpiresAt:       pgtype.Timestamptz{Time: time.Now().Add(time.Hour), InfinityModifier: pgtype.Finite, Valid: true},
	})
	require.NoError(t, err)

	tokens, err := mgr.ResolveAccessTokens(ctx, *authCtx.ProjectID, authCtx.ActiveOrganizationID, userIssuerID, subject, "")
	require.NoError(t, err)
	require.Equal(t, map[uuid.UUID]string{remoteIssuerID: "upstream-access-token"}, tokens)
}

func TestResolveAccessTokens_NoClientsReturnsNil(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	mgr := newResolveManager(t, ti.conn, testenv.NewEncryptionClient(t))

	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "usi-resolve-empty")
	subject := urn.NewUserSubject("resolve-empty-subject")

	tokens, err := mgr.ResolveAccessTokens(ctx, *authCtx.ProjectID, authCtx.ActiveOrganizationID, userIssuerID, subject, "")
	require.NoError(t, err)
	require.Nil(t, tokens)
}

func TestResolveAccessTokens_MissingSessionReturnsErrNoValidToken(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	mgr := newResolveManager(t, ti.conn, testenv.NewEncryptionClient(t))

	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "usi-resolve-missing")
	// Client bound, but the subject has never linked an upstream session.
	seedActiveClient(t, ctx, ti.conn, *authCtx.ProjectID, userIssuerID, authCtx.ActiveOrganizationID, "rsi-resolve-missing")

	subject := urn.NewUserSubject("resolve-missing-subject")
	tokens, err := mgr.ResolveAccessTokens(ctx, *authCtx.ProjectID, authCtx.ActiveOrganizationID, userIssuerID, subject, "")
	require.ErrorIs(t, err, remotesessions.ErrNoValidToken)
	require.Nil(t, tokens)
}

// TestResolveAccessTokens_TenantClientOnPlatformIssuer proves an existing remote
// session on a tenant client that points at a platform issuer resolves its
// upstream token through the unchanged runtime path.
func TestResolveAccessTokens_TenantClientOnPlatformIssuer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	enc := testenv.NewEncryptionClient(t)
	mgr := newResolveManager(t, ti.conn, enc)

	platformID := seedGlobalRemoteIssuer(t, ctx, ti.conn, "resolve-platform")
	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "resolve-platform-usi")
	clientID := createRemoteClient(t, ctx, ti, platformID.String(), userIssuerID.String(), "resolve-platform-client")

	subject := urn.NewUserSubject("resolve-platform-subject")
	accessEnc, err := enc.Encrypt([]byte("platform-upstream-token"))
	require.NoError(t, err)
	_, err = repo.New(ti.conn).InsertRemoteSession(ctx, repo.InsertRemoteSessionParams{
		SubjectUrn:            subject,
		UserSessionIssuerID:   userIssuerID,
		RemoteSessionClientID: uuid.MustParse(clientID),
		AccessTokenEncrypted:  accessEnc,
		AccessExpiresAt:       pgtype.Timestamptz{Time: time.Now().Add(time.Hour), InfinityModifier: pgtype.Finite, Valid: true},
	})
	require.NoError(t, err)

	tokens, err := mgr.ResolveAccessTokens(ctx, *authCtx.ProjectID, authCtx.ActiveOrganizationID, userIssuerID, subject, "")
	require.NoError(t, err)
	require.Equal(t, map[uuid.UUID]string{platformID: "platform-upstream-token"}, tokens)
}
