package xaareadiness

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/xaareadiness/repo"
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
	server := repo.ListEligibleServersRow{IssuerID: issuer, ProjectID: project, UserSessionIssuerID: uuid.NullUUID{UUID: login, Valid: true}}
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
	id, scopes, state := resolveClient(server, "https://mcp.example", []repo.ListIssuerClientsRow{unrelated, client}, nil)
	require.Equal(t, "attached", id)
	require.Equal(t, ClientBindingSingle, state)
	require.Equal(t, []string{"read", "openid", "offline_access"}, scopes)
	_, _, state = resolveClient(server, "https://mcp.example", []repo.ListIssuerClientsRow{unrelated}, nil)
	require.Equal(t, ClientBindingMissing, state)

	client.IssuerScopeOverride = []string{"pinned"}
	_, scopes, _ = resolveClient(server, "https://mcp.example", []repo.ListIssuerClientsRow{client}, nil)
	require.Equal(t, []string{"pinned"}, scopes)
	client.IssuerScopeOverride = nil
	client.Scope = nil
	_, scopes, _ = resolveClient(server, "https://mcp.example", []repo.ListIssuerClientsRow{client}, nil)
	require.Equal(t, client.IssuerScopesSupported, scopes)

	// Resource-matched clients use the same trailing-slash normalization as runtime.
	server.UserSessionIssuerID = uuid.NullUUID{}
	id, _, state = resolveClient(server, "https://mcp.example", []repo.ListIssuerClientsRow{client, unrelated}, nil)
	require.Equal(t, "unrelated", id)
	require.Equal(t, ClientBindingSingle, state)
}

func TestResolveClientBindingsRespectLoginIssuer(t *testing.T) {
	t.Parallel()
	issuer, project, login, otherLogin := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	server := repo.ListEligibleServersRow{IssuerID: issuer, ProjectID: project, UserSessionIssuerID: uuid.NullUUID{UUID: login, Valid: true}}
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
		id, scopes, state := resolveClient(server, binding.Resource, clients, bindings)
		require.Equal(t, "attached", id)
		require.Equal(t, ClientBindingBound, state)
		require.Equal(t, []string{"read"}, scopes)
	}
	// Even a foreign binding to the same client must not contribute scopes.
	foreign.RemoteSessionClientID.UUID = client.ID
	_, scopes, state := resolveClient(server, binding.Resource, clients, []repo.ListEMABindingsRow{foreign, binding})
	require.Equal(t, ClientBindingBound, state)
	require.Equal(t, []string{"read"}, scopes)

	// With only foreign bindings, use the server's attached fallback client.
	id, scopes, state := resolveClient(server, binding.Resource, clients, []repo.ListEMABindingsRow{foreign})
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
		id, scopes, state := resolveClient(server, "https://mcp.example", []repo.ListIssuerClientsRow{client}, bindings)
		require.Equal(t, "bound", id)
		require.Equal(t, ClientBindingBound, state)
		require.Equal(t, []string{"openid", "read", "write"}, scopes)
	}
}
