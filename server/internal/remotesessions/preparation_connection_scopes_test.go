package remotesessions_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	clientsgen "github.com/speakeasy-api/gram/server/gen/remote_session_clients"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oauthwire"
	remotemcprepo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/remotesessionmetrics"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

// connectedPreparation links the fixture's client to its user session issuer
// and seeds a remote-backed MCP server at serverURL whose resource row pins
// pin, so Turn on has a server connection to follow.
func connectedPreparation(t *testing.T, ti *testInstance, ctx context.Context, in remotesessions.PreparationInput, serverURL string, pin []string) {
	t.Helper()
	auth, _ := contextvalues.GetAuthContext(ctx)
	require.NoError(t, repo.New(ti.conn).AttachRemoteSessionClientToUserSessionIssuer(ctx, repo.AttachRemoteSessionClientToUserSessionIssuerParams{RemoteSessionClientID: in.ClientID, UserSessionIssuerID: in.UserSessionIssuerID}))
	seedRemoteMcpServerForLogin(t, ctx, ti.conn, *auth.ProjectID, in.UserSessionIssuerID, "connected-"+uuid.NewString()[:8], serverURL)
	seedProtectedResource(t, ctx, ti.conn, *auth.ProjectID, auth.ActiveOrganizationID, serverURL, protectedResourceSeed{pin: pin})
}

func setPin(t *testing.T, ti *testInstance, ctx context.Context, resourceURL string, pin []string) {
	t.Helper()
	auth, _ := contextvalues.GetAuthContext(ctx)
	_, err := remotemcprepo.New(ti.conn).UpsertRemoteProtectedResourceScopeOverride(ctx, remotemcprepo.UpsertRemoteProtectedResourceScopeOverrideParams{ScopeOverride: pin, ProjectID: *auth.ProjectID, OrganizationID: auth.ActiveOrganizationID, ResourceIdentifier: resourceURL})
	require.NoError(t, err)
}

func readEMA(t *testing.T, ti *testInstance, ctx context.Context, in remotesessions.PreparationInput) *clientsgen.IdentityChainingPreparation {
	t.Helper()
	read, err := ti.service.ReadEMA(ctx, &clientsgen.ReadEMAPayload{UserSessionIssuerID: in.UserSessionIssuerID.String(), RemoteSessionIssuerID: in.RemoteSessionIssuerID.String(), Resource: in.Resource})
	require.NoError(t, err)
	return read
}

func TestPreparationScopes_TurnOnSnapshotsTheConnection(t *testing.T) {
	t.Parallel()
	ctx, ti, in := preparationFixture(t)
	in.ConfirmGrants = []string{oauthwire.GrantTypeJWTBearer}
	in.Scopes = []string{"ignored:scope"}
	connectedPreparation(t, ti, ctx, in, in.Resource, []string{"files:read", "openid"})

	prepared, err := ti.service.PrepareIdentityChaining(ctx, in)
	require.NoError(t, err)
	require.Equal(t, "ready", prepared.State)
	require.Equal(t, []string{"files:read"}, prepared.Scopes, "the connection's scopes, without OIDC scopes, not the request's")

	read := readEMA(t, ti, ctx, in)
	require.Equal(t, []string{"files:read"}, read.Scopes)
	require.Equal(t, []string{"files:read"}, read.ConnectionScopes)
	require.False(t, *read.ScopesOutOfDate)
}

func TestPreparationScopes_UnchangedTurnOnIsANoOp(t *testing.T) {
	t.Parallel()
	ctx, ti, in := preparationFixture(t)
	in.ConfirmGrants = []string{oauthwire.GrantTypeJWTBearer}
	connectedPreparation(t, ti, ctx, in, in.Resource, []string{"files:read"})
	prepared, err := ti.service.PrepareIdentityChaining(ctx, in)
	require.NoError(t, err)

	before := preparationAuditCounts(t, ctx, ti)
	in.Scopes = []string{"another:ignored"}
	in.ExpectedGeneration = prepared.Generation
	again, err := ti.service.PrepareIdentityChaining(ctx, in)
	require.NoError(t, err)
	require.Equal(t, "ready", again.State)
	require.Equal(t, prepared.Generation, again.Generation, "an ignored request scope must not bump the generation")
	require.Equal(t, []string{"files:read"}, again.Scopes)
	requirePreparationAuditDelta(t, before, preparationAuditCounts(t, ctx, ti), 0, 0, 0)
}

func TestPreparationScopes_ConnectionChangeAppliesOnlyOnTurnOn(t *testing.T) {
	t.Parallel()
	ctx, ti, in := preparationFixture(t)
	in.ConfirmGrants = []string{oauthwire.GrantTypeJWTBearer}
	connectedPreparation(t, ti, ctx, in, in.Resource, []string{"files:read"})
	prepared, err := ti.service.PrepareIdentityChaining(ctx, in)
	require.NoError(t, err)

	setPin(t, ti, ctx, in.Resource, []string{"files:write"})
	read := readEMA(t, ti, ctx, in)
	require.Equal(t, []string{"files:read"}, read.Scopes, "the recorded scopes stand until Turn on")
	require.Equal(t, []string{"files:write"}, read.ConnectionScopes)
	require.True(t, *read.ScopesOutOfDate)
	auth, _ := contextvalues.GetAuthContext(ctx)
	runtime, err := remotesessions.ReadIdentityChainingForTenant(ctx, ti.conn, *auth.ProjectID, auth.ActiveOrganizationID, in)
	require.NoError(t, err)
	require.Equal(t, []string{"files:read"}, runtime.Scopes, "the runtime's read requests the recorded scopes")
	require.Nil(t, runtime.ConnectionScopes, "the runtime's read never resolves the connection")

	before := preparationAuditCounts(t, ctx, ti)
	in.ExpectedGeneration = prepared.Generation
	again, err := ti.service.PrepareIdentityChaining(ctx, in)
	require.NoError(t, err)
	require.Equal(t, []string{"files:write"}, again.Scopes)
	require.Greater(t, again.Generation, prepared.Generation)
	requirePreparationAuditDelta(t, before, preparationAuditCounts(t, ctx, ti), 0, 1, 0)
	require.False(t, *readEMA(t, ti, ctx, in).ScopesOutOfDate)
}

func TestPreparationScopes_ServerFoundByItsDocumentResource(t *testing.T) {
	t.Parallel()
	ctx, ti, in := preparationFixture(t)
	in.ConfirmGrants = []string{oauthwire.GrantTypeJWTBearer}
	auth, _ := contextvalues.GetAuthContext(ctx)
	serverURL := "https://mcp.example.com/v1/mcp"
	connectedPreparation(t, ti, ctx, in, serverURL, []string{"files:read"})
	_, err := remotemcprepo.New(ti.conn).UpsertRemoteProtectedResource(ctx, remotemcprepo.UpsertRemoteProtectedResourceParams{
		ProjectID: *auth.ProjectID, OrganizationID: auth.ActiveOrganizationID, ResourceIdentifier: serverURL,
		MetadataUrl: "", AuthorizationServers: []string{syntheticIssuerURL}, ScopesSupported: nil, BearerMethodsSupported: nil,
		ResourceName: "", ResourceDocumentation: "", ResourcePolicyUri: "", ResourceTosUri: "",
		Metadata: `{"resource":"` + in.Resource + `"}`,
	})
	require.NoError(t, err)

	prepared, err := ti.service.PrepareIdentityChaining(ctx, in)
	require.NoError(t, err)
	require.Equal(t, []string{"files:read"}, prepared.Scopes, "the row is keyed by the server URL even though the binding names the RFC 9728 resource")
}

func TestPreparationScopes_UnlinkedClientRecordsTheRequest(t *testing.T) {
	t.Parallel()
	ctx, ti, in := preparationFixture(t)
	in.ConfirmGrants = []string{oauthwire.GrantTypeJWTBearer}
	in.Scopes = []string{"openid", "files:read"}
	auth, _ := contextvalues.GetAuthContext(ctx)
	seedRemoteMcpServerForLogin(t, ctx, ti.conn, *auth.ProjectID, in.UserSessionIssuerID, "unlinked", in.Resource)
	seedProtectedResource(t, ctx, ti.conn, *auth.ProjectID, auth.ActiveOrganizationID, in.Resource, protectedResourceSeed{pin: []string{"files:write"}})

	prepared, err := ti.service.PrepareIdentityChaining(ctx, in)
	require.NoError(t, err)
	require.Equal(t, "ready", prepared.State)
	require.Equal(t, []string{"files:read"}, prepared.Scopes, "a client no sign-in uses has no connection to follow")
	read := readEMA(t, ti, ctx, in)
	require.Equal(t, []string{"files:read"}, read.ConnectionScopes)
	require.False(t, *read.ScopesOutOfDate)
}

func TestPreparationScopes_DiscoveryOffIgnoresTheResourceRow(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestServiceWithConfig(t, testServiceConfig{noProber: true})
	issuer := uuid.MustParse(createRemoteIssuer(t, ctx, ti, "preparation", ""))
	user := createUserSessionIssuer(t, ctx, ti.conn, "preparation-human")
	preparationAdvertise(t, ctx, ti, issuer)
	auth, _ := contextvalues.GetAuthContext(ctx)
	client := preparationManualClient(t, ctx, ti, *auth.ProjectID, issuer, "downstream-client")
	in := remotesessions.PreparationInput{UserSessionIssuerID: user, RemoteSessionIssuerID: issuer, ClientID: client, Resource: "https://resource.example.com/", Mechanism: "manual", Scopes: []string{"requested:scope"}, ConfirmGrants: []string{oauthwire.GrantTypeJWTBearer}}
	connectedPreparation(t, ti, ctx, in, in.Resource, []string{"files:read"})

	prepared, err := ti.service.PrepareIdentityChaining(ctx, in)
	require.NoError(t, err)
	require.Equal(t, []string{"requested:scope"}, prepared.Scopes, "without discovery the pin does not decide, and a connection naming nothing falls back to the request")
}

func TestConnectionScopes_DiscoverFlagGatesTheResourceRow(t *testing.T) {
	t.Parallel()
	ctx, ti, in := preparationFixture(t)
	connectedPreparation(t, ti, ctx, in, in.Resource, []string{"files:read"})
	auth, _ := contextvalues.GetAuthContext(ctx)
	b := remotesessions.BindingConnection{ProjectID: *auth.ProjectID, OrganizationID: auth.ActiveOrganizationID, UserSessionIssuerID: in.UserSessionIssuerID, ClientID: in.ClientID, Resource: in.Resource, Discover: true}

	on, err := remotesessions.ConnectionScopes(ctx, ti.conn, b)
	require.NoError(t, err)
	require.NotNil(t, on)
	require.Equal(t, []string{"files:read"}, on.Scopes)

	b.Discover = false
	off, err := remotesessions.ConnectionScopes(ctx, ti.conn, b)
	require.NoError(t, err)
	require.NotNil(t, off)
	require.Equal(t, remotesessionmetrics.ScopeSourceIssuerCatalogue, off.Source, "without discovery the pin does not decide")
	require.NotContains(t, off.Scopes, "files:read")
}
