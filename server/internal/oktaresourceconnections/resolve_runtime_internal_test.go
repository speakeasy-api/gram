package oktaresourceconnections

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/oktaresourceconnections/repo"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
)

func TestResourceIndicatorRuntimeNormalization(t *testing.T) {
	t.Parallel()
	text := func(s string) pgtype.Text { return pgtype.Text{String: s, Valid: true} }
	for _, tt := range []struct {
		name   string
		server repo.ListEligibleServersRow
		want   string
	}{
		{name: "tunneled", server: repo.ListEligibleServersRow{TunneledResourceIdentifier: text("https://tunneled.example/mcp///")}, want: "https://tunneled.example/mcp"},
		{name: "remote", server: repo.ListEligibleServersRow{RemoteUrl: text("https://remote.example/mcp///")}, want: "https://remote.example/mcp"},
		{name: "unproxied", server: repo.ListEligibleServersRow{UnproxiedUrl: text("https://unproxied.example/mcp///")}, want: "https://unproxied.example/mcp"},
		{name: "tunneled wins over remote and unproxied", server: repo.ListEligibleServersRow{TunneledResourceIdentifier: text("https://tunneled.example/mcp"), RemoteUrl: text("https://remote.example/mcp"), UnproxiedUrl: text("https://unproxied.example/mcp")}, want: "https://tunneled.example/mcp"},
		{name: "remote wins over unproxied", server: repo.ListEligibleServersRow{RemoteUrl: text("https://remote.example/mcp"), UnproxiedUrl: text("https://unproxied.example/mcp")}, want: "https://remote.example/mcp"},
		{name: "empty tunneled identifier falls through", server: repo.ListEligibleServersRow{TunneledResourceIdentifier: text(""), RemoteUrl: text("https://remote.example/mcp")}, want: "https://remote.example/mcp"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, resourceIndicator(tt.server))
		})
	}
}

func TestResolveClientRuntimeScopesAndAttachments(t *testing.T) {
	t.Parallel()
	issuer, project, login, otherLogin := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	server := repo.ListEligibleServersRow{IssuerID: issuer, ProjectID: project, UserSessionIssuerID: uuid.NullUUID{UUID: login, Valid: true}, RemoteUrl: pgtype.Text{String: "https://mcp.example", Valid: true}}
	client := repo.ListIssuerClientsRow{
		ID: uuid.New(), RemoteSessionIssuerID: issuer, ClientID: "attached",
		Scope: []string{"read"}, IssuerScopesSupported: []string{"read", "write", "openid", "offline_access"},
		UserSessionIssuerIds: []uuid.UUID{login},
	}
	unrelated := client
	unrelated.ID = uuid.New()
	unrelated.ClientID = "unrelated"
	unrelated.UserSessionIssuerIds = []uuid.UUID{otherLogin}
	unrelated.ResourceIdentifier = pgtype.Text{String: "https://mcp.example/", Valid: true}
	id, scopes, state := resolveClient(server, "https://mcp.example", "", []repo.ListIssuerClientsRow{unrelated, client}, nil, allClients(remotesessions.ResourceScopes{}))
	require.Equal(t, "attached", id)
	require.Equal(t, ClientBindingSingle, state)
	require.Equal(t, []string{"read"}, scopes)
	_, _, state = resolveClient(server, "https://mcp.example", "", []repo.ListIssuerClientsRow{unrelated}, nil, allClients(remotesessions.ResourceScopes{}))
	require.Equal(t, ClientBindingMissing, state)

	// With discovery on the client's own scope beats the issuer override;
	// off (no prober), the override still wins.
	client.IssuerScopeOverride = []string{"pinned"}
	_, scopes, _ = resolveClient(server, "https://mcp.example", "", []repo.ListIssuerClientsRow{client}, nil, allClients(remotesessions.ResourceScopes{UseDiscovered: true}))
	require.Equal(t, []string{"read"}, scopes)
	_, scopes, _ = resolveClient(server, "https://mcp.example", "", []repo.ListIssuerClientsRow{client}, nil, allClients(remotesessions.ResourceScopes{}))
	require.Equal(t, []string{"pinned"}, scopes)
	client.Scope = nil
	_, scopes, _ = resolveClient(server, "https://mcp.example", "", []repo.ListIssuerClientsRow{client}, nil, allClients(remotesessions.ResourceScopes{}))
	require.Equal(t, []string{"pinned"}, scopes)
	client.IssuerScopeOverride = nil
	_, scopes, _ = resolveClient(server, "https://mcp.example", "", []repo.ListIssuerClientsRow{client}, nil, allClients(remotesessions.ResourceScopes{}))
	require.Equal(t, []string{"read", "write"}, scopes, "identity chaining never requests OIDC-reserved scopes")

	// Resource-matched clients use the same trailing-slash normalization as runtime.
	server.UserSessionIssuerID = uuid.NullUUID{}
	id, _, state = resolveClient(server, "https://mcp.example", "", []repo.ListIssuerClientsRow{client, unrelated}, nil, allClients(remotesessions.ResourceScopes{}))
	require.Equal(t, "unrelated", id)
	require.Equal(t, ClientBindingSingle, state)
}

func TestResolveClientBindingsRespectLoginIssuer(t *testing.T) {
	t.Parallel()
	issuer, project, login, otherLogin := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	server := repo.ListEligibleServersRow{IssuerID: issuer, ProjectID: project, UserSessionIssuerID: uuid.NullUUID{UUID: login, Valid: true}, RemoteUrl: pgtype.Text{String: "https://mcp.example", Valid: true}}
	client := repo.ListIssuerClientsRow{ID: uuid.New(), RemoteSessionIssuerID: issuer, ClientID: "attached", Scope: []string{"fallback"}, UserSessionIssuerIds: []uuid.UUID{login}}
	other := repo.ListIssuerClientsRow{ID: uuid.New(), RemoteSessionIssuerID: issuer, ClientID: "other", UserSessionIssuerIds: []uuid.UUID{otherLogin}}
	clients := []repo.ListIssuerClientsRow{client, other}
	binding := repo.ListEMABindingsRow{ProjectID: project, UserSessionIssuerID: login, RemoteSessionIssuerID: issuer, Resource: "https://mcp.example", RemoteSessionClientID: uuid.NullUUID{UUID: client.ID, Valid: true}, RequestedScopes: []string{"read"}}
	foreign := binding
	foreign.UserSessionIssuerID = otherLogin
	foreign.RemoteSessionClientID.UUID = other.ID
	foreign.RequestedScopes = []string{"write"}

	// Another login's binding must neither win nor create false ambiguity.
	for _, bindings := range [][]repo.ListEMABindingsRow{{foreign, binding}, {binding, foreign}} {
		id, scopes, state := resolveClient(server, binding.Resource, "", clients, bindings, allClients(remotesessions.ResourceScopes{}))
		require.Equal(t, "attached", id)
		require.Equal(t, ClientBindingBound, state)
		require.Equal(t, []string{"read"}, scopes)
	}
	// Even a foreign binding to the same client must not contribute scopes.
	foreign.RemoteSessionClientID.UUID = client.ID
	_, scopes, state := resolveClient(server, binding.Resource, "", clients, []repo.ListEMABindingsRow{foreign, binding}, allClients(remotesessions.ResourceScopes{}))
	require.Equal(t, ClientBindingBound, state)
	require.Equal(t, []string{"read"}, scopes)

	// With only foreign bindings, use the server's attached fallback client.
	id, scopes, state := resolveClient(server, binding.Resource, "", clients, []repo.ListEMABindingsRow{foreign}, allClients(remotesessions.ResourceScopes{}))
	require.Equal(t, "attached", id)
	require.Equal(t, ClientBindingSingle, state)
	require.Equal(t, []string{"fallback"}, scopes)
}

func TestResolveClientAggregatesEffectiveBindingScopes(t *testing.T) {
	t.Parallel()
	issuer, project, clientID := uuid.New(), uuid.New(), uuid.New()
	server := repo.ListEligibleServersRow{IssuerID: issuer, ProjectID: project}
	client := repo.ListIssuerClientsRow{ID: clientID, RemoteSessionIssuerID: issuer, ClientID: "bound", Scope: []string{"read"}, IssuerScopesSupported: []string{"openid"}}
	binding := repo.ListEMABindingsRow{ProjectID: project, RemoteSessionIssuerID: issuer, Resource: "https://mcp.example/", RemoteSessionClientID: uuid.NullUUID{UUID: clientID, Valid: true}, RequestedScopes: []string{"write", "read"}}
	fallback := binding
	fallback.RequestedScopes = nil
	fallback.Resource = "https://mcp.example"
	for _, bindings := range [][]repo.ListEMABindingsRow{{binding, fallback}, {fallback, binding}} {
		id, scopes, state := resolveClient(server, "https://mcp.example", "", []repo.ListIssuerClientsRow{client}, bindings, allClients(remotesessions.ResourceScopes{}))
		require.Equal(t, "bound", id)
		require.Equal(t, ClientBindingBound, state)
		require.Equal(t, []string{"read", "write"}, scopes)
	}
}

func TestResolveClientIgnoresBindingsToAnotherProjectsClient(t *testing.T) {
	t.Parallel()
	issuer, project, otherProject := uuid.New(), uuid.New(), uuid.New()
	server := repo.ListEligibleServersRow{IssuerID: issuer, ProjectID: project}
	foreign := repo.ListIssuerClientsRow{ID: uuid.New(), RemoteSessionIssuerID: issuer, ProjectID: uuid.NullUUID{UUID: otherProject, Valid: true}, ClientID: "other-project"}
	binding := repo.ListEMABindingsRow{ProjectID: project, RemoteSessionIssuerID: issuer, Resource: "https://mcp.example", RemoteSessionClientID: uuid.NullUUID{UUID: foreign.ID, Valid: true}, RequestedScopes: []string{"read"}}

	id, scopes, state := resolveClient(server, binding.Resource, "", []repo.ListIssuerClientsRow{foreign}, []repo.ListEMABindingsRow{binding}, allClients(remotesessions.ResourceScopes{}))
	require.Empty(t, id)
	require.Empty(t, scopes)
	require.Equal(t, ClientBindingMissing, state)
}

func TestResolveClientReportsRecordedScopesForABinding(t *testing.T) {
	t.Parallel()
	issuer, project, clientID, login := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	server := repo.ListEligibleServersRow{IssuerID: issuer, ProjectID: project, UserSessionIssuerID: uuid.NullUUID{UUID: login, Valid: true}, RemoteUrl: pgtype.Text{String: "https://mcp.example", Valid: true}}
	client := repo.ListIssuerClientsRow{ID: clientID, RemoteSessionIssuerID: issuer, ClientID: "bound", Scope: []string{"admin", "read"}, UserSessionIssuerIds: []uuid.UUID{login}}
	binding := repo.ListEMABindingsRow{ProjectID: project, UserSessionIssuerID: login, RemoteSessionIssuerID: issuer, Resource: "https://mcp.example", RemoteSessionClientID: uuid.NullUUID{UUID: clientID, Valid: true}, RequestedScopes: []string{"openid", "read"}}
	_, scopes, state := resolveClient(server, "https://mcp.example", "", []repo.ListIssuerClientsRow{client}, []repo.ListEMABindingsRow{binding}, allClients(remotesessions.ResourceScopes{}))
	require.Equal(t, ClientBindingBound, state)
	require.Equal(t, []string{"read"}, scopes, "a binding requests the scopes recorded at Turn on, not the connection's current set")

	binding.RequestedScopes = nil
	_, scopes, _ = resolveClient(server, "https://mcp.example", "", []repo.ListIssuerClientsRow{client}, []repo.ListEMABindingsRow{binding}, allClients(remotesessions.ResourceScopes{}))
	require.Empty(t, scopes, "a binding that recorded no scope requests none")
}

func TestResolveClientWithoutARemoteURLHasNoConnectionScopes(t *testing.T) {
	t.Parallel()
	issuer, project, login := uuid.New(), uuid.New(), uuid.New()
	server := repo.ListEligibleServersRow{IssuerID: issuer, ProjectID: project, UserSessionIssuerID: uuid.NullUUID{UUID: login, Valid: true}, TunneledResourceIdentifier: pgtype.Text{String: "https://tunneled.example/mcp", Valid: true}}
	client := repo.ListIssuerClientsRow{ID: uuid.New(), RemoteSessionIssuerID: issuer, ClientID: "attached", Scope: []string{"read"}, UserSessionIssuerIds: []uuid.UUID{login}}
	id, scopes, state := resolveClient(server, "https://tunneled.example/mcp", "", []repo.ListIssuerClientsRow{client}, nil, allClients(remotesessions.ResourceScopes{}))
	require.Equal(t, "attached", id)
	require.Equal(t, ClientBindingSingle, state)
	require.Empty(t, scopes, "Turn on keys no connection for a server that is not remote-backed, so it records the request's scopes")
}

func TestResolveClientTunneledResourceFollowsConnectionOnlyWhenItServesIt(t *testing.T) {
	t.Parallel()
	issuer, project, login := uuid.New(), uuid.New(), uuid.New()
	server := repo.ListEligibleServersRow{
		IssuerID: issuer, ProjectID: project, UserSessionIssuerID: uuid.NullUUID{UUID: login, Valid: true},
		TunneledResourceIdentifier: pgtype.Text{String: "https://tunneled.example/mcp", Valid: true},
		RemoteUrl:                  pgtype.Text{String: "https://mcp.example", Valid: true},
	}
	client := repo.ListIssuerClientsRow{ID: uuid.New(), RemoteSessionIssuerID: issuer, ClientID: "attached", Scope: []string{"read"}, UserSessionIssuerIds: []uuid.UUID{login}}

	_, scopes, state := resolveClient(server, "https://tunneled.example/mcp", "", []repo.ListIssuerClientsRow{client}, nil, allClients(remotesessions.ResourceScopes{}))
	require.Equal(t, ClientBindingSingle, state)
	require.Empty(t, scopes, "a tunneled resource the remote URL does not serve has no connection to follow")

	_, scopes, _ = resolveClient(server, "https://tunneled.example/mcp", "https://tunneled.example/mcp/", []repo.ListIssuerClientsRow{client}, nil, allClients(remotesessions.ResourceScopes{}))
	require.Empty(t, scopes, "the document resource must name the binding resource exactly")

	_, scopes, _ = resolveClient(server, "https://tunneled.example/mcp", "https://tunneled.example/mcp", []repo.ListIssuerClientsRow{client}, nil, allClients(remotesessions.ResourceScopes{}))
	require.Equal(t, []string{"read"}, scopes, "a cached document naming the resource selects the connection")
}
