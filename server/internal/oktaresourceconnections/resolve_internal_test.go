package oktaresourceconnections

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/oktaresourceconnections/repo"
)

func TestResolveClient(t *testing.T) {
	t.Parallel()

	issuer := uuid.New()
	project := uuid.New()
	other := uuid.New()
	server := repo.ListEligibleServersRow{
		ID:                                  uuid.New(),
		ProjectID:                           project,
		ProjectSlug:                         "p",
		Name:                                pgtype.Text{String: "s", Valid: true},
		Slug:                                pgtype.Text{String: "s", Valid: true},
		IssuerID:                            issuer,
		MetadataFetchedAt:                   pgtype.Timestamptz{},
		GrantTypesSupported:                 nil,
		AuthorizationGrantProfilesSupported: nil,
		TunneledResourceIdentifier:          pgtype.Text{},
		RemoteUrl:                           pgtype.Text{String: "https://mcp.example/", Valid: true},
		UnproxiedUrl:                        pgtype.Text{},
	}
	client := func(id uuid.UUID, projectID uuid.NullUUID, clientID string, resource string) repo.ListIssuerClientsRow {
		return repo.ListIssuerClientsRow{ID: id, RemoteSessionIssuerID: issuer, ProjectID: projectID, ClientID: clientID, Scope: []string{"read"}, ResourceIdentifier: pgtype.Text{String: resource, Valid: resource != ""}}
	}
	binding := func(clientID uuid.UUID, scopes ...string) repo.ListEMABindingsRow {
		return repo.ListEMABindingsRow{ProjectID: project, RemoteSessionIssuerID: issuer, Resource: "https://mcp.example/", RemoteSessionClientID: uuid.NullUUID{UUID: clientID, Valid: true}, RequestedScopes: scopes}
	}
	a, b := uuid.New(), uuid.New()
	inProject := uuid.NullUUID{UUID: project, Valid: true}
	orgWide := uuid.NullUUID{}

	id, scopes, state := resolveClient(server, "https://mcp.example/", nil, nil)
	require.Empty(t, id)
	require.Empty(t, scopes)
	require.Equal(t, ClientBindingMissing, state)

	id, _, state = resolveClient(server, "https://mcp.example/", []repo.ListIssuerClientsRow{client(a, inProject, "one", "")}, nil)
	require.Equal(t, "one", id)
	require.Equal(t, ClientBindingSingle, state)

	// A client in another project does not count; an org-level one does.
	id, _, state = resolveClient(server, "https://mcp.example/", []repo.ListIssuerClientsRow{client(a, uuid.NullUUID{UUID: other, Valid: true}, "elsewhere", ""), client(b, orgWide, "org", "")}, nil)
	require.Equal(t, "org", id)
	require.Equal(t, ClientBindingSingle, state)

	two := []repo.ListIssuerClientsRow{client(a, inProject, "one", ""), client(b, orgWide, "org", "")}
	_, _, state = resolveClient(server, "https://mcp.example/", two, nil)
	require.Equal(t, ClientBindingAmbiguous, state)

	// A client registered for this resource narrows the candidates.
	id, _, state = resolveClient(server, "https://mcp.example/", []repo.ListIssuerClientsRow{client(a, inProject, "one", "https://mcp.example/"), client(b, orgWide, "org", "")}, nil)
	require.Equal(t, "one", id)
	require.Equal(t, ClientBindingSingle, state)

	// An explicit binding for the resource wins over ambiguity and carries its scopes.
	id, scopes, state = resolveClient(server, "https://mcp.example/", two, []repo.ListEMABindingsRow{binding(b, "files:read")})
	require.Equal(t, "org", id)
	require.Equal(t, []string{"files:read"}, scopes)
	require.Equal(t, ClientBindingBound, state)

	// Bindings that agree stay bound; bindings that disagree are ambiguous.
	_, _, state = resolveClient(server, "https://mcp.example/", two, []repo.ListEMABindingsRow{binding(b), binding(b, "x")})
	require.Equal(t, ClientBindingBound, state)
	_, _, state = resolveClient(server, "https://mcp.example/", two, []repo.ListEMABindingsRow{binding(a), binding(b)})
	require.Equal(t, ClientBindingAmbiguous, state)

	// A binding to a client outside the organization is ignored.
	id, _, state = resolveClient(server, "https://mcp.example/", []repo.ListIssuerClientsRow{client(a, inProject, "one", "")}, []repo.ListEMABindingsRow{binding(uuid.New())})
	require.Equal(t, "one", id)
	require.Equal(t, ClientBindingSingle, state)
}

func TestDeepLink(t *testing.T) {
	t.Parallel()

	agent := pgtype.Text{String: "wlp1", Valid: true}
	link := func(orgURL string) string {
		return deepLink(repo.GetLiveConnectionRow{ID: uuid.Nil, Status: "verified", OrgUrl: orgURL, AgentID: agent})
	}
	require.Empty(t, deepLink(repo.GetLiveConnectionRow{ID: uuid.Nil, Status: "verified", OrgUrl: "https://tenant.okta.com", AgentID: pgtype.Text{}}))
	require.Equal(t, "https://tenant-admin.okta.com/admin/workload-principals/ai-agents/wlp1/resource-connections/create", link("https://tenant.okta.com"))
	require.Equal(t, "https://tenant-admin.okta.com/admin/workload-principals/ai-agents/wlp1/resource-connections/create", link("https://Tenant-Admin.okta.com/"))
	require.Equal(t, "https://tenant-admin.oktapreview.com/admin/workload-principals/ai-agents/wlp1/resource-connections/create", link("https://tenant.oktapreview.com"))
	require.Empty(t, link("https://login.example.com"), "custom domains have no derivable admin host")
	require.Empty(t, link("https://evil.okta.com.example"))
	require.Empty(t, link("https://a.b.okta.com"))
	require.Empty(t, link("http://tenant.okta.com"))
}
