package identityproviderconnections_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/identity_provider_connections"
	"github.com/speakeasy-api/gram/server/internal/conv"
	idpc "github.com/speakeasy-api/gram/server/internal/identityproviderconnections"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	remotesessionsrepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

const signInAgentID = "wlpagent000000000001"

func TestSignInReader_ReportsSetupProgress(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)
	origin := mustURL(t, "https://app.example.com")
	reader := idpc.NewSignInReader(testenv.NewLogger(t), si.conn.conn, remotesessions.DefaultCallbackOrigins(origin))

	_, err := reader.Read(ctx, si.orgID)
	require.ErrorIs(t, err, idpc.ErrConnectionNotFound)

	created := createConnection(t, ctx, si, fullOrgURL)
	setup, err := reader.Read(ctx, si.orgID)
	require.NoError(t, err)
	require.Equal(t, idpc.SignInStepVerifyConnection, setup.NextStep)
	require.NotEmpty(t, setup.Checklist)

	submitClientID(t, ctx, si, created.ID)
	setup, err = reader.Read(ctx, si.orgID)
	require.NoError(t, err)
	require.Equal(t, idpc.SignInStepRecordAgent, setup.NextStep)
	require.False(t, setup.AgentRecorded)

	_, err = si.svc.RecordAgent(ctx, &gen.RecordAgentPayload{SessionToken: nil, ID: created.ID, AgentID: conv.PtrEmpty(signInAgentID), AgentAppID: conv.PtrEmpty("0oassoapp00000000001")})
	require.NoError(t, err)
	setup, err = reader.Read(ctx, si.orgID)
	require.NoError(t, err)
	require.Equal(t, idpc.SignInStepSetUpSignIn, setup.NextStep)
	require.True(t, setup.AgentRecorded)
	require.False(t, setup.ClientRegistered)
	require.Empty(t, setup.RedirectURI)
	require.Empty(t, setup.JWKSURL)

	managed, err := si.provisioner.GetManagedClient(ctx, si.orgID, uuid.MustParse(created.ID))
	require.NoError(t, err)
	clientID := createSignInClient(t, ctx, si, managed.IssuerID, []string{"openid"})

	setup, err = reader.Read(ctx, si.orgID)
	require.NoError(t, err)
	require.True(t, setup.ClientRegistered)
	require.False(t, setup.ClientReady)
	require.Equal(t, idpc.SignInStepSetUpSignIn, setup.NextStep)
	require.Equal(t, "https://app.example.com/mcp/idp_callback/"+clientID.String(), setup.RedirectURI)
	require.Equal(t, "https://app.example.com/.well-known/oauth-client/"+clientID.String()+"/jwks.json", setup.JWKSURL)

	rsRepo := remotesessionsrepo.New(si.conn.conn)
	_, err = rsRepo.SetOrganizationRemoteSessionClientJsonWebKeySet(ctx, remotesessionsrepo.SetOrganizationRemoteSessionClientJsonWebKeySetParams{
		JsonWebKeySetID: uuid.NullUUID{UUID: managed.JSONWebKeySetID.UUID, Valid: true},
		ID:              clientID,
		OrganizationID:  conv.ToPGText(si.orgID),
	})
	require.NoError(t, err)
	_, err = rsRepo.UpdateOrganizationRemoteSessionClient(ctx, remotesessionsrepo.UpdateOrganizationRemoteSessionClientParams{
		ClientSecretEncrypted:           pgtype.Text{},
		TokenEndpointAuthMethod:         conv.ToPGText("private_key_jwt"),
		TokenEndpointAuthAudienceFormat: conv.ToPGText("token_endpoint"),
		Scope:                           []string{"openid", "email", "profile", "offline_access"},
		Audience:                        pgtype.Text{},
		LegacyCallbackUrl:               pgtype.Bool{},
		ID:                              clientID,
		OrganizationID:                  conv.ToPGText(si.orgID),
	})
	require.NoError(t, err)
	setup, err = reader.Read(ctx, si.orgID)
	require.NoError(t, err)
	require.True(t, setup.ClientReady)
	require.Equal(t, idpc.SignInStepTrustSignIn, setup.NextStep)
	require.Empty(t, setup.TrustingIssuers)

	trustingID := createSignInUserSessionIssuer(t, ctx, si, "okta-sign-in", uuid.NullUUID{UUID: managed.IssuerID, Valid: true}, uuid.NullUUID{UUID: clientID, Valid: true})
	createSignInUserSessionIssuer(t, ctx, si, "unrelated", uuid.NullUUID{}, uuid.NullUUID{})

	setup, err = reader.Read(ctx, si.orgID)
	require.NoError(t, err)
	require.Equal(t, []idpc.SignInIssuer{{ID: trustingID.String(), Slug: "okta-sign-in"}}, setup.TrustingIssuers)
	require.Empty(t, setup.StaleTrustingIssuers)
	require.Equal(t, idpc.SignInStepRegisterInOkta, setup.NextStep)
	require.Equal(t, managed.ActiveKid, setup.ActiveKeyID)
	require.Equal(t, managed.ActiveKid, setup.PublicJWK["kid"])
	require.Equal(t, "sig", setup.PublicJWK["use"])
	for member := range setup.PublicJWK {
		require.Contains(t, []string{"kid", "kty", "alg", "use", "n", "e", "crv", "x", "y"}, member)
	}

	other, err := reader.Read(ctx, createOrganization(t, ctx, si.conn.conn))
	require.ErrorIs(t, err, idpc.ErrConnectionNotFound, "another organization sees none of this")
	require.Nil(t, other)
}

func TestSignInReader_IgnoresClientsOutsideTheConnectionIssuer(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)
	reader := idpc.NewSignInReader(testenv.NewLogger(t), si.conn.conn, remotesessions.DefaultCallbackOrigins(mustURL(t, "https://app.example.com")))

	created := createConnection(t, ctx, si, fullOrgURL)
	submitClientID(t, ctx, si, created.ID)
	_, err := si.svc.RecordAgent(ctx, &gen.RecordAgentPayload{SessionToken: nil, ID: created.ID, AgentID: conv.PtrEmpty(signInAgentID), AgentAppID: nil})
	require.NoError(t, err)

	otherIssuer := createIssuer(t, ctx, si.conn.conn, si.orgID, uuid.NullUUID{}, "https://other.example.com/token")
	createSignInClient(t, ctx, si, otherIssuer, nil)

	setup, err := reader.Read(ctx, si.orgID)
	require.NoError(t, err)
	require.False(t, setup.ClientRegistered)
	require.Equal(t, idpc.SignInStepSetUpSignIn, setup.NextStep)
}

type signInClientSpec struct {
	clientID  string
	method    string
	audience  string
	keySet    uuid.NullUUID
	scope     []string
	managedBy uuid.NullUUID
}

func createSignInClient(t *testing.T, ctx context.Context, si *serviceInstance, issuerID uuid.UUID, scope []string) uuid.UUID {
	t.Helper()
	return createSignInClientSpec(t, ctx, si, issuerID, signInClientSpec{clientID: signInAgentID, method: "client_secret_basic", scope: scope})
}

func createSignInClientSpec(t *testing.T, ctx context.Context, si *serviceInstance, issuerID uuid.UUID, spec signInClientSpec) uuid.UUID {
	t.Helper()
	client, err := remotesessionsrepo.New(si.conn.conn).CreateRemoteSessionClient(ctx, remotesessionsrepo.CreateRemoteSessionClientParams{
		ProjectID:                       uuid.NullUUID{},
		OrganizationID:                  conv.ToPGText(si.orgID),
		RemoteSessionIssuerID:           issuerID,
		ClientID:                        spec.clientID,
		ClientSecretEncrypted:           pgtype.Text{},
		ClientIDIssuedAt:                pgtype.Timestamptz{},
		ClientSecretExpiresAt:           pgtype.Timestamptz{},
		TokenEndpointAuthMethod:         conv.ToPGText(spec.method),
		TokenEndpointAuthAudienceFormat: conv.ToPGTextEmpty(spec.audience),
		Scope:                           spec.scope,
		Audience:                        pgtype.Text{},
		LegacyCallbackUrl:               false,
		JsonWebKeySetID:                 spec.keySet,
		IdentityProviderConnectionID:    spec.managedBy,
		CallbackBaseUrl:                 pgtype.Text{},
	})
	require.NoError(t, err)
	return client.ID
}

func createSignInUserSessionIssuer(t *testing.T, ctx context.Context, si *serviceInstance, slug string, trustedIssuerID, trustedClientID uuid.NullUUID) uuid.UUID {
	t.Helper()
	issuer, err := usersessionsrepo.New(si.conn.conn).CreateOrganizationUserSessionIssuer(ctx, usersessionsrepo.CreateOrganizationUserSessionIssuerParams{
		OrganizationID:               conv.ToPGText(si.orgID),
		Slug:                         slug,
		AuthnChallengeMode:           "interactive",
		SessionDuration:              pgtype.Interval{Microseconds: (24 * time.Hour).Microseconds(), Days: 0, Months: 0, Valid: true},
		TrustedRemoteSessionIssuerID: trustedIssuerID,
		TrustedRemoteSessionClientID: trustedClientID,
	})
	require.NoError(t, err)
	return issuer.ID
}

type signInFixture struct {
	si           *serviceInstance
	reader       *idpc.SignInReader
	connectionID string
	managed      *idpc.ManagedClient
}

func newSignInFixture(t *testing.T) (context.Context, signInFixture) {
	t.Helper()
	ctx, si := newTestService(t)
	reader := idpc.NewSignInReader(testenv.NewLogger(t), si.conn.conn, remotesessions.DefaultCallbackOrigins(mustURL(t, "https://app.example.com")))
	created := createConnection(t, ctx, si, fullOrgURL)
	submitClientID(t, ctx, si, created.ID)
	_, err := si.svc.RecordAgent(ctx, &gen.RecordAgentPayload{SessionToken: nil, ID: created.ID, AgentID: conv.PtrEmpty(signInAgentID), AgentAppID: nil})
	require.NoError(t, err)
	managed, err := si.provisioner.GetManagedClient(ctx, si.orgID, uuid.MustParse(created.ID))
	require.NoError(t, err)
	return ctx, signInFixture{si: si, reader: reader, connectionID: created.ID, managed: managed}
}

func (f signInFixture) readyClient() signInClientSpec {
	return signInClientSpec{
		clientID: signInAgentID,
		method:   "private_key_jwt",
		audience: "token_endpoint",
		keySet:   uuid.NullUUID{UUID: f.managed.JSONWebKeySetID.UUID, Valid: true},
		scope:    []string{"openid", "email", "profile", "offline_access"},
	}
}

func TestSignInReader_ReadinessNeedsEveryRequirement(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		vary  func(*signInClientSpec)
		ready bool
	}{
		{name: "ready", vary: func(*signInClientSpec) {}, ready: true},
		{name: "client secret instead of private_key_jwt", vary: func(c *signInClientSpec) { c.method = "client_secret_basic" }},
		{name: "issuer audience", vary: func(c *signInClientSpec) { c.audience = "issuer" }},
		{name: "default audience", vary: func(c *signInClientSpec) { c.audience = "" }},
		{name: "no key set", vary: func(c *signInClientSpec) { c.keySet = uuid.NullUUID{} }},
		{name: "missing offline_access", vary: func(c *signInClientSpec) { c.scope = []string{"openid", "email", "profile"} }},
		{name: "missing openid", vary: func(c *signInClientSpec) { c.scope = []string{"email", "profile", "offline_access"} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, f := newSignInFixture(t)
			spec := f.readyClient()
			tc.vary(&spec)
			createSignInClientSpec(t, ctx, f.si, f.managed.IssuerID, spec)

			setup, err := f.reader.Read(ctx, f.si.orgID)
			require.NoError(t, err)
			require.True(t, setup.ClientRegistered)
			require.Equal(t, tc.ready, setup.ClientReady)
			if tc.ready {
				require.Equal(t, idpc.SignInStepTrustSignIn, setup.NextStep)
			} else {
				require.Equal(t, idpc.SignInStepSetUpSignIn, setup.NextStep)
			}
		})
	}
}

func TestSignInReader_DuplicateClientsFailClosed(t *testing.T) {
	t.Parallel()
	ctx, f := newSignInFixture(t)
	first := createSignInClientSpec(t, ctx, f.si, f.managed.IssuerID, f.readyClient())
	createSignInClientSpec(t, ctx, f.si, f.managed.IssuerID, f.readyClient())
	createSignInUserSessionIssuer(t, ctx, f.si, "okta-sign-in", uuid.NullUUID{UUID: f.managed.IssuerID, Valid: true}, uuid.NullUUID{UUID: first, Valid: true})

	setup, err := f.reader.Read(ctx, f.si.orgID)
	require.NoError(t, err)
	require.Equal(t, 2, setup.DuplicateClients)
	require.Equal(t, idpc.SignInStepResolveDuplicates, setup.NextStep)
	require.False(t, setup.ClientReady)
	require.Empty(t, setup.RedirectURI)
	require.Empty(t, setup.JWKSURL)
	require.Nil(t, setup.PublicJWK)
	require.Empty(t, setup.TrustingIssuers)
	require.Empty(t, setup.StaleTrustingIssuers)
}

func TestSignInReader_IgnoresManagedClients(t *testing.T) {
	t.Parallel()
	ctx, f := newSignInFixture(t)
	decoy := f.readyClient()
	decoy.keySet = uuid.NullUUID{}
	decoy.managedBy = uuid.NullUUID{UUID: uuid.MustParse(f.connectionID), Valid: true}
	createSignInClientSpec(t, ctx, f.si, f.managed.IssuerID, decoy)

	setup, err := f.reader.Read(ctx, f.si.orgID)
	require.NoError(t, err)
	require.False(t, setup.ClientRegistered, "a connection-managed client is never the sign-in client")
	require.Zero(t, setup.DuplicateClients)
	require.Equal(t, idpc.SignInStepSetUpSignIn, setup.NextStep)
}

func TestSignInReader_ReportsIssuersTrustingAPreviousAgent(t *testing.T) {
	t.Parallel()
	ctx, f := newSignInFixture(t)
	previous := createSignInClientSpec(t, ctx, f.si, f.managed.IssuerID, f.readyClient())
	stale := createSignInUserSessionIssuer(t, ctx, f.si, "okta-sign-in", uuid.NullUUID{UUID: f.managed.IssuerID, Valid: true}, uuid.NullUUID{UUID: previous, Valid: true})

	const newAgentID = "wlpagent000000000002"
	_, err := f.si.svc.RecordAgent(ctx, &gen.RecordAgentPayload{SessionToken: nil, ID: f.connectionID, AgentID: conv.PtrEmpty(newAgentID), AgentAppID: nil})
	require.NoError(t, err)
	current := f.readyClient()
	current.clientID = newAgentID
	createSignInClientSpec(t, ctx, f.si, f.managed.IssuerID, current)

	setup, err := f.reader.Read(ctx, f.si.orgID)
	require.NoError(t, err)
	require.True(t, setup.ClientReady)
	require.Empty(t, setup.TrustingIssuers)
	require.Equal(t, []idpc.SignInIssuer{{ID: stale.String(), Slug: "okta-sign-in"}}, setup.StaleTrustingIssuers)
	require.Equal(t, idpc.SignInStepTrustSignIn, setup.NextStep)
}

func TestSignInReader_ListsEveryTrustingIssuer(t *testing.T) {
	t.Parallel()
	ctx, f := newSignInFixture(t)
	client := createSignInClientSpec(t, ctx, f.si, f.managed.IssuerID, f.readyClient())
	const count = 55
	for i := range count {
		createSignInUserSessionIssuer(t, ctx, f.si, fmt.Sprintf("okta-sign-in-%02d", i), uuid.NullUUID{UUID: f.managed.IssuerID, Valid: true}, uuid.NullUUID{UUID: client, Valid: true})
	}

	setup, err := f.reader.Read(ctx, f.si.orgID)
	require.NoError(t, err)
	require.Len(t, setup.TrustingIssuers, count)
}

func TestSignInScopesMatchTheDashboard(t *testing.T) {
	t.Parallel()
	source, err := os.ReadFile(filepath.Join("..", "..", "..", "client", "dashboard", "src", "pages", "org", "identity-provider", "tabs", "setup", "oktaSignIn.ts"))
	require.NoError(t, err)
	match := regexp.MustCompile(`const SIGN_IN_SCOPES = \[([^\]]*)\]`).FindSubmatch(source)
	require.NotNil(t, match, "the dashboard declares SIGN_IN_SCOPES")
	dashboard := []string{}
	for _, scope := range regexp.MustCompile(`"([^"]+)"`).FindAllSubmatch(match[1], -1) {
		dashboard = append(dashboard, string(scope[1]))
	}
	require.Equal(t, idpc.SignInScopes, dashboard)
}
