package identityproviders_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/identity_providers"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/identityproviders/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

const (
	fakeOktaPassed      = "passed"
	fakeOktaRefused     = "refused"
	fakeOktaUnavailable = "unavailable"
	fakeOktaAppsBlocked = "apps_blocked"
	testAccessToken     = "test-access-token"
	testClientID        = "test-client-id"
)

type fakeOktaServer struct {
	server *httptest.Server
	mode   string

	mu            sync.Mutex
	publicJWK     jose.JSONWebKey
	validationErr error
	tokenStarted  chan struct{}
	releaseToken  chan struct{}
	startOnce     sync.Once
	releaseOnce   sync.Once
}

func TestVerifySetupStepPassesAndPersistsEvidence(t *testing.T) {
	t.Parallel()

	fake := newFakeOktaServer(t, fakeOktaPassed)
	ctx, ti := newTestServiceWithOktaEndpoint(t, fake.server.URL)
	connection := prepareConnectionForVerification(t, ctx, ti, fake)
	beforeAudits, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionIdentityProviderConnectionVerified)
	require.NoError(t, err)

	result, err := ti.service.VerifySetupStep(ctx, &gen.VerifySetupStepPayload{StepKey: "connect", SessionToken: nil, ApikeyToken: nil})
	require.NoError(t, err)
	require.Equal(t, "passed", result.Outcome)
	require.Equal(t, []string{"directory_read", "application_assignment_read", "sign_in_provisioning"}, result.Capabilities)
	require.Equal(t, []string{"okta.apps.read", "okta.groups.read", "okta.users.read", "okta.apps.manage"}, result.GrantedScopes)
	require.Len(t, result.Evidence.Reads, 3)
	require.True(t, result.Evidence.Reads[0].OK)
	require.Contains(t, *result.Evidence.Reads[0].Detail, "more pages are available")
	require.NoError(t, fake.ValidationError())

	stored, err := repo.New(ti.conn).GetIdentityProviderConnectionByOrganization(ctx, ti.orgID)
	require.NoError(t, err)
	require.Equal(t, mustUUID(t, connection.ID), stored.ID)
	require.Equal(t, "active", stored.Status)
	require.Equal(t, result.Capabilities, stored.Capabilities)
	require.Equal(t, result.GrantedScopes, stored.GrantedScopes)
	require.True(t, stored.LastVerifiedAt.Valid)
	require.NotContains(t, string(stored.VerifyEvidence), testAccessToken)
	require.NotContains(t, string(stored.VerifyEvidence), "PRIVATE KEY")

	signingKey, err := repo.New(ti.conn).GetIdentityProviderSigningKey(ctx, repo.GetIdentityProviderSigningKeyParams{
		OrganizationID:               ti.orgID,
		IdentityProviderConnectionID: stored.ID,
	})
	require.NoError(t, err)
	require.True(t, signingKey.LastUsedAt.Valid)

	afterAudits, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionIdentityProviderConnectionVerified)
	require.NoError(t, err)
	require.Equal(t, beforeAudits+1, afterAudits)
	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionIdentityProviderConnectionVerified)
	require.NoError(t, err)
	afterSnapshot, err := audittest.DecodeAuditData(record.AfterSnapshot)
	require.NoError(t, err)
	require.Equal(t, "passed", afterSnapshot["outcome"])
	require.Equal(t, []any{"directory_read", "application_assignment_read", "sign_in_provisioning"}, afterSnapshot["capabilities"])
	auditJSON := string(record.Metadata) + string(record.BeforeSnapshot) + string(record.AfterSnapshot)
	require.NotContains(t, auditJSON, testAccessToken)
	require.NotContains(t, auditJSON, "PRIVATE KEY")

	setup, err := ti.service.DescribeSetup(ctx, &gen.DescribeSetupPayload{SessionToken: nil, ApikeyToken: nil})
	require.NoError(t, err)
	require.Equal(t, "passed", setup.Steps[0].State)
	require.Equal(t, result, setup.Steps[0].LastOutcome)
	getResult, err := ti.service.Get(ctx, &gen.GetPayload{SessionToken: nil, ApikeyToken: nil})
	require.NoError(t, err)
	require.Equal(t, result.Evidence, getResult.Connection.VerifyEvidence)
}

func TestVerifySetupStepPersistsInvalidClientRefusal(t *testing.T) {
	t.Parallel()

	fake := newFakeOktaServer(t, fakeOktaRefused)
	ctx, ti := newTestServiceWithOktaEndpoint(t, fake.server.URL)
	prepareConnectionForVerification(t, ctx, ti, fake)

	result, err := ti.service.VerifySetupStep(ctx, &gen.VerifySetupStepPayload{StepKey: "connect", SessionToken: nil, ApikeyToken: nil})
	require.NoError(t, err)
	require.Equal(t, "refused", result.Outcome)
	require.Contains(t, result.Detail, "JWKS URL")
	require.Contains(t, result.Detail, "Client ID")
	require.Contains(t, result.Detail, "Okta said: invalid_client: The client assertion could not be verified.")
	require.Empty(t, result.Capabilities)
	require.Empty(t, result.GrantedScopes)
	require.Empty(t, result.Evidence.Reads)
	require.NoError(t, fake.ValidationError())

	stored, err := repo.New(ti.conn).GetIdentityProviderConnectionByOrganization(ctx, ti.orgID)
	require.NoError(t, err)
	require.Equal(t, "failed", stored.Status)
	require.Equal(t, result.Detail, stored.StatusDetail.String)
	require.NotContains(t, string(stored.VerifyEvidence), testAccessToken)
	signingKey, err := repo.New(ti.conn).GetIdentityProviderSigningKey(ctx, repo.GetIdentityProviderSigningKeyParams{OrganizationID: ti.orgID, IdentityProviderConnectionID: stored.ID})
	require.NoError(t, err)
	require.False(t, signingKey.LastUsedAt.Valid)
}

func TestVerifySetupStepReportsMissingApplicationRead(t *testing.T) {
	t.Parallel()

	fake := newFakeOktaServer(t, fakeOktaAppsBlocked)
	ctx, ti := newTestServiceWithOktaEndpoint(t, fake.server.URL)
	prepareConnectionForVerification(t, ctx, ti, fake)

	result, err := ti.service.VerifySetupStep(ctx, &gen.VerifySetupStepPayload{StepKey: "connect", SessionToken: nil, ApikeyToken: nil})
	require.NoError(t, err)
	require.Equal(t, "capability_missing", result.Outcome)
	require.Contains(t, result.Detail, "apps read failed")
	require.Equal(t, []string{"directory_read", "sign_in_provisioning"}, result.Capabilities)
	require.Len(t, result.Evidence.Reads, 3)
	require.False(t, result.Evidence.Reads[2].OK)
	require.Equal(t, "Okta application read is not permitted.", *result.Evidence.Reads[2].Detail)
	require.NoError(t, fake.ValidationError())

	stored, err := repo.New(ti.conn).GetIdentityProviderConnectionByOrganization(ctx, ti.orgID)
	require.NoError(t, err)
	require.Equal(t, "failed", stored.Status)
	require.Equal(t, result.Capabilities, stored.Capabilities)
	require.Equal(t, result.GrantedScopes, stored.GrantedScopes)
	signingKey, err := repo.New(ti.conn).GetIdentityProviderSigningKey(ctx, repo.GetIdentityProviderSigningKeyParams{OrganizationID: ti.orgID, IdentityProviderConnectionID: stored.ID})
	require.NoError(t, err)
	require.True(t, signingKey.LastUsedAt.Valid)
}

func TestVerifySetupStepPersistsUnreachableOutcome(t *testing.T) {
	t.Parallel()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	endpoint := "http://" + listener.Addr().String()
	require.NoError(t, listener.Close())
	ctx, ti := newTestServiceWithOktaEndpoint(t, endpoint)
	connection := createConnection(t, ctx, ti, "https://example.okta.com")
	setStoredClientID(t, ctx, ti)

	result, err := ti.service.VerifySetupStep(ctx, &gen.VerifySetupStepPayload{StepKey: "connect", SessionToken: nil, ApikeyToken: nil})
	require.NoError(t, err)
	require.Equal(t, "unreachable", result.Outcome)
	require.Contains(t, result.Detail, "Unable to reach")
	require.Empty(t, result.Evidence.Reads)

	stored, err := repo.New(ti.conn).GetIdentityProviderConnectionByOrganization(ctx, ti.orgID)
	require.NoError(t, err)
	require.Equal(t, mustUUID(t, connection.ID), stored.ID)
	require.Equal(t, "failed", stored.Status)
}

func TestVerifySetupStepPersistsProviderUnreachableDetail(t *testing.T) {
	t.Parallel()

	fake := newFakeOktaServer(t, fakeOktaUnavailable)
	ctx, ti := newTestServiceWithOktaEndpoint(t, fake.server.URL)
	prepareConnectionForVerification(t, ctx, ti, fake)

	result, err := ti.service.VerifySetupStep(ctx, &gen.VerifySetupStepPayload{StepKey: "connect", SessionToken: nil, ApikeyToken: nil})
	require.NoError(t, err)
	require.Equal(t, "unreachable", result.Outcome)
	require.Contains(t, result.Detail, "Okta said: server_error: The token service is temporarily unavailable.")

	stored, err := repo.New(ti.conn).GetIdentityProviderConnectionByOrganization(ctx, ti.orgID)
	require.NoError(t, err)
	require.Equal(t, result.Detail, stored.StatusDetail.String)
}

func TestVerifySetupStepRejectsPendingConnectionWithoutClientID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	createConnection(t, ctx, ti, "https://example.okta.com")
	beforeAudits, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionIdentityProviderConnectionVerified)
	require.NoError(t, err)

	_, err = ti.service.VerifySetupStep(ctx, &gen.VerifySetupStepPayload{StepKey: "connect", SessionToken: nil, ApikeyToken: nil})
	requireOopsCode(t, err, oops.CodeBadRequest)
	require.ErrorContains(t, err, "Client ID")
	afterAudits, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionIdentityProviderConnectionVerified)
	require.NoError(t, err)
	require.Equal(t, beforeAudits, afterAudits)
}

func TestVerifySetupStepRequiresOrganizationAdmin(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	ctx = authztest.WithExactGrants(t, ctx)

	_, err := ti.service.VerifySetupStep(ctx, &gen.VerifySetupStepPayload{StepKey: "connect", SessionToken: nil, ApikeyToken: nil})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestVerifySetupStepRejectsResultWhenClientIDChangesDuringProbe(t *testing.T) {
	t.Parallel()

	fake := newFakeOktaServer(t, fakeOktaPassed)
	fake.BlockTokenResponse()
	t.Cleanup(fake.ReleaseTokenResponse)
	ctx, ti := newTestServiceWithOktaEndpoint(t, fake.server.URL)
	prepareConnectionForVerification(t, ctx, ti, fake)
	beforeAudits, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionIdentityProviderConnectionVerified)
	require.NoError(t, err)

	type verifyCall struct {
		result *gen.IdentityProviderVerifyResult
		err    error
	}
	finished := make(chan verifyCall, 1)
	go func() {
		result, err := ti.service.VerifySetupStep(ctx, &gen.VerifySetupStepPayload{StepKey: "connect", SessionToken: nil, ApikeyToken: nil})
		finished <- verifyCall{result: result, err: err}
	}()
	<-fake.tokenStarted
	_, err = ti.service.SubmitSetupStep(ctx, &gen.SubmitSetupStepPayload{
		StepKey:      "connect",
		Values:       []*gen.IdentityProviderSetupValue{{Key: "client_id", Value: "replacement-client-id"}},
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.NoError(t, err)
	fake.ReleaseTokenResponse()
	call := <-finished
	require.Nil(t, call.result)
	requireOopsCode(t, call.err, oops.CodeConflict)
	require.NoError(t, fake.ValidationError())

	stored, err := repo.New(ti.conn).GetIdentityProviderConnectionByOrganization(ctx, ti.orgID)
	require.NoError(t, err)
	require.Equal(t, "replacement-client-id", stored.ClientID.String)
	require.Equal(t, "awaiting_verification", stored.Status)
	afterAudits, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionIdentityProviderConnectionVerified)
	require.NoError(t, err)
	require.Equal(t, beforeAudits, afterAudits)
}

func newFakeOktaServer(t *testing.T, mode string) *fakeOktaServer {
	t.Helper()
	fake := &fakeOktaServer{
		server:        nil,
		mode:          mode,
		mu:            sync.Mutex{},
		publicJWK:     jose.JSONWebKey{},
		validationErr: nil,
		tokenStarted:  nil,
		releaseToken:  nil,
		startOnce:     sync.Once{},
		releaseOnce:   sync.Once{},
	}
	fake.server = httptest.NewServer(http.HandlerFunc(fake.handle))
	t.Cleanup(fake.server.Close)
	return fake
}

func (f *fakeOktaServer) handle(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/oauth2/v1/token":
		f.handleToken(w, r)
	case "/api/v1/groups":
		f.handleCollection(w, r, "groups")
	case "/api/v1/users":
		f.handleCollection(w, r, "users")
	case "/api/v1/apps":
		f.handleCollection(w, r, "apps")
	default:
		http.NotFound(w, r)
	}
}

func (f *fakeOktaServer) handleToken(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		f.fail(w, err)
		return
	}
	expectedScopes := "okta.apps.read okta.groups.read okta.users.read okta.apps.manage"
	if r.Method != http.MethodPost || r.Form.Get("grant_type") != "client_credentials" || r.Form.Get("scope") != expectedScopes || r.Form.Get("client_assertion_type") != "urn:ietf:params:oauth:client-assertion-type:jwt-bearer" {
		f.fail(w, errors.New("unexpected token request"))
		return
	}

	f.mu.Lock()
	publicJWK := f.publicJWK
	f.mu.Unlock()
	token, err := jwt.ParseSigned(r.Form.Get("client_assertion"), []jose.SignatureAlgorithm{jose.RS256})
	if err != nil {
		f.fail(w, err)
		return
	}
	var claims jwt.Claims
	if err := token.Claims(publicJWK.Key, &claims); err != nil {
		f.fail(w, err)
		return
	}
	if len(token.Headers) != 1 || token.Headers[0].KeyID != publicJWK.KeyID || claims.Issuer != testClientID || claims.Subject != testClientID || len(claims.Audience) != 1 || claims.Audience[0] != f.server.URL+"/oauth2/v1/token" || claims.ID == "" || claims.Expiry == nil {
		f.fail(w, errors.New("unexpected client assertion"))
		return
	}
	if f.tokenStarted != nil {
		f.startOnce.Do(func() { close(f.tokenStarted) })
		<-f.releaseToken
	}

	if f.mode == fakeOktaRefused {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid_client","error_description":"The client assertion could not be verified."}`))
		return
	}
	if f.mode == fakeOktaUnavailable {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error":"server_error","error_description":"The token service is temporarily unavailable."}`))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"access_token":"` + testAccessToken + `","expires_in":3600,"scope":"` + expectedScopes + `"}`))
}

func (f *fakeOktaServer) handleCollection(w http.ResponseWriter, r *http.Request, resource string) {
	if r.Method != http.MethodGet || r.Header.Get("Authorization") != "Bearer "+testAccessToken || r.URL.Query().Get("limit") != "1" {
		f.fail(w, errors.New("unexpected collection request"))
		return
	}
	if resource == "apps" && f.mode == fakeOktaAppsBlocked {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"errorCode":"E0000006","errorSummary":"Okta application read is not permitted."}`))
		return
	}
	if resource == "groups" {
		w.Header().Set("Link", `<`+f.server.URL+`/api/v1/groups?after=next-cursor&limit=1>; rel="next"`)
	}
	w.Header().Set("X-Rate-Limit-Remaining", "99")
	w.Header().Set("X-Rate-Limit-Reset", fmt.Sprintf("%d", time.Now().Add(time.Minute).Unix()))
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`[{"id":"` + resource + `-1"}]`))
}

func (f *fakeOktaServer) fail(w http.ResponseWriter, err error) {
	f.mu.Lock()
	if f.validationErr == nil {
		f.validationErr = err
	}
	f.mu.Unlock()
	http.Error(w, "invalid request", http.StatusBadRequest)
}

func (f *fakeOktaServer) SetPublicJWK(raw []byte) {
	var publicJWK jose.JSONWebKey
	if err := json.Unmarshal(raw, &publicJWK); err != nil {
		f.mu.Lock()
		f.validationErr = err
		f.mu.Unlock()
		return
	}
	f.mu.Lock()
	f.publicJWK = publicJWK
	f.mu.Unlock()
}

func (f *fakeOktaServer) ValidationError() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.validationErr
}

func (f *fakeOktaServer) BlockTokenResponse() {
	f.tokenStarted = make(chan struct{})
	f.releaseToken = make(chan struct{})
}

func (f *fakeOktaServer) ReleaseTokenResponse() {
	if f.releaseToken != nil {
		f.releaseOnce.Do(func() { close(f.releaseToken) })
	}
}

func prepareConnectionForVerification(t *testing.T, ctx context.Context, ti *testInstance, fake *fakeOktaServer) *gen.IdentityProviderConnection {
	t.Helper()
	connection := createConnection(t, ctx, ti, "https://example.okta.com")
	storedKey, err := repo.New(ti.conn).GetIdentityProviderSigningKey(ctx, repo.GetIdentityProviderSigningKeyParams{
		OrganizationID:               ti.orgID,
		IdentityProviderConnectionID: mustUUID(t, connection.ID),
	})
	require.NoError(t, err)
	fake.SetPublicJWK(storedKey.PublicJwk)
	setStoredClientID(t, ctx, ti)
	return connection
}

func setStoredClientID(t *testing.T, ctx context.Context, ti *testInstance) {
	t.Helper()
	_, err := ti.service.SubmitSetupStep(ctx, &gen.SubmitSetupStepPayload{
		StepKey:      "connect",
		Values:       []*gen.IdentityProviderSetupValue{{Key: "client_id", Value: testClientID}},
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.NoError(t, err)
}
