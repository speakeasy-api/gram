// A remote-login callback that lands after its user session issuer was
// deleted must be rejected: storing the exchanged tokens would resurrect a
// grant the orphan-revoke cascade can no longer reach or revoke.

package remotesessions_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

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

func TestHandleRemoteLoginCallback_RejectsStateWhoseIssuerWasDeleted(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	exchanged := false
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		exchanged = true
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"stale-access","token_type":"Bearer","expires_in":3600,"refresh_token":"stale-refresh"}`))
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
		Slug:                              "stale-cb-remote",
		Issuer:                            tokenServer.URL,
		AuthorizationEndpoint:             conv.ToPGText(tokenServer.URL + "/authorize"),
		TokenEndpoint:                     conv.ToPGText(tokenServer.URL + "/token"),
		ScopesSupported:                   []string{"openid"},
		GrantTypesSupported:               []string{"authorization_code", "refresh_token"},
		ResponseTypesSupported:            []string{"code"},
		TokenEndpointAuthMethodsSupported: []string{"client_secret_post"},
	})
	require.NoError(t, err)

	userIssuer := createUserSessionIssuer(t, ctx, ti.conn, "stale-cb-usi")

	secretCiphertext, err := enc.Encrypt([]byte("stale-cb-secret"))
	require.NoError(t, err)
	client, err := q.CreateRemoteSessionClient(ctx, repo.CreateRemoteSessionClientParams{
		ProjectID:               conv.ToNullUUID(*authCtx.ProjectID),
		OrganizationID:          conv.ToPGTextEmpty(authCtx.ActiveOrganizationID),
		RemoteSessionIssuerID:   issuer.ID,
		ClientID:                "stale-cb-cid",
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

	subject := urn.NewUserSubject("stale-cb-subject")
	authURL, err := mgr.BuildAuthorizationUrl(ctx, remotesessions.ParentChallenge{
		ID:                  "stale-cb-parent",
		ProjectID:           *authCtx.ProjectID,
		OrganizationID:      authCtx.ActiveOrganizationID,
		UserSessionIssuerID: userIssuer,
		Subject:             &subject,
		McpSlug:             "stale-cb",
		RouteBase:           "mcp",
	}, clients[0])
	require.NoError(t, err)

	// The issuer dies while the user is off at the upstream provider. The
	// binding row survives here (unlike the full delete handler) so the
	// callback's rejection is provably keyed on issuer liveness, not on the
	// link row's absence.
	_, err = usersessionsrepo.New(ti.conn).DeleteUserSessionIssuer(ctx, usersessionsrepo.DeleteUserSessionIssuerParams{
		ID:        userIssuer,
		ProjectID: *authCtx.ProjectID,
	})
	require.NoError(t, err)

	parsed, err := url.Parse(authURL)
	require.NoError(t, err)
	state := parsed.Query().Get("state")
	require.NotEmpty(t, state)

	req := httptest.NewRequest(http.MethodGet,
		"/mcp/remote_login_callback?code=stale-code&state="+url.QueryEscape(state), nil)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	err = mgr.HandleRemoteLoginCallback(w, req)
	require.ErrorContains(t, err, "no longer exists", "the stale callback is rejected instead of stored")
	require.True(t, exchanged, "rejection happens at persist time, after the code exchange")

	_, err = q.GetActiveRemoteSession(ctx, repo.GetActiveRemoteSessionParams{
		SubjectUrn:            subject,
		RemoteSessionClientID: client.ID,
	})
	require.ErrorIs(t, err, pgx.ErrNoRows, "no remote_sessions row is resurrected for the dead issuer")
}
