package remotesessions_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	adminrsgen "github.com/speakeasy-api/gram/server/gen/admin_remote_sessions"
	clientsgen "github.com/speakeasy-api/gram/server/gen/remote_session_clients"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/oauth/registration"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

const (
	// pinnedOrigin is the host existing registrations were made against.
	pinnedOrigin = "https://app.example.test"
	// movedOrigin is the host the server URL and new registrations move to.
	movedOrigin = "https://ai.example.test"
)

func pinnedCallbackOrigins(t *testing.T, registration string) remotesessions.CallbackOrigins {
	t.Helper()
	origins := remotesessions.CallbackOrigins{Outbound: mustURL(t, pinnedOrigin), Registration: nil}
	if registration != "" {
		origins.Registration = mustURL(t, registration)
	}
	return origins
}

func storedCallbackBaseURL(t *testing.T, ti *testInstance, clientID string) pgtype.Text {
	t.Helper()
	row, err := repo.New(ti.conn).GetRemoteSessionClientForRotation(t.Context(), uuid.MustParse(clientID))
	require.NoError(t, err)
	return row.RemoteSessionClient.CallbackBaseUrl
}

func TestCallbackOrigins_ResolveClientAndNewClientOrigins(t *testing.T) {
	t.Parallel()

	origins := pinnedCallbackOrigins(t, movedOrigin+"/")

	require.Equal(t, pinnedOrigin+"/mcp/remote_login_callback", origins.ClientCallbackURL(pgtype.Text{String: "", Valid: false}), "NULL resolves to the pinned origin")
	require.Equal(t, pinnedOrigin+"/mcp/remote_login_callback", remotesessions.RemoteLoginCallbackURL(mustURL(t, pinnedOrigin+"/")), "a trailing slash does not change the redirect_uri")
	require.Equal(t, movedOrigin+"/mcp/remote_login_callback", origins.ClientCallbackURL(pgtype.Text{String: movedOrigin, Valid: true}))
	require.Equal(t, movedOrigin+"/oauth/callback", remotesessions.LegacyProxyCallbackURL(origins.ForClient(pgtype.Text{String: movedOrigin, Valid: true})))

	require.Equal(t, pgtype.Text{String: movedOrigin, Valid: true}, origins.NewClientBaseURL(true), "organization-owned clients record the registration origin")
	require.False(t, origins.NewClientBaseURL(false).Valid, "global clients never record one")
	require.Equal(t, movedOrigin, origins.ForNewClient(true).String())
	require.Equal(t, pinnedOrigin, origins.ForNewClient(false).String())

	unset := pinnedCallbackOrigins(t, "")
	require.False(t, unset.NewClientBaseURL(true).Valid, "no registration origin records none")
	require.Equal(t, pinnedOrigin, unset.ForNewClient(true).String())
}

func TestRemoteLogin_NullClientKeepsPinnedCallbackAfterServerURLFlip(t *testing.T) {
	t.Parallel()

	origins := pinnedCallbackOrigins(t, movedOrigin)
	spy := &exchangeSpy{}
	_, env, w, err := driveSyntheticLogin(t, "pinned-null-client", spy.handler(), func(o *syntheticLoginOptions) {
		o.serverURL = movedOrigin
		o.callbackOrigins = &origins
	})
	require.NoError(t, err)
	require.Equal(t, http.StatusSeeOther, w.Code)

	want := pinnedOrigin + "/mcp/remote_login_callback"
	require.Equal(t, want, authorizeQuery(t, env.authURL).Get("redirect_uri"))
	form, _ := spy.only(t)
	require.Equal(t, want, form.Get("redirect_uri"), "the token exchange repeats the pinned redirect_uri")
	require.Contains(t, w.Header().Get("Location"), movedOrigin+"/mcp/", "the post-login bounce still follows the server URL")
}

func TestRemoteLogin_LegacyNullClientKeepsPinnedLegacyCallback(t *testing.T) {
	t.Parallel()

	origins := pinnedCallbackOrigins(t, movedOrigin)
	spy := &exchangeSpy{}
	_, env, _, err := driveSyntheticLogin(t, "pinned-legacy-client", spy.handler(), func(o *syntheticLoginOptions) {
		o.serverURL = movedOrigin
		o.callbackOrigins = &origins
		o.legacyCallbackURL = true
	})
	require.NoError(t, err)
	require.Equal(t, pinnedOrigin+"/oauth/callback", authorizeQuery(t, env.authURL).Get("redirect_uri"))
}

func TestRemoteLogin_ClientCallbackBaseURLDrivesRedirectURI(t *testing.T) {
	t.Parallel()

	origins := pinnedCallbackOrigins(t, "")
	spy := &exchangeSpy{}
	_, env, w, err := driveSyntheticLogin(t, "recorded-origin-client", spy.handler(), func(o *syntheticLoginOptions) {
		o.callbackOrigins = &origins
		o.clientCallbackBaseURL = movedOrigin
	})
	require.NoError(t, err)
	require.Equal(t, http.StatusSeeOther, w.Code)

	want := movedOrigin + "/mcp/remote_login_callback"
	require.Equal(t, want, authorizeQuery(t, env.authURL).Get("redirect_uri"))
	form, _ := spy.only(t)
	require.Equal(t, want, form.Get("redirect_uri"))
}

func TestBuildAuthorizationUrl_RotationKeepsRecordedCallbackOrigin(t *testing.T) {
	t.Parallel()

	origins := pinnedCallbackOrigins(t, "https://registration.example.test")
	upstream := &rotationUpstream{refreshStatus: http.StatusUnauthorized, refreshBody: invalidClientBody}
	ctx, env := newSyntheticExpiryEnv(t, "rotation-origin", upstream.handler(), func(o *syntheticLoginOptions) {
		o.callbackOrigins = &origins
		o.clientCallbackBaseURL = movedOrigin
	})
	rejectedAt := time.Now().Add(-time.Hour)
	stageRegistration(t, env, issuerTokenEndpoint(t, env)+"/register", &rejectedAt, nil)

	authURL, err := env.mgr.BuildAuthorizationUrl(ctx, remotesessions.ParentChallenge{
		ID:                  uuid.NewString(),
		ProjectID:           env.projectID,
		OrganizationID:      env.organizationID,
		UserSessionIssuerID: env.session.UserSessionIssuerID,
		Subject:             &env.subject,
		McpSlug:             "rotation-origin-mcp",
	}, listClient(t, env))
	require.NoError(t, err)

	want := movedOrigin + "/mcp/remote_login_callback"
	query := authorizeQuery(t, authURL)
	require.Equal(t, "rotated-cid", query.Get("client_id"))
	require.Equal(t, want, query.Get("redirect_uri"))
	registration := *upstream.lastRegistration.Load()
	require.Equal(t, []any{want}, registration["redirect_uris"], "a rotation re-registers the client's recorded origin")
	require.Equal(t, pgtype.Text{String: movedOrigin, Valid: true}, loadClient(t, env).CallbackBaseUrl, "a rotation never moves the recorded origin")
}

func TestHandleClientMetadataDocument_PinsNullClientURLsToOutboundOrigin(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	ti.enableCustomerManagedKeys(t, ctx, authCtx.ActiveOrganizationID)

	issuerID := createCIMDIssuer(t, ctx, ti, "cimd-pinned", "https://idp.example.com/authorize", "https://idp.example.com/token")
	userIssuer := createUserSessionIssuer(t, ctx, ti.conn, "cimd-pinned-usi")
	created := createCimdClient(t, ctx, ti, issuerID.String(), userIssuer.String(), nil)
	clientID := uuid.MustParse(created.ID)
	require.False(t, storedCallbackBaseURL(t, ti, created.ID).Valid)
	setID := createJsonWebKeySet(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "cimd-pinned-set")
	attachJsonWebKeySet(t, ctx, ti, created.ID, setID)

	// The server URL has moved; the client's identity has not.
	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), []string{})
	require.NoError(t, err)
	mgr := remotesessions.NewChallengeManager(testenv.NewLogger(t), testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), ti.conn, testenv.NewEncryptionClient(t), policy, ti.tunnels, ti.redisCache, mustURL(t, movedOrigin),
		remotesessions.WithCallbackOrigins(remotesessions.CallbackOrigins{Outbound: mustURL(t, cimdServerURL), Registration: nil}))
	rec := httptest.NewRecorder()
	require.NoError(t, mgr.HandleClientMetadataDocument(rec, cimdDocumentRequest(t, created.ID, false)))

	var got map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.Equal(t, remotesessions.ClientMetadataDocumentURL(mustURL(t, cimdServerURL), clientID), got["client_id"])
	require.Equal(t, []any{cimdServerURL + "/mcp/remote_login_callback"}, got["redirect_uris"])
	require.Equal(t, remotesessions.ClientJSONWebKeySetURL(mustURL(t, cimdServerURL), clientID), got["jwks_uri"])
}

func TestCreateClients_RecordRegistrationOriginOnOrganizationOwnedClients(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	ti.service.SetCallbackOrigins(pinnedCallbackOrigins(t, movedOrigin))
	moved := pgtype.Text{String: movedOrigin, Valid: true}
	wantCallback := movedOrigin + "/mcp/remote_login_callback"

	// A project client set up through the management API.
	issuerID := createRemoteIssuer(t, ctx, ti, "origin-project-issuer", "")
	userIssuer := createUserSessionIssuer(t, ctx, ti.conn, "origin-project-usi")
	projectClientID := createRemoteClient(t, ctx, ti, issuerID, userIssuer.String(), "origin-project-client")
	require.Equal(t, moved, storedCallbackBaseURL(t, ti, projectClientID))
	got, err := ti.service.GetRemoteSessionClient(ctx, &clientsgen.GetRemoteSessionClientPayload{ID: projectClientID})
	require.NoError(t, err)
	require.Equal(t, wantCallback, *got.CallbackURL)

	// An organization-level client created by an organization administrator.
	orgIssuerID := seedOrgLevelRemoteIssuer(t, ctx, ti.conn, activeOrganizationID(t, ctx), "origin-org-issuer")
	orgClient, err := ti.service.CreateClient(ctx, newCreateClientPayload(orgIssuerID.String(), nil, nil))
	require.NoError(t, err)
	require.Equal(t, moved, storedCallbackBaseURL(t, ti, orgClient.ID))
	require.Equal(t, wantCallback, *orgClient.CallbackURL)

	// A CIMD client publishes its whole identity on the recorded origin.
	cimdIssuerID := createCIMDIssuer(t, ctx, ti, "origin-cimd-issuer", "https://idp.example.com/authorize", "https://idp.example.com/token")
	cimdClient := createCimdClient(t, ctx, ti, cimdIssuerID.String(), createUserSessionIssuer(t, ctx, ti.conn, "origin-cimd-usi").String(), nil)
	require.Equal(t, moved, storedCallbackBaseURL(t, ti, cimdClient.ID))
	require.Equal(t, remotesessions.ClientMetadataDocumentURL(mustURL(t, movedOrigin), uuid.MustParse(cimdClient.ID)), *cimdClient.ClientIDMetadataURI)

	// A shared global client stays on the pinned origin.
	adminCtx := withAdmin(t, ctx)
	globalIssuer, err := ti.service.CreateGlobalIssuer(adminCtx, createGlobalIssuer(t, "origin-global-issuer"))
	require.NoError(t, err)
	globalClient, err := ti.service.CreateGlobalClient(adminCtx, &adminrsgen.CreateGlobalClientPayload{
		SessionToken:            nil,
		RemoteSessionIssuerID:   globalIssuer.ID,
		ClientID:                "origin-global-client",
		ClientSecret:            nil,
		TokenEndpointAuthMethod: nil,
		Scope:                   nil,
		Audience:                nil,
	})
	require.NoError(t, err)
	require.False(t, storedCallbackBaseURL(t, ti, globalClient.ID).Valid)

	newClient, err := ti.service.GetNewClientCallbackURL(ctx, &clientsgen.GetNewClientCallbackURLPayload{})
	require.NoError(t, err)
	require.Equal(t, wantCallback, newClient.CallbackURL)
}

func TestCreateClients_RecordNoOriginWithoutRegistrationOrigin(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	ti.service.SetCallbackOrigins(pinnedCallbackOrigins(t, ""))
	wantCallback := pinnedOrigin + "/mcp/remote_login_callback"

	issuerID := createRemoteIssuer(t, ctx, ti, "no-origin-project-issuer", "")
	userIssuer := createUserSessionIssuer(t, ctx, ti.conn, "no-origin-project-usi")
	projectClientID := createRemoteClient(t, ctx, ti, issuerID, userIssuer.String(), "no-origin-project-client")
	require.False(t, storedCallbackBaseURL(t, ti, projectClientID).Valid)

	orgIssuerID := seedOrgLevelRemoteIssuer(t, ctx, ti.conn, activeOrganizationID(t, ctx), "no-origin-org-issuer")
	orgClient, err := ti.service.CreateClient(ctx, newCreateClientPayload(orgIssuerID.String(), nil, nil))
	require.NoError(t, err)
	require.False(t, storedCallbackBaseURL(t, ti, orgClient.ID).Valid)
	require.Equal(t, wantCallback, *orgClient.CallbackURL, "a NULL client reports the pinned callback")

	newClient, err := ti.service.GetNewClientCallbackURL(ctx, &clientsgen.GetNewClientCallbackURLPayload{})
	require.NoError(t, err)
	require.Equal(t, wantCallback, newClient.CallbackURL)
}

func TestIdentityCommit_DynamicRegistrationRecordsRegistrationOrigin(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name         string
		registration string
		wantOrigin   string
		wantStored   pgtype.Text
	}{
		{name: "registration origin set", registration: movedOrigin, wantOrigin: movedOrigin, wantStored: pgtype.Text{String: movedOrigin, Valid: true}},
		{name: "registration origin unset", registration: "", wantOrigin: pinnedOrigin, wantStored: pgtype.Text{String: "", Valid: false}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx, ti := newTestService(t)
			var registered atomic.Pointer[remotesessions.DCRRequest]
			registrationServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body remotesessions.DCRRequest
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				registered.Store(&body)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusCreated)
				_, _ = w.Write([]byte(`{"client_id":"dcr-origin-cid","client_secret":"dcr-origin-secret","token_endpoint_auth_method":"client_secret_basic"}`))
			}))
			t.Cleanup(registrationServer.Close)

			policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), []string{})
			require.NoError(t, err)
			committer := remotesessions.NewIdentityCommitter(testenv.NewLogger(t), ti.conn, testenv.NewEncryptionClient(t), audit.NewLogger(), mustURL(t, movedOrigin), policy, ti.tunnels, registration.NewMetrics(testenv.NewLogger(t), testenv.NewMeterProvider(t)))
			committer.SetCallbackOrigins(pinnedCallbackOrigins(t, tc.registration))

			userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "dcr-origin-usi")
			providerID := createServerIdentityProvider(t, ctx, ti, "dcr-origin-provider", registrationServer.URL, false, []string{"client_secret_basic"})
			plan := linkPlan(t, ctx, userIssuerID, providerID, uuid.Nil)
			plan.Client = remotesessions.RegisterClient(remotesessions.RegistrationPolicy{Scope: nil, Audience: nil, TokenEndpointAuthMethod: nil, RequireClientSecret: true, AllowCIMD: false})
			commit := committer.Prepare(plan)
			require.NoError(t, commit.Preflight(ctx))
			reg, err := commit.Register(ctx)
			require.NoError(t, err)
			require.Equal(t, remotesessions.RegistrationDCR, reg.Method)
			tx, err := commit.Begin(ctx)
			require.NoError(t, err)
			require.NoError(t, commit.Lock(ctx, tx))
			require.NoError(t, commit.Bind(ctx, tx, reg))
			res, err := commit.Commit(ctx, tx)
			require.NoError(t, err)

			body := registered.Load()
			require.NotNil(t, body)
			require.Equal(t, []string{tc.wantOrigin + "/mcp/remote_login_callback"}, body.RedirectURIs, "the registration and the stored row agree on the origin")
			require.Equal(t, tc.wantStored, res.Client.CallbackBaseUrl)
		})
	}
}
