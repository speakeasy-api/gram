// challenge_resource_e2e_test.go drives ListClients → BuildAuthorizationUrl →
// HandleRemoteLoginCallback against a real ChallengeManager + database to
// assert that the RFC 8707 `resource` parameter from the parent challenge is
// attached to the authorize redirect and repeated on the code exchange, and
// omitted on both legs when the parent carries none.

package remotesessions_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type resourceDanceFixture struct {
	mgr     *remotesessions.ChallengeManager
	parent  remotesessions.ParentChallenge
	clients []remotesessions.Client
}

// setupResourceDanceFixture seeds an issuer (whose token endpoint is the
// spy's httptest server) plus a bound client, and returns a parent challenge
// carrying the given resource.
func setupResourceDanceFixture(t *testing.T, resource string, slugSuffix string, spy *upstreamSpy) (context.Context, resourceDanceFixture) {
	t.Helper()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			spy.handlerErr = err
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		spy.form = r.PostForm
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"exchanged-access","token_type":"Bearer","expires_in":3600,"refresh_token":"exchanged-refresh"}`))
	}))
	t.Cleanup(tokenServer.Close)

	enc := testenv.NewEncryptionClient(t)
	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)
	policy, err := guardian.NewUnsafePolicy(tracerProvider, []string{})
	require.NoError(t, err)
	mgr := remotesessions.NewChallengeManager(
		logger,
		ti.conn,
		enc,
		policy,
		ti.redisCache,
		mustURL(t, "http://localhost"),
	)

	q := repo.New(ti.conn)
	issuer, err := q.CreateRemoteSessionIssuer(ctx, repo.CreateRemoteSessionIssuerParams{
		ProjectID:                         uuid.NullUUID{UUID: *authCtx.ProjectID, Valid: true},
		Slug:                              "auth-res-" + slugSuffix,
		Issuer:                            tokenServer.URL,
		AuthorizationEndpoint:             conv.ToPGText(tokenServer.URL + "/authorize"),
		TokenEndpoint:                     conv.ToPGText(tokenServer.URL + "/token"),
		RegistrationEndpoint:              pgtype.Text{String: "", Valid: false},
		JwksUri:                           pgtype.Text{String: "", Valid: false},
		ScopesSupported:                   []string{"openid"},
		GrantTypesSupported:               []string{"authorization_code", "refresh_token"},
		ResponseTypesSupported:            []string{"code"},
		TokenEndpointAuthMethodsSupported: []string{"client_secret_post"},
		Oidc:                              false,
		Passthrough:                       false,
	})
	require.NoError(t, err)

	userIssuer := createUserSessionIssuer(t, ctx, ti.conn, "usi-res-"+slugSuffix)

	secretCiphertext, err := enc.Encrypt([]byte("res-secret"))
	require.NoError(t, err)
	client, err := q.CreateRemoteSessionClient(ctx, repo.CreateRemoteSessionClientParams{
		ProjectID:               conv.ToNullUUID(*authCtx.ProjectID),
		OrganizationID:          conv.ToPGTextEmpty(authCtx.ActiveOrganizationID),
		RemoteSessionIssuerID:   issuer.ID,
		ClientID:                "res-cid",
		ClientSecretEncrypted:   conv.ToPGText(secretCiphertext),
		ClientIDIssuedAt:        conv.ToPGTimestamptz(time.Now()),
		ClientSecretExpiresAt:   pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false},
		TokenEndpointAuthMethod: conv.ToPGText("client_secret_post"),
		Scope:                   nil,
		Audience:                pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err)

	err = q.AttachRemoteSessionClientToUserSessionIssuer(ctx, repo.AttachRemoteSessionClientToUserSessionIssuerParams{
		RemoteSessionClientID: client.ID,
		UserSessionIssuerID:   userIssuer,
	})
	require.NoError(t, err)

	clients, err := mgr.ListClients(ctx, *authCtx.ProjectID, authCtx.ActiveOrganizationID, userIssuer)
	require.NoError(t, err)
	require.Len(t, clients, 1)

	subject := urn.NewUserSubject("res-subject-" + slugSuffix)
	return ctx, resourceDanceFixture{
		mgr: mgr,
		parent: remotesessions.ParentChallenge{
			ID:                  uuid.NewString(),
			ProjectID:           *authCtx.ProjectID,
			OrganizationID:      authCtx.ActiveOrganizationID,
			UserSessionIssuerID: userIssuer,
			Subject:             &subject,
			McpSlug:             "res-" + slugSuffix,
			RouteBase:           "mcp",
			FinalRedirectURI:    "",
			Resource:            resource,
		},
		clients: clients,
	}
}

// runCallback exchanges a fake code through HandleRemoteLoginCallback using
// the state minted by BuildAuthorizationUrl, so the spy token endpoint sees
// the real exchange form.
func runCallback(t *testing.T, ctx context.Context, fx resourceDanceFixture, authURL string) {
	t.Helper()

	parsed, err := url.Parse(authURL)
	require.NoError(t, err)
	state := parsed.Query().Get("state")
	require.NotEmpty(t, state)

	req := httptest.NewRequest(http.MethodGet,
		"/mcp/remote_login_callback?code=fake-code&state="+url.QueryEscape(state), nil)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	require.NoError(t, fx.mgr.HandleRemoteLoginCallback(w, req))
	require.Equal(t, http.StatusSeeOther, w.Code)
}

func TestRemoteLoginDance_ResourceOnAuthorizeAndExchange(t *testing.T) {
	t.Parallel()

	var spy upstreamSpy
	ctx, fx := setupResourceDanceFixture(t, "https://mcp.example.com/mcp", "set", &spy)

	authURL, err := fx.mgr.BuildAuthorizationUrl(ctx, fx.parent, fx.clients[0])
	require.NoError(t, err)

	parsed, err := url.Parse(authURL)
	require.NoError(t, err)
	require.Equal(t, "https://mcp.example.com/mcp", parsed.Query().Get("resource"),
		"authorize URL must carry the parent challenge's RFC 8707 resource")

	runCallback(t, ctx, fx, authURL)
	require.NoError(t, spy.handlerErr)
	require.Equal(t, "https://mcp.example.com/mcp", spy.form.Get("resource"),
		"code exchange must repeat the resource sent on the authorize leg")
}

func TestRemoteLoginDance_ResourceOmittedWhenUnset(t *testing.T) {
	t.Parallel()

	var spy upstreamSpy
	ctx, fx := setupResourceDanceFixture(t, "", "unset", &spy)

	authURL, err := fx.mgr.BuildAuthorizationUrl(ctx, fx.parent, fx.clients[0])
	require.NoError(t, err)

	parsed, err := url.Parse(authURL)
	require.NoError(t, err)
	require.False(t, parsed.Query().Has("resource"),
		"authorize URL must omit resource when the parent challenge carries none")

	runCallback(t, ctx, fx, authURL)
	require.NoError(t, spy.handlerErr)
	require.False(t, spy.form.Has("resource"),
		"code exchange must omit resource when the parent challenge carries none")
}
