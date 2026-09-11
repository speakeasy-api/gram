package remotesessions_test

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	orgclientsgen "github.com/speakeasy-api/gram/server/gen/organization_remote_session_clients"
	clientsgen "github.com/speakeasy-api/gram/server/gen/remote_session_clients"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// newRegistrationServer serves an RFC 7591 registration endpoint that hands
// out one replacement client and counts how often it was asked.
func newRegistrationServer(t *testing.T) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var registrations atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/register" || r.Method != http.MethodPost {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		registrations.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"client_id":"rotated-cid","client_secret":"rotated-secret","token_endpoint_auth_method":"client_secret_basic","client_id_issued_at":1700000000,"client_secret_expires_at":1707776000}`))
	}))
	t.Cleanup(server.Close)
	return server, &registrations
}

// An administrator's rotation needs no upstream confirmation: the client is
// re-registered at the issuer's published endpoint (the row predates the
// endpoint being recorded), the row keeps its id and bindings, the endpoint is
// recorded for future automatic rotations, and the sessions minted against the
// old client_id are revoked.
func TestRotateClient(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	registration, registrations := newRegistrationServer(t)

	issuerID := createRemoteIssuer(t, ctx, ti, "rotate-client-issuer", registration.URL+"/register")
	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "rotate-client-usi")
	clientID := createRemoteClient(t, ctx, ti, issuerID, userIssuerID.String(), "rotate-client-original")
	subject := urn.NewUserSubject("rotate-client-user")
	insertRemoteSession(t, ctx, ti.conn, subject, userIssuerID.String(), clientID)

	rotated, err := ti.service.RotateClient(ctx, &orgclientsgen.RotateClientPayload{
		ID:           clientID,
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.NoError(t, err)
	require.EqualValues(t, 1, registrations.Load())

	require.Equal(t, clientID, rotated.ID, "the row is replaced in place")
	require.Equal(t, "rotated-cid", rotated.ClientID)
	require.Equal(t, []string{userIssuerID.String()}, rotated.UserSessionIssuerIds, "issuer bindings survive")
	require.NotNil(t, rotated.RegistrationEndpoint)
	require.Equal(t, registration.URL+"/register", *rotated.RegistrationEndpoint)
	require.NotNil(t, rotated.ClientSecretExpiresAt)
	require.Nil(t, rotated.UpstreamRejectedAt)
	require.NotNil(t, rotated.TokenEndpointAuthMethod)
	require.Equal(t, "client_secret_basic", *rotated.TokenEndpointAuthMethod)

	clientUUID, err := uuid.Parse(clientID)
	require.NoError(t, err)
	_, err = repo.New(ti.conn).GetActiveRemoteSession(ctx, repo.GetActiveRemoteSessionParams{
		SubjectUrn:            subject,
		RemoteSessionClientID: clientUUID,
	})
	require.ErrorIs(t, err, pgx.ErrNoRows, "sessions bound to the old client_id are revoked")
}

func TestRotateClient_RBACForbidden(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	registration, registrations := newRegistrationServer(t)

	issuerID := createRemoteIssuer(t, ctx, ti, "rotate-rbac-issuer", registration.URL+"/register")
	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "rotate-rbac-usi")
	clientID := createRemoteClient(t, ctx, ti, issuerID, userIssuerID.String(), "rotate-rbac-client")

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	ctx = withExactAccessGrants(t, ctx, ti.conn, authz.Grant{
		Scope:    authz.ScopeOrgRead,
		Selector: authz.NewSelector(authz.ScopeOrgRead, authCtx.ActiveOrganizationID),
	})

	_, err := ti.service.RotateClient(ctx, &orgclientsgen.RotateClientPayload{
		ID:           clientID,
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeForbidden)
	require.Zero(t, registrations.Load())
}

func TestRotateClient_NotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	_, err := ti.service.RotateClient(ctx, &orgclientsgen.RotateClientPayload{
		ID:           uuid.NewString(),
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeNotFound)
}

// Neither the row nor the issuer names a registration endpoint, so there is
// nowhere to re-register: the request is refused and the client is untouched.
func TestRotateClient_NoRegistrationEndpoint(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	issuerID := createRemoteIssuer(t, ctx, ti, "rotate-noendpoint-issuer", "")
	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "rotate-noendpoint-usi")
	clientID := createRemoteClient(t, ctx, ti, issuerID, userIssuerID.String(), "rotate-noendpoint-client")

	_, err := ti.service.RotateClient(ctx, &orgclientsgen.RotateClientPayload{
		ID:           clientID,
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeBadRequest)

	current, err := ti.service.GetClient(ctx, &orgclientsgen.GetClientPayload{
		ID:           clientID,
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.NoError(t, err)
	require.Equal(t, "rotate-noendpoint-client", current.ClientID)
}

// An issuer that refuses the replacement surfaces its refusal to the
// administrator and leaves the client as it was.
func TestRotateClient_IssuerRejectsRegistration(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	refusing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_redirect_uri","error_description":"redirect_uris not allowed"}`))
	}))
	t.Cleanup(refusing.Close)

	issuerID := createRemoteIssuer(t, ctx, ti, "rotate-refused-issuer", refusing.URL+"/register")
	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "rotate-refused-usi")
	clientID := createRemoteClient(t, ctx, ti, issuerID, userIssuerID.String(), "rotate-refused-client")

	_, err := ti.service.RotateClient(ctx, &orgclientsgen.RotateClientPayload{
		ID:           clientID,
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeBadRequest)
	require.ErrorContains(t, err, "redirect_uris not allowed")

	current, err := ti.service.GetClient(ctx, &orgclientsgen.GetClientPayload{
		ID:           clientID,
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.NoError(t, err)
	require.Equal(t, "rotate-refused-client", current.ClientID)
}

// A client in another organization is invisible to the caller: not found,
// and its issuer is never contacted.
func TestRotateClient_CrossOrganizationNotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	registration, registrations := newRegistrationServer(t)

	otherOrg := createOrganization(t, ctx, ti.conn, "rotate-other-org")
	otherIssuer := seedRemoteIssuerWithURL(t, ctx, ti.conn, uuid.NullUUID{}, conv.ToPGText(otherOrg), "rotate-other-issuer", registration.URL)
	otherClient := seedOrgLevelRemoteClient(t, ctx, ti.conn, otherOrg, otherIssuer, "rotate-other-client")

	_, err := ti.service.RotateClient(ctx, &orgclientsgen.RotateClientPayload{
		ID:           otherClient.String(),
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeNotFound)
	require.Zero(t, registrations.Load())
}

// A private_key_jwt client's registration is bound to a key set, which a
// dynamic registration cannot reproduce, so it is refused and untouched.
func TestRotateClient_PrivateKeyJWTClientNotRotatable(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	registration, registrations := newRegistrationServer(t)

	issuerID := createRemoteIssuer(t, ctx, ti, "rotate-pkjwt-issuer", registration.URL+"/register")
	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "rotate-pkjwt-usi")
	clientID := createRemoteClient(t, ctx, ti, issuerID, userIssuerID.String(), "rotate-pkjwt-client")

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	clientUUID, err := uuid.Parse(clientID)
	require.NoError(t, err)
	forceTokenEndpointAuthMethod(t, ctx, ti.conn, clientUUID, *authCtx.ProjectID, "private_key_jwt")

	_, err = ti.service.RotateClient(ctx, &orgclientsgen.RotateClientPayload{
		ID:           clientID,
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeBadRequest)
	require.Zero(t, registrations.Load())
}

// Rotating the secret by hand clears an earlier upstream rejection: the
// rejection may have been the old secret, and a client that authenticates
// again must not stay flagged for re-registration.
func TestUpdateClient_NewSecretClearsUpstreamRejection(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	issuerID := createRemoteIssuer(t, ctx, ti, "rotate-update-issuer", "")
	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "rotate-update-usi")
	clientID := createRemoteClient(t, ctx, ti, issuerID, userIssuerID.String(), "rotate-update-client")
	clientUUID, err := uuid.Parse(clientID)
	require.NoError(t, err)

	rejectedAt := time.Now().Add(-time.Hour)
	expiredAt := time.Now().Add(-time.Minute)
	n, err := repo.New(ti.conn).ForceRemoteSessionClientRegistrationFixture(ctx, repo.ForceRemoteSessionClientRegistrationFixtureParams{
		RegistrationEndpoint:  pgtype.Text{String: "", Valid: false},
		ClientSecretExpiresAt: conv.ToPGTimestamptz(expiredAt),
		UpstreamRejectedAt:    conv.ToPGTimestamptz(rejectedAt),
		ID:                    clientUUID,
	})
	require.NoError(t, err)
	require.EqualValues(t, 1, n)

	audience := "https://api.example.com"
	unchanged, err := ti.service.UpdateClient(ctx, &orgclientsgen.UpdateClientPayload{
		ID:           clientID,
		Audience:     &audience,
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.NoError(t, err)
	require.NotNil(t, unchanged.UpstreamRejectedAt, "an update that leaves the secret alone keeps the marker")
	require.NotNil(t, unchanged.ClientSecretExpiresAt, "and keeps the old secret's expiry")

	secret := "a-new-secret"
	updated, err := ti.service.UpdateClient(ctx, &orgclientsgen.UpdateClientPayload{
		ID:           clientID,
		ClientSecret: &secret,
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.NoError(t, err)
	require.Nil(t, updated.UpstreamRejectedAt, "a new secret clears the marker")
	require.Nil(t, updated.ClientSecretExpiresAt, "a pasted-in secret carries no expiry, so the old deadline cannot flag it as expired")
}

// The registration endpoint a create form records is tied to the issuer, so
// a later rotation can only ever post to an endpoint the issuer owns.
func TestCreateRemoteSessionClient_RegistrationEndpointMustBelongToIssuer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	published := createRemoteIssuer(t, ctx, ti, "provenance-published-issuer", "https://idp.example.com/register")
	unpublished := createRemoteIssuer(t, ctx, ti, "provenance-unpublished-issuer", "")
	// Standalone clients (no user session issuer attachment): the single
	// binding per (user session issuer, remote issuer) guard would otherwise
	// refuse the second successful create against the same issuer.
	create := func(issuerID, clientID, endpoint string) (*types.RemoteSessionClient, error) {
		return ti.service.CreateRemoteSessionClient(ctx, &clientsgen.CreateRemoteSessionClientPayload{
			RemoteSessionIssuerID: issuerID,
			UserSessionIssuerIds:  nil,
			ClientID:              clientID,
			ClientSecret:          nil,
			RegistrationEndpoint:  &endpoint,
			SessionToken:          nil,
			ApikeyToken:           nil,
			ProjectSlugInput:      nil,
		})
	}

	_, err := create(published, "provenance-bad-scheme", "ftp://idp.example.com/register")
	requireOopsCode(t, err, oops.CodeBadRequest)

	_, err = create(published, "provenance-plaintext", "http://idp.example.com/register")
	requireOopsCode(t, err, oops.CodeBadRequest)

	_, err = create(published, "provenance-other-host", "https://attacker.example.net/register")
	requireOopsCode(t, err, oops.CodeBadRequest)

	created, err := create(published, "provenance-match", "https://idp.example.com/register")
	require.NoError(t, err)
	require.NotNil(t, created.RegistrationEndpoint)
	require.Equal(t, "https://idp.example.com/register", *created.RegistrationEndpoint)

	// An issuer that publishes no endpoint cannot vouch for a sibling host, so
	// the client is created without provenance rather than refused: providers
	// do register on sibling hosts, and the client simply never rotates on its
	// own.
	foreign, err := create(unpublished, "provenance-foreign-origin", "https://attacker.example.net/register")
	require.NoError(t, err)
	require.Nil(t, foreign.RegistrationEndpoint)

	sameOrigin, err := create(unpublished, "provenance-same-origin", "https://idp.example.com/oauth/register")
	require.NoError(t, err)
	require.NotNil(t, sameOrigin.RegistrationEndpoint)
}
