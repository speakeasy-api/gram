package remotesessions_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	orgclientsgen "github.com/speakeasy-api/gram/server/gen/organization_remote_session_clients"
	clientsgen "github.com/speakeasy-api/gram/server/gen/remote_session_clients"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/orghost"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
)

func organizationCallbackOrigins(t *testing.T) remotesessions.CallbackOrigins {
	t.Helper()
	origins := pinnedCallbackOrigins(t, movedOrigin)
	origins.OrganizationHosts = orghost.New(orghost.Config{
		ServerURL:         mustURL(t, movedOrigin),
		LegacyDefaultHost: mustURL(t, pinnedOrigin),
		PlatformHosts:     map[string]string{"app.example.test": pinnedOrigin},
	})
	return origins
}

func TestNewClientsUseOrganizationCallbackOrigin(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		stored string
		want   string
	}{
		{"legacy null", "", pinnedOrigin},
		{"explicit legacy", pinnedOrigin, pinnedOrigin},
		{"canonical", movedOrigin, movedOrigin},
		{"unrecognized", "https://unknown.example.test", pinnedOrigin},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestServiceWithConfig(t, testServiceConfig{maxDBConns: 1})
			ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			err := orgrepo.New(ti.conn).SetOrganizationDefaultHostForTest(ctx, orgrepo.SetOrganizationDefaultHostForTestParams{DefaultHost: pgtype.Text{String: tc.stored, Valid: tc.stored != ""}, ID: activeOrganizationID(t, ctx)})
			require.NoError(t, err)
			ti.service.SetCallbackOrigins(organizationCallbackOrigins(t))
			want := pgtype.Text{String: tc.want, Valid: true}
			preview, err := ti.service.GetNewClientCallbackURL(ctx, &clientsgen.GetNewClientCallbackURLPayload{})
			require.NoError(t, err)
			require.Equal(t, tc.want+"/mcp/remote_login_callback", preview.CallbackURL)

			issuer := createRemoteIssuer(t, ctx, ti, "org-origin-project", "")
			usi := createUserSessionIssuer(t, ctx, ti.conn, "org-origin-usi")
			id := createRemoteClient(t, ctx, ti, issuer, usi.String(), "org-origin-client")
			require.Equal(t, want, storedCallbackBaseURL(t, ti, id))
			orgIssuer := seedOrgLevelRemoteIssuer(t, ctx, ti.conn, activeOrganizationID(t, ctx), "org-origin-issuer")
			client, err := ti.service.CreateClient(ctx, newCreateClientPayload(orgIssuer.String(), nil, nil))
			require.NoError(t, err)
			require.Equal(t, want, storedCallbackBaseURL(t, ti, client.ID))
			cimdIssuer := createCIMDIssuer(t, ctx, ti, "org-origin-cimd", "https://idp.example.test/authorize", "https://idp.example.test/token")
			cimd := createCimdClient(t, ctx, ti, cimdIssuer.String(), usi.String(), nil)
			require.Equal(t, want, storedCallbackBaseURL(t, ti, cimd.ID))
			require.Equal(t, remotesessions.ClientMetadataDocumentURL(mustURL(t, tc.want), uuid.MustParse(cimd.ID)), *cimd.ClientIDMetadataURI)

			orgCimd, err := ti.service.CreateCimdClient(ctx, &orgclientsgen.CreateCimdClientPayload{RemoteSessionIssuerID: cimdIssuer.String()})
			require.NoError(t, err)
			require.Equal(t, want, storedCallbackBaseURL(t, ti, orgCimd.ID))
			require.Equal(t, remotesessions.ClientMetadataDocumentURL(mustURL(t, tc.want), uuid.MustParse(orgCimd.ID)), *orgCimd.ClientIDMetadataURI)

			authCtx, ok := contextvalues.GetAuthContext(ctx)
			require.True(t, ok)
			ti.enableCustomerManagedKeys(t, ctx, authCtx.ActiveOrganizationID)
			keySet := createJsonWebKeySet(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "org-origin-keys")
			attachJsonWebKeySet(t, ctx, ti, cimd.ID, keySet)
			forceTokenEndpointAuthMethod(t, ctx, ti.conn, uuid.MustParse(cimd.ID), *authCtx.ProjectID, string(remotesessions.TokenEndpointAuthMethodPrivateKeyJWT))
			manager := newCIMDChallengeManager(t, ti, movedOrigin)
			document := httptest.NewRecorder()
			require.NoError(t, manager.HandleClientMetadataDocument(document, cimdDocumentRequest(t, cimd.ID, false)))
			var metadata map[string]any
			require.NoError(t, json.Unmarshal(document.Body.Bytes(), &metadata))
			require.Equal(t, remotesessions.ClientJSONWebKeySetURL(mustURL(t, tc.want), uuid.MustParse(cimd.ID)), metadata["jwks_uri"])
			require.Equal(t, []any{preview.CallbackURL}, metadata["redirect_uris"])

			// Updating the organization's default must not move an existing client.
			err = orgrepo.New(ti.conn).SetOrganizationDefaultHostForTest(ctx, orgrepo.SetOrganizationDefaultHostForTestParams{DefaultHost: pgtype.Text{String: movedOrigin, Valid: movedOrigin != ""}, ID: activeOrganizationID(t, ctx)})
			require.NoError(t, err)
			got, err := ti.service.GetRemoteSessionClient(ctx, &clientsgen.GetRemoteSessionClientPayload{ID: id})
			require.NoError(t, err)
			require.Equal(t, preview.CallbackURL, *got.CallbackURL)
		})
	}
}

func TestIdentityCommitOrganizationOrigin(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name        string
		defaultHost string
		wantOrigin  string
	}{
		{name: "canonical", defaultHost: movedOrigin, wantOrigin: movedOrigin},
		{name: "legacy null", defaultHost: "", wantOrigin: pinnedOrigin},
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
			committer := remotesessions.NewIdentityCommitter(testenv.NewLogger(t), ti.conn, testenv.NewEncryptionClient(t), audit.NewLogger(), mustURL(t, movedOrigin), policy, nil, nil)
			err = orgrepo.New(ti.conn).SetOrganizationDefaultHostForTest(ctx, orgrepo.SetOrganizationDefaultHostForTestParams{DefaultHost: pgtype.Text{String: tc.defaultHost, Valid: tc.defaultHost != ""}, ID: activeOrganizationID(t, ctx)})
			require.NoError(t, err)
			committer.SetCallbackOrigins(organizationCallbackOrigins(t))

			userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "dcr-origin-usi")
			providerID := createServerIdentityProvider(t, ctx, ti, "dcr-origin-provider", registrationServer.URL, false, []string{"client_secret_basic"})
			plan := linkPlan(t, ctx, userIssuerID, providerID, uuid.Nil)
			plan.Client = remotesessions.RegisterClient(remotesessions.RegistrationPolicy{Scope: nil, RegistrationResource: nil, Audience: nil, TokenEndpointAuthMethod: nil, RequireClientSecret: true, AllowCIMD: false})
			commit := committer.Prepare(plan)
			require.NoError(t, commit.Preflight(ctx))
			reg, err := commit.Register(ctx)
			require.NoError(t, err)
			require.Equal(t, remotesessions.RegistrationDCR, reg.Method)
			// The outbound origin is pinned even if the org changes before persistence.
			err = orgrepo.New(ti.conn).SetOrganizationDefaultHostForTest(ctx, orgrepo.SetOrganizationDefaultHostForTestParams{DefaultHost: pgtype.Text{String: "https://unknown.example.test", Valid: "https://unknown.example.test" != ""}, ID: activeOrganizationID(t, ctx)})
			require.NoError(t, err)
			tx, err := commit.Begin(ctx)
			require.NoError(t, err)
			require.NoError(t, commit.Lock(ctx, tx))
			require.NoError(t, commit.Bind(ctx, tx, reg))
			res, err := commit.Commit(ctx, tx)
			require.NoError(t, err)

			body := registered.Load()
			require.NotNil(t, body)
			require.Equal(t, []string{tc.wantOrigin + "/mcp/remote_login_callback"}, body.RedirectURIs, "the registration and the stored row agree on the origin")
			require.Equal(t, pgtype.Text{String: tc.wantOrigin, Valid: true}, res.Client.CallbackBaseUrl)
		})
	}
}

func TestPreparationDCRUsesOrganizationCallbackOrigin(t *testing.T) {
	t.Parallel()
	var registered atomic.Pointer[remotesessions.DCRRequest]
	registration := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body remotesessions.DCRRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		registered.Store(&body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"client_id":"org-origin-prepared","client_secret":"test-secret","token_endpoint_auth_method":"client_secret_basic","grant_types":["urn:ietf:params:oauth:grant-type:jwt-bearer"]}`))
	}))
	t.Cleanup(registration.Close)
	ctx, ti, in, _ := preparationDCRFixture(t, registration.URL)
	err := orgrepo.New(ti.conn).SetOrganizationDefaultHostForTest(ctx, orgrepo.SetOrganizationDefaultHostForTestParams{DefaultHost: pgtype.Text{}, ID: activeOrganizationID(t, ctx)})
	require.NoError(t, err)
	ti.service.SetCallbackOrigins(organizationCallbackOrigins(t))
	result, err := ti.service.PrepareIdentityChaining(ctx, in)
	require.NoError(t, err)
	require.NotNil(t, registered.Load())
	require.Equal(t, []string{pinnedOrigin + "/mcp/remote_login_callback"}, registered.Load().RedirectURIs)
	require.Equal(t, pgtype.Text{String: pinnedOrigin, Valid: true}, storedCallbackBaseURL(t, ti, result.ClientID.String()))
}

func TestProxyRegisterUsesOrganizationCallbackOrigin(t *testing.T) {
	t.Parallel()
	var registered atomic.Pointer[remotesessions.DCRRequest]
	registration := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body remotesessions.DCRRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		registered.Store(&body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"client_id":"org-origin-proxy"}`))
	}))
	t.Cleanup(registration.Close)
	ctx, ti := newTestService(t)
	err := orgrepo.New(ti.conn).SetOrganizationDefaultHostForTest(ctx, orgrepo.SetOrganizationDefaultHostForTestParams{DefaultHost: pgtype.Text{}, ID: activeOrganizationID(t, ctx)})
	require.NoError(t, err)
	ti.service.SetCallbackOrigins(organizationCallbackOrigins(t))
	response := serveProxyRegister(t, ctx, ti, remotesessions.ProxyRegisterRequest{RegistrationEndpoint: registration.URL})
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	require.NotNil(t, registered.Load())
	require.Equal(t, []string{pinnedOrigin + "/mcp/remote_login_callback"}, registered.Load().RedirectURIs)
}

func TestIdentityCommitResolvesOriginBeforeBegin(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"manual", "cimd"} {
		t.Run(mode, func(t *testing.T) {
			cimd := mode == "cimd"
			t.Parallel()
			ctx, ti := newTestServiceWithConfig(t, testServiceConfig{maxDBConns: 1})
			ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			committer := remotesessions.NewIdentityCommitter(testenv.NewLogger(t), ti.conn, testenv.NewEncryptionClient(t), audit.NewLogger(), mustURL(t, movedOrigin), nil, nil, nil)
			committer.SetCallbackOrigins(organizationCallbackOrigins(t))
			usi := createUserSessionIssuer(t, ctx, ti.conn, "origin-before-begin")
			provider := createServerIdentityProvider(t, ctx, ti, "origin-provider", "", cimd, []string{"none", "client_secret_basic"})
			plan := linkPlan(t, ctx, usi, provider, uuid.Nil)
			plan.Client = remotesessions.ManualClient(remotesessions.ClientCredentials{ClientID: "manual-origin", ClientSecret: "test-secret"})
			if cimd {
				plan.Client = remotesessions.RegisterClient(remotesessions.RegistrationPolicy{AllowCIMD: true})
			}
			commit := committer.Prepare(plan)
			require.NoError(t, commit.Preflight(ctx))
			reg, err := commit.Register(ctx)
			require.NoError(t, err)
			require.True(t, reg.Ready())
			tx, err := commit.Begin(ctx)
			require.NoError(t, err)
			defer o11y.NoLogDefer(func() error { return tx.Rollback(context.Background()) })
			require.NoError(t, commit.Lock(ctx, tx))
			require.NoError(t, commit.Bind(ctx, tx, reg))
			result, err := commit.Commit(ctx, tx)
			require.NoError(t, err)
			require.True(t, result.Client.CallbackBaseUrl.Valid)
		})
	}
}
