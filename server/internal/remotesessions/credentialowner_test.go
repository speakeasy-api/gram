package remotesessions_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	orgclientsgen "github.com/speakeasy-api/gram/server/gen/organization_remote_session_clients"
	orgissuersgen "github.com/speakeasy-api/gram/server/gen/organization_remote_session_issuers"
	clientsgen "github.com/speakeasy-api/gram/server/gen/remote_session_clients"
	issuersgen "github.com/speakeasy-api/gram/server/gen/remote_session_issuers"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	jsonwebkeysetsrepo "github.com/speakeasy-api/gram/server/internal/jsonwebkeysets/repo"
	"github.com/speakeasy-api/gram/server/internal/oauthwire"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/clientcredentials"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// createTokenEndpointOnlyIssuer creates an issuer the way an operator enters
// one by hand for an identity provider that publishes no authorization server
// metadata: an issuer identifier and a token endpoint, nothing discovered.
func createTokenEndpointOnlyIssuer(t *testing.T, ctx context.Context, ti *testInstance, slug, issuer, tokenEndpoint string) string {
	t.Helper()

	created, err := ti.service.CreateRemoteSessionIssuer(ctx, &issuersgen.CreateRemoteSessionIssuerPayload{
		Slug:          slug,
		Issuer:        issuer,
		TokenEndpoint: &tokenEndpoint,
	})
	require.NoError(t, err)

	return created.ID
}

// newSelfCreateClientPayload is an organization create of a self client.
func newSelfCreateClientPayload(issuerID string, method, secret, keySetID *string) *orgclientsgen.CreateClientPayload {
	return &orgclientsgen.CreateClientPayload{
		SessionToken:            nil,
		ApikeyToken:             nil,
		RemoteSessionIssuerID:   issuerID,
		ProjectID:               nil,
		ClientID:                "self-client-" + uuid.NewString(),
		ClientSecret:            secret,
		TokenEndpointAuthMethod: method,
		JSONWebKeySetID:         keySetID,
		Scope:                   []string{"devices.read"},
		Audience:                nil,
		CredentialOwner:         string(remotesessions.CredentialOwnerSelf),
	}
}

func TestCreateClient_SelfClientForTokenEndpointOnlyIssuer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	issuerID := createTokenEndpointOnlyIssuer(t, ctx, ti, "self-tenant-issuer", "https://tenant.example.com", "https://tenant.example.com/api/oauth/token")

	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionClientCreate)
	require.NoError(t, err)

	created, err := ti.service.CreateClient(ctx, newSelfCreateClientPayload(issuerID, new(oauthwire.AuthMethodClientSecretPost), new("tenant-secret"), nil))
	require.NoError(t, err)
	require.Equal(t, string(remotesessions.CredentialOwnerSelf), created.CredentialOwner)
	require.Equal(t, oauthwire.AuthMethodClientSecretPost, *created.TokenEndpointAuthMethod)
	require.Equal(t, []string{oauthwire.GrantTypeClientCredentials}, created.GrantTypes)

	read, err := ti.service.GetClient(ctx, &orgclientsgen.GetClientPayload{ID: created.ID, SessionToken: nil, ApikeyToken: nil})
	require.NoError(t, err)
	require.Equal(t, string(remotesessions.CredentialOwnerSelf), read.CredentialOwner)

	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionClientCreate)
	require.NoError(t, err)
	require.Equal(t, before+1, after)

	entry, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionRemoteSessionClientCreate)
	require.NoError(t, err)
	snapshot, err := audittest.DecodeAuditData(entry.AfterSnapshot)
	require.NoError(t, err)
	require.Equal(t, string(remotesessions.CredentialOwnerSelf), snapshot["CredentialOwner"])
	require.Equal(t, oauthwire.AuthMethodClientSecretPost, snapshot["TokenEndpointAuthMethod"])
}

func TestCreateClient_SelfClientDefaultsToClientSecretBasic(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	issuerID := createTokenEndpointOnlyIssuer(t, ctx, ti, "self-default-issuer", "https://tenant.example.com", "https://tenant.example.com/api/oauth/token")

	created, err := ti.service.CreateClient(ctx, newSelfCreateClientPayload(issuerID, nil, new("tenant-secret"), nil))
	require.NoError(t, err)
	require.NotNil(t, created.TokenEndpointAuthMethod, "a self client stores its method explicitly")
	require.Equal(t, oauthwire.AuthMethodClientSecretBasic, *created.TokenEndpointAuthMethod)
}

func TestCreateClient_SubjectClientByDefault(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	issuerID := createRemoteIssuer(t, ctx, ti, "subject-default-issuer", "")

	created, err := ti.service.CreateClient(ctx, newCreateClientPayload(issuerID, nil, new("secret")))
	require.NoError(t, err)
	require.Equal(t, string(remotesessions.CredentialOwnerSubject), created.CredentialOwner)
	require.Nil(t, created.TokenEndpointAuthMethod, "a subject client keeps an omitted method unset")
	require.Nil(t, created.GrantTypes)
}

func TestCreateRemoteSessionClient_SelfProjectClient(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	issuerID := createTokenEndpointOnlyIssuer(t, ctx, ti, "self-project-issuer", "https://tenant.example.com", "https://tenant.example.com/api/oauth/token")
	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "self-project-usi")

	created, err := ti.service.CreateRemoteSessionClient(ctx, &clientsgen.CreateRemoteSessionClientPayload{
		SessionToken:            nil,
		ApikeyToken:             nil,
		ProjectSlugInput:        nil,
		RemoteSessionIssuerID:   issuerID,
		UserSessionIssuerIds:    []string{userIssuerID.String()},
		ClientID:                "self-project-client",
		ClientSecret:            new("tenant-secret"),
		TokenEndpointAuthMethod: new(oauthwire.AuthMethodClientSecretBasic),
		Scope:                   nil,
		Audience:                nil,
		CredentialOwner:         string(remotesessions.CredentialOwnerSelf),
	})
	require.NoError(t, err)
	require.Equal(t, string(remotesessions.CredentialOwnerSelf), created.CredentialOwner)
	require.NotEmpty(t, created.ProjectID)
	require.Equal(t, []string{userIssuerID.String()}, created.UserSessionIssuerIds)
}

func TestCreateClient_SelfClientRefusesInvalidCombinations(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		owner    string
		method   *string
		secret   *string
		keySetID *string
	}{
		{name: "no authentication", owner: "self", method: new(oauthwire.AuthMethodNone), secret: nil, keySetID: nil},
		{name: "client secret method without a secret", owner: "self", method: new(oauthwire.AuthMethodClientSecretBasic), secret: nil, keySetID: nil},
		{name: "default method without a secret", owner: "self", method: nil, secret: nil, keySetID: nil},
		{name: "private key jwt without a key set", owner: "self", method: new(oauthwire.AuthMethodPrivateKeyJWT), secret: nil, keySetID: nil},
		{name: "unknown owner", owner: "someone", method: new(oauthwire.AuthMethodClientSecretBasic), secret: new("tenant-secret"), keySetID: nil},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx, ti := newTestService(t)

			issuerID := createTokenEndpointOnlyIssuer(t, ctx, ti, "self-invalid-issuer", "https://tenant.example.com", "https://tenant.example.com/api/oauth/token")

			payload := newSelfCreateClientPayload(issuerID, tc.method, tc.secret, tc.keySetID)
			payload.CredentialOwner = tc.owner

			_, err := ti.service.CreateClient(ctx, payload)
			requireOopsCode(t, err, oops.CodeBadRequest)
		})
	}
}

func TestCreateClient_SelfClientRequiresIssuerTokenEndpoint(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	created, err := ti.service.CreateRemoteSessionIssuer(ctx, &issuersgen.CreateRemoteSessionIssuerPayload{
		Slug:   "self-no-token-endpoint",
		Issuer: "https://tenant.example.com",
	})
	require.NoError(t, err)

	_, err = ti.service.CreateClient(ctx, newSelfCreateClientPayload(created.ID, nil, new("tenant-secret"), nil))
	requireOopsCode(t, err, oops.CodeBadRequest)

	// A subject client is not held to it: it can still be configured before
	// the issuer's endpoints are filled in.
	_, err = ti.service.CreateClient(ctx, newCreateClientPayload(created.ID, nil, new("secret")))
	require.NoError(t, err)
}

func TestCreateClient_PrivateKeyJWTWithKeySet(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	organizationID := activeOrganizationID(t, ctx)
	ti.enableCustomerManagedKeys(t, ctx, organizationID)
	keySetID := createJsonWebKeySet(t, ctx, ti.conn, organizationID, "self-create-set")

	issuerID := createTokenEndpointOnlyIssuer(t, ctx, ti, "self-pkjwt-issuer", "https://tenant.example.com", "https://tenant.example.com/api/oauth/token")

	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionClientCreate)
	require.NoError(t, err)

	created, err := ti.service.CreateClient(ctx, newSelfCreateClientPayload(issuerID, new(oauthwire.AuthMethodPrivateKeyJWT), nil, new(keySetID.String())))
	require.NoError(t, err)
	require.Equal(t, string(remotesessions.CredentialOwnerSelf), created.CredentialOwner)
	require.Equal(t, oauthwire.AuthMethodPrivateKeyJWT, *created.TokenEndpointAuthMethod)
	require.NotNil(t, created.JSONWebKeySetID)
	require.Equal(t, keySetID.String(), *created.JSONWebKeySetID)

	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionClientCreate)
	require.NoError(t, err)
	require.Equal(t, before+1, after)

	entry, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionRemoteSessionClientCreate)
	require.NoError(t, err)
	snapshot, err := audittest.DecodeAuditData(entry.AfterSnapshot)
	require.NoError(t, err)
	require.Equal(t, keySetID.String(), snapshot["JSONWebKeySetID"])
}

func TestCreateRemoteSessionClient_PrivateKeyJWTWithKeySet(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	organizationID := activeOrganizationID(t, ctx)
	ti.enableCustomerManagedKeys(t, ctx, organizationID)
	keySetID := createJsonWebKeySet(t, ctx, ti.conn, organizationID, "subject-create-set")

	issuerID := createRemoteIssuer(t, ctx, ti, "subject-pkjwt-issuer", "")

	created, err := ti.service.CreateRemoteSessionClient(ctx, &clientsgen.CreateRemoteSessionClientPayload{
		SessionToken:            nil,
		ApikeyToken:             nil,
		ProjectSlugInput:        nil,
		RemoteSessionIssuerID:   issuerID,
		UserSessionIssuerIds:    nil,
		ClientID:                "subject-pkjwt-client",
		ClientSecret:            nil,
		TokenEndpointAuthMethod: new(oauthwire.AuthMethodPrivateKeyJWT),
		JSONWebKeySetID:         new(keySetID.String()),
		Scope:                   nil,
		Audience:                nil,
		CredentialOwner:         string(remotesessions.CredentialOwnerSubject),
	})
	require.NoError(t, err)
	require.Equal(t, string(remotesessions.CredentialOwnerSubject), created.CredentialOwner)
	require.Equal(t, keySetID.String(), *created.JSONWebKeySetID)
}

func TestCreateClient_KeySetRequiresEntitlement(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	// A fresh organization, because the default test organization holds the
	// entitlement.
	organizationID := createOrganization(t, ctx, ti.conn, "self-unentitled-org")
	ctx = withOrganization(t, ctx, ti.conn, organizationID)
	keySetID := createJsonWebKeySet(t, ctx, ti.conn, organizationID, "self-unentitled-set")
	issuerID := seedOrgLevelRemoteIssuer(t, ctx, ti.conn, organizationID, "self-unentitled-issuer")

	_, err := ti.service.CreateClient(ctx, newSelfCreateClientPayload(issuerID.String(), new(oauthwire.AuthMethodPrivateKeyJWT), nil, new(keySetID.String())))
	requireOopsCode(t, err, oops.CodeForbidden)

	ti.enableCustomerManagedKeys(t, ctx, organizationID)

	_, err = ti.service.CreateClient(ctx, newSelfCreateClientPayload(issuerID.String(), new(oauthwire.AuthMethodPrivateKeyJWT), nil, new(keySetID.String())))
	require.NoError(t, err)
}

func TestCreateClient_KeySetFromAnotherOrganizationNotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	organizationID := activeOrganizationID(t, ctx)
	ti.enableCustomerManagedKeys(t, ctx, organizationID)
	otherOrganizationID := createOrganization(t, ctx, ti.conn, "self-other-org")
	keySetID := createJsonWebKeySet(t, ctx, ti.conn, otherOrganizationID, "self-foreign-set")

	issuerID := createTokenEndpointOnlyIssuer(t, ctx, ti, "self-foreign-issuer", "https://tenant.example.com", "https://tenant.example.com/api/oauth/token")

	_, err := ti.service.CreateClient(ctx, newSelfCreateClientPayload(issuerID, new(oauthwire.AuthMethodPrivateKeyJWT), nil, new(keySetID.String())))
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestUpdateClient_SelfClientMustKeepAWorkingCredential(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	issuerID := createTokenEndpointOnlyIssuer(t, ctx, ti, "self-update-issuer", "https://tenant.example.com", "https://tenant.example.com/api/oauth/token")

	created, err := ti.service.CreateClient(ctx, newSelfCreateClientPayload(issuerID, new(oauthwire.AuthMethodClientSecretBasic), new("tenant-secret"), nil))
	require.NoError(t, err)

	update := func(method *string, secret *string, legacyCallbackURL *bool) *orgclientsgen.UpdateClientPayload {
		return &orgclientsgen.UpdateClientPayload{
			SessionToken:                    nil,
			ApikeyToken:                     nil,
			ID:                              created.ID,
			ClientSecret:                    secret,
			TokenEndpointAuthMethod:         method,
			TokenEndpointAuthAudienceFormat: nil,
			Scope:                           nil,
			Audience:                        nil,
			LegacyCallbackURL:               legacyCallbackURL,
		}
	}

	_, err = ti.service.UpdateClient(ctx, update(new(oauthwire.AuthMethodNone), nil, nil))
	requireOopsCode(t, err, oops.CodeBadRequest)

	_, err = ti.service.UpdateClient(withAdmin(t, ctx), update(nil, nil, new(true)))
	requireOopsCode(t, err, oops.CodeBadRequest)

	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionClientUpdate)
	require.NoError(t, err)

	updated, err := ti.service.UpdateClient(ctx, update(new(oauthwire.AuthMethodClientSecretPost), new("rotated-secret"), nil))
	require.NoError(t, err)
	require.Equal(t, string(remotesessions.CredentialOwnerSelf), updated.CredentialOwner)
	require.Equal(t, oauthwire.AuthMethodClientSecretPost, *updated.TokenEndpointAuthMethod)

	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionClientUpdate)
	require.NoError(t, err)
	require.Equal(t, before+1, after)
}

func TestUpdateClient_SelfClientLeavingPrivateKeyJWTRequiresSecret(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	organizationID := activeOrganizationID(t, ctx)
	ti.enableCustomerManagedKeys(t, ctx, organizationID)
	keySetID := createJsonWebKeySet(t, ctx, ti.conn, organizationID, "self-switch-set")

	issuerID := createTokenEndpointOnlyIssuer(t, ctx, ti, "self-switch-issuer", "https://tenant.example.com", "https://tenant.example.com/api/oauth/token")

	created, err := ti.service.CreateClient(ctx, newSelfCreateClientPayload(issuerID, new(oauthwire.AuthMethodPrivateKeyJWT), nil, new(keySetID.String())))
	require.NoError(t, err)

	switchTo := func(secret *string) *orgclientsgen.UpdateClientPayload {
		return &orgclientsgen.UpdateClientPayload{
			SessionToken:                    nil,
			ApikeyToken:                     nil,
			ID:                              created.ID,
			ClientSecret:                    secret,
			TokenEndpointAuthMethod:         new(oauthwire.AuthMethodClientSecretBasic),
			TokenEndpointAuthAudienceFormat: nil,
			Scope:                           nil,
			Audience:                        nil,
			LegacyCallbackURL:               nil,
		}
	}

	_, err = ti.service.UpdateClient(ctx, switchTo(nil))
	requireOopsCode(t, err, oops.CodeBadRequest)

	updated, err := ti.service.UpdateClient(ctx, switchTo(new("tenant-secret")))
	require.NoError(t, err)
	require.Equal(t, oauthwire.AuthMethodClientSecretBasic, *updated.TokenEndpointAuthMethod)
}

func TestRotateClient_SelfClientNotRotatable(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	registration, registrations := newRegistrationServer(t)

	issuerID := createRemoteIssuer(t, ctx, ti, "self-rotate-issuer", registration.URL+"/register")

	created, err := ti.service.CreateClient(ctx, newSelfCreateClientPayload(issuerID, new(oauthwire.AuthMethodClientSecretBasic), new("tenant-secret"), nil))
	require.NoError(t, err)

	_, err = ti.service.RotateClient(ctx, &orgclientsgen.RotateClientPayload{
		ID:           created.ID,
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
	require.Zero(t, registrations.Load())
}

// selfTokenServer is a token endpoint for a tenant that publishes no
// authorization server metadata. It answers client_credentials grants with a
// numbered token and records the secret each request authenticated with.
type selfTokenServer struct {
	server  *httptest.Server
	mu      sync.Mutex
	secrets []string
}

func newSelfTokenServer(t *testing.T) *selfTokenServer {
	t.Helper()

	ts := &selfTokenServer{server: nil, mu: sync.Mutex{}, secrets: nil}
	ts.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/oauth/token" {
			http.Error(w, "not the token endpoint", http.StatusNotFound)

			return
		}

		if err := r.ParseForm(); err != nil || r.PostForm.Get(oauthwire.ParamGrantType) != oauthwire.GrantTypeClientCredentials {
			http.Error(w, "unsupported grant", http.StatusBadRequest)

			return
		}

		ts.mu.Lock()
		ts.secrets = append(ts.secrets, r.PostForm.Get(oauthwire.ParamClientSecret))
		n := len(ts.secrets)
		ts.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": fmt.Sprintf("tenant-token-%d", n), "token_type": "Bearer", "expires_in": 3600})
	}))
	t.Cleanup(ts.server.Close)

	return ts
}

func (ts *selfTokenServer) received() []string {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	return append([]string(nil), ts.secrets...)
}

// TestSelfClient_ObtainsCredentialEndToEnd creates an issuer with only a token
// endpoint and a self client through the management API, then obtains the
// client's credential with the client credentials minter. Rotating the secret
// through the API makes the next request mint again with the new secret.
func TestSelfClient_ObtainsCredentialEndToEnd(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	tokens := newSelfTokenServer(t)

	issuerID := createTokenEndpointOnlyIssuer(t, ctx, ti, "self-e2e-issuer", tokens.server.URL, tokens.server.URL+"/api/oauth/token")

	created, err := ti.service.CreateClient(ctx, newSelfCreateClientPayload(issuerID, new(oauthwire.AuthMethodClientSecretPost), new("first-secret"), nil))
	require.NoError(t, err)

	clientID, err := uuid.Parse(created.ID)
	require.NoError(t, err)

	minter := newTestMinter(t, ti)
	request := remotesessions.ClientCredentialRequest{OrganizationID: activeOrganizationID(t, ctx), ClientID: clientID, Resource: ""}

	credential, err := minter.Credential(ctx, request)
	require.NoError(t, err)
	require.Equal(t, "tenant-token-1", credential.Value())

	cached, err := minter.Credential(ctx, request)
	require.NoError(t, err)
	require.Equal(t, "tenant-token-1", cached.Value())
	require.Equal(t, []string{"first-secret"}, tokens.received(), "a second request is served from the cache")

	_, err = ti.service.UpdateClient(ctx, &orgclientsgen.UpdateClientPayload{
		SessionToken:                    nil,
		ApikeyToken:                     nil,
		ID:                              created.ID,
		ClientSecret:                    new("second-secret"),
		TokenEndpointAuthMethod:         nil,
		TokenEndpointAuthAudienceFormat: nil,
		Scope:                           nil,
		Audience:                        nil,
		LegacyCallbackURL:               nil,
	})
	require.NoError(t, err)

	rotated, err := minter.Credential(ctx, request)
	require.NoError(t, err)
	require.Equal(t, "tenant-token-2", rotated.Value())
	require.Equal(t, []string{"first-secret", "second-secret"}, tokens.received())
}

// newTestMinter builds a client credentials minter over the test service's
// database and cache, as request-time resolution would.
func newTestMinter(t *testing.T, ti *testInstance) *clientcredentials.Minter {
	t.Helper()

	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)
	enc := testenv.NewEncryptionClient(t)

	policy, err := guardian.NewUnsafePolicy(tracerProvider, []string{})
	require.NoError(t, err)

	serverURL, err := url.Parse(testServerURL)
	require.NoError(t, err)

	challenges := remotesessions.NewChallengeManager(logger, tracerProvider, testenv.NewMeterProvider(t), ti.conn, enc, policy, nil, ti.redisCache, serverURL)

	return clientcredentials.New(logger, ti.conn, enc, challenges, ti.redisCache)
}

func TestCreateClient_EmptyCredentialOwnerIsSubject(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	issuerID := createRemoteIssuer(t, ctx, ti, "empty-owner-issuer", "")

	// A direct service call skips the HTTP decoder's default.
	payload := newCreateClientPayload(issuerID, nil, new("secret"))
	payload.CredentialOwner = ""

	created, err := ti.service.CreateClient(ctx, payload)
	require.NoError(t, err)
	require.Equal(t, string(remotesessions.CredentialOwnerSubject), created.CredentialOwner)
}

func TestCreateClient_SelfClientHasNoCallbackURL(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	issuerID := createTokenEndpointOnlyIssuer(t, ctx, ti, "self-callback-issuer", "https://tenant.example.com", "https://tenant.example.com/api/oauth/token")

	created, err := ti.service.CreateClient(ctx, newSelfCreateClientPayload(issuerID, nil, new("tenant-secret"), nil))
	require.NoError(t, err)
	require.Nil(t, created.CallbackURL)

	read, err := ti.service.GetClient(ctx, &orgclientsgen.GetClientPayload{ID: created.ID, SessionToken: nil, ApikeyToken: nil})
	require.NoError(t, err)
	require.Nil(t, read.CallbackURL)

	updated, err := ti.service.UpdateClient(ctx, &orgclientsgen.UpdateClientPayload{
		SessionToken:                    nil,
		ApikeyToken:                     nil,
		ID:                              created.ID,
		ClientSecret:                    nil,
		TokenEndpointAuthMethod:         nil,
		TokenEndpointAuthAudienceFormat: nil,
		Scope:                           []string{"devices.write"},
		Audience:                        nil,
		LegacyCallbackURL:               nil,
	})
	require.NoError(t, err)
	require.Nil(t, updated.CallbackURL)

	subject, err := ti.service.CreateClient(ctx, newCreateClientPayload(issuerID, nil, new("secret")))
	require.NoError(t, err)
	require.NotNil(t, subject.CallbackURL, "a subject client still registers a callback")
}

func TestCreateRemoteSessionClient_SelfClientRefusesMissingSecret(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	issuerID := createTokenEndpointOnlyIssuer(t, ctx, ti, "self-project-invalid-issuer", "https://tenant.example.com", "https://tenant.example.com/api/oauth/token")

	_, err := ti.service.CreateRemoteSessionClient(ctx, &clientsgen.CreateRemoteSessionClientPayload{
		SessionToken:            nil,
		ApikeyToken:             nil,
		ProjectSlugInput:        nil,
		RemoteSessionIssuerID:   issuerID,
		UserSessionIssuerIds:    nil,
		ClientID:                "self-project-no-secret",
		ClientSecret:            nil,
		TokenEndpointAuthMethod: new(oauthwire.AuthMethodClientSecretPost),
		Scope:                   nil,
		Audience:                nil,
		CredentialOwner:         string(remotesessions.CredentialOwnerSelf),
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestUpdateRemoteSessionClient_SelfClientMustKeepAWorkingCredential(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	issuerID := createTokenEndpointOnlyIssuer(t, ctx, ti, "self-project-update-issuer", "https://tenant.example.com", "https://tenant.example.com/api/oauth/token")

	created, err := ti.service.CreateRemoteSessionClient(ctx, &clientsgen.CreateRemoteSessionClientPayload{
		SessionToken:            nil,
		ApikeyToken:             nil,
		ProjectSlugInput:        nil,
		RemoteSessionIssuerID:   issuerID,
		UserSessionIssuerIds:    nil,
		ClientID:                "self-project-update-client",
		ClientSecret:            new("tenant-secret"),
		TokenEndpointAuthMethod: new(oauthwire.AuthMethodClientSecretBasic),
		Scope:                   nil,
		Audience:                nil,
		CredentialOwner:         string(remotesessions.CredentialOwnerSelf),
	})
	require.NoError(t, err)

	update := func(method *string, legacyCallbackURL *bool) *clientsgen.UpdateRemoteSessionClientPayload {
		return &clientsgen.UpdateRemoteSessionClientPayload{
			SessionToken:                    nil,
			ApikeyToken:                     nil,
			ProjectSlugInput:                nil,
			ID:                              created.ID,
			ClientSecret:                    nil,
			TokenEndpointAuthMethod:         method,
			TokenEndpointAuthAudienceFormat: nil,
			Scope:                           nil,
			Audience:                        nil,
			LegacyCallbackURL:               legacyCallbackURL,
		}
	}

	_, err = ti.service.UpdateRemoteSessionClient(ctx, update(new(oauthwire.AuthMethodNone), nil))
	requireOopsCode(t, err, oops.CodeBadRequest)

	_, err = ti.service.UpdateRemoteSessionClient(withAdmin(t, ctx), update(nil, new(true)))
	requireOopsCode(t, err, oops.CodeBadRequest)

	updated, err := ti.service.UpdateRemoteSessionClient(ctx, update(new(oauthwire.AuthMethodClientSecretPost), nil))
	require.NoError(t, err)
	require.Equal(t, oauthwire.AuthMethodClientSecretPost, *updated.TokenEndpointAuthMethod)
	require.Nil(t, updated.CallbackURL)
}

func TestDetachClientKeySet_RefusedForSelfPrivateKeyJWTClient(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	organizationID := activeOrganizationID(t, ctx)
	ti.enableCustomerManagedKeys(t, ctx, organizationID)
	keySetID := createJsonWebKeySet(t, ctx, ti.conn, organizationID, "self-detach-set")

	issuerID := createTokenEndpointOnlyIssuer(t, ctx, ti, "self-detach-issuer", "https://tenant.example.com", "https://tenant.example.com/api/oauth/token")

	created, err := ti.service.CreateClient(ctx, newSelfCreateClientPayload(issuerID, new(oauthwire.AuthMethodPrivateKeyJWT), nil, new(keySetID.String())))
	require.NoError(t, err)

	_, err = ti.service.DetachClientKeySet(ctx, &orgclientsgen.DetachClientKeySetPayload{SessionToken: nil, ApikeyToken: nil, ID: created.ID})
	requireOopsCode(t, err, oops.CodeConflict)
}

func TestCreateClient_KeySetRefusals(t *testing.T) {
	t.Parallel()

	t.Run("invalid id", func(t *testing.T) {
		t.Parallel()

		ctx, ti := newTestService(t)
		ti.enableCustomerManagedKeys(t, ctx, activeOrganizationID(t, ctx))
		issuerID := createTokenEndpointOnlyIssuer(t, ctx, ti, "self-bad-set-issuer", "https://tenant.example.com", "https://tenant.example.com/api/oauth/token")

		_, err := ti.service.CreateClient(ctx, newSelfCreateClientPayload(issuerID, new(oauthwire.AuthMethodPrivateKeyJWT), nil, new("not-a-uuid")))
		requireOopsCode(t, err, oops.CodeBadRequest)
	})

	t.Run("deleted set", func(t *testing.T) {
		t.Parallel()

		ctx, ti := newTestService(t)
		organizationID := activeOrganizationID(t, ctx)
		ti.enableCustomerManagedKeys(t, ctx, organizationID)
		keySetID := createJsonWebKeySet(t, ctx, ti.conn, organizationID, "self-deleted-set")
		_, err := jsonwebkeysetsrepo.New(ti.conn).SoftDeleteJsonWebKeySet(ctx, jsonwebkeysetsrepo.SoftDeleteJsonWebKeySetParams{ID: keySetID, OrganizationID: organizationID})
		require.NoError(t, err)
		issuerID := createTokenEndpointOnlyIssuer(t, ctx, ti, "self-deleted-set-issuer", "https://tenant.example.com", "https://tenant.example.com/api/oauth/token")

		_, err = ti.service.CreateClient(ctx, newSelfCreateClientPayload(issuerID, new(oauthwire.AuthMethodPrivateKeyJWT), nil, new(keySetID.String())))
		requireOopsCode(t, err, oops.CodeNotFound)
	})

	t.Run("managed set", func(t *testing.T) {
		t.Parallel()

		ctx, ti := newTestService(t)
		ti.enableCustomerManagedKeys(t, ctx, activeOrganizationID(t, ctx))
		_, fx := provisionManagedClient(t, ctx, ti, "self-managed-target-issuer")
		issuerID := createTokenEndpointOnlyIssuer(t, ctx, ti, "self-managed-set-issuer", "https://tenant.example.com", "https://tenant.example.com/api/oauth/token")

		_, err := ti.service.CreateClient(ctx, newSelfCreateClientPayload(issuerID, new(oauthwire.AuthMethodPrivateKeyJWT), nil, new(fx.Client.JSONWebKeySetID.UUID.String())))
		requireOopsCode(t, err, oops.CodeConflict)
	})
}

func TestCreateCimdClients_RecordCreateSnapshot(t *testing.T) {
	t.Parallel()

	t.Run("project", func(t *testing.T) {
		t.Parallel()

		ctx, ti := newTestService(t)
		issuerID := createCIMDIssuer(t, ctx, ti, "cimd-snapshot-issuer", "https://idp.example.com/authorize", "https://idp.example.com/token")
		userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "cimd-snapshot-usi")

		created := createCimdClient(t, ctx, ti, issuerID.String(), userIssuerID.String(), nil)

		requireLatestClientCreateSnapshot(t, ctx, ti, created.ID)
	})

	t.Run("organization", func(t *testing.T) {
		t.Parallel()

		ctx, ti := newTestService(t)
		issuerID := createCIMDIssuer(t, ctx, ti, "cimd-org-snapshot-issuer", "https://idp.example.com/authorize", "https://idp.example.com/token")

		created, err := ti.service.CreateCimdClient(ctx, &orgclientsgen.CreateCimdClientPayload{
			SessionToken:          nil,
			ApikeyToken:           nil,
			RemoteSessionIssuerID: issuerID.String(),
			ProjectID:             nil,
			Scope:                 nil,
			Audience:              nil,
		})
		require.NoError(t, err)

		requireLatestClientCreateSnapshot(t, ctx, ti, created.ID)
	})
}

// requireLatestClientCreateSnapshot asserts the latest client create audit
// entry snapshots clientID as a subject client.
func requireLatestClientCreateSnapshot(t *testing.T, ctx context.Context, ti *testInstance, clientID string) {
	t.Helper()

	entry, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionRemoteSessionClientCreate)
	require.NoError(t, err)
	require.Equal(t, clientID, entry.SubjectID)

	snapshot, err := audittest.DecodeAuditData(entry.AfterSnapshot)
	require.NoError(t, err)
	require.Equal(t, clientID, snapshot["ID"])
	require.Equal(t, string(remotesessions.CredentialOwnerSubject), snapshot["CredentialOwner"])
}

func TestUpdateIssuer_KeepsTokenEndpointForSelfClients(t *testing.T) {
	t.Parallel()

	t.Run("project", func(t *testing.T) {
		t.Parallel()

		ctx, ti := newTestService(t)
		issuerID := createTokenEndpointOnlyIssuer(t, ctx, ti, "self-keep-te-issuer", "https://tenant.example.com", "https://tenant.example.com/api/oauth/token")

		clearTokenEndpoint := &issuersgen.UpdateRemoteSessionIssuerPayload{ID: issuerID, TokenEndpoint: new("")}

		_, err := ti.service.CreateClient(ctx, newSelfCreateClientPayload(issuerID, nil, new("tenant-secret"), nil))
		require.NoError(t, err)

		_, err = ti.service.UpdateRemoteSessionIssuer(ctx, clearTokenEndpoint)
		requireOopsCode(t, err, oops.CodeConflict)

		// Other edits, including replacing the token endpoint, still apply.
		updated, err := ti.service.UpdateRemoteSessionIssuer(ctx, &issuersgen.UpdateRemoteSessionIssuerPayload{ID: issuerID, TokenEndpoint: new("https://tenant.example.com/api/oauth/v2/token")})
		require.NoError(t, err)
		require.Equal(t, "https://tenant.example.com/api/oauth/v2/token", *updated.TokenEndpoint)
	})

	t.Run("organization", func(t *testing.T) {
		t.Parallel()

		ctx, ti := newTestService(t)
		issuerID := seedOrgLevelRemoteIssuer(t, ctx, ti.conn, activeOrganizationID(t, ctx), "self-keep-te-org-issuer")

		_, err := ti.service.CreateClient(ctx, newSelfCreateClientPayload(issuerID.String(), nil, new("tenant-secret"), nil))
		require.NoError(t, err)

		_, err = ti.service.UpdateIssuer(ctx, &orgissuersgen.UpdateIssuerPayload{ID: issuerID.String(), TokenEndpoint: new("")})
		requireOopsCode(t, err, oops.CodeConflict)
	})

	t.Run("subject clients only", func(t *testing.T) {
		t.Parallel()

		ctx, ti := newTestService(t)
		issuerID := createRemoteIssuer(t, ctx, ti, "subject-clear-te-issuer", "")

		_, err := ti.service.CreateClient(ctx, newCreateClientPayload(issuerID, nil, new("secret")))
		require.NoError(t, err)

		updated, err := ti.service.UpdateRemoteSessionIssuer(ctx, &issuersgen.UpdateRemoteSessionIssuerPayload{ID: issuerID, TokenEndpoint: new("")})
		require.NoError(t, err)
		require.Nil(t, updated.TokenEndpoint)
	})
}
