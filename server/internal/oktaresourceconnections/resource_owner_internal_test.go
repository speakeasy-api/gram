package oktaresourceconnections

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/oktaresourceconnections/repo"
)

// Behind a login issuer only the client that owns the resource reads its
// row, as that client's login would; a server without one has no login to
// match, so its resolved client reads it.
func TestResourceScopesFollowLoginOwnership(t *testing.T) {
	t.Parallel()
	project, owner, sibling := uuid.New(), uuid.New(), uuid.New()
	const upstream = "https://mcp.example/mcp"
	snap := &snapshot{
		resources: map[resourceKey]repo.ListRemoteProtectedResourceScopesRow{
			{projectID: project, url: upstream}: {ProjectID: project, ResourceIdentifier: upstream, ScopeOverride: []string{"pinned"}},
		},
		discoverScopes: true,
		now:            time.Now(),
	}
	withLogin := repo.ListEligibleServersRow{ID: uuid.New(), ProjectID: project, UserSessionIssuerID: uuid.NullUUID{UUID: uuid.New(), Valid: true}, RemoteUrl: pgtype.Text{String: upstream, Valid: true}}
	snap.owners = map[uuid.UUID]map[uuid.UUID]bool{withLogin.ID: {owner: true, sibling: false}}

	scopes := snap.resourceScopes(withLogin)
	require.Equal(t, []string{"pinned"}, scopes(owner).Pin)
	require.Empty(t, scopes(sibling).Pin, "a client that does not own the resource gets none")
	require.True(t, scopes(sibling).UseDiscovered, "the flag still ranks its own scope first")

	withoutLogin := withLogin
	withoutLogin.ID = uuid.New()
	withoutLogin.UserSessionIssuerID = uuid.NullUUID{}
	require.Equal(t, []string{"pinned"}, snap.resourceScopes(withoutLogin)(sibling).Pin)
}

// Typed offers own the server's resource without a legacy login-issuer link.
func TestResourceScopesFollowTypedOffersUnderEnforcement(t *testing.T) {
	t.Parallel()
	project, issuer, offered, legacy := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	const upstream = "https://mcp.example/mcp"
	server := repo.ListEligibleServersRow{
		ID: uuid.New(), ProjectID: project, IssuerID: issuer,
		UserSessionIssuerID: uuid.NullUUID{UUID: uuid.New(), Valid: true},
		RemoteUrl:           pgtype.Text{String: upstream, Valid: true},
		TypedAuthEnforced:   true, OfferedClientIds: []uuid.UUID{offered},
	}
	snap := &snapshot{
		resources: map[resourceKey]repo.ListRemoteProtectedResourceScopesRow{
			{projectID: project, url: upstream}: {ScopeOverride: []string{"protected:read"}},
		},
		owners:         map[uuid.UUID]map[uuid.UUID]bool{server.ID: {legacy: true}},
		discoverScopes: true, now: time.Now(),
	}
	scopes := snap.resourceScopes(server)
	require.Equal(t, []string{"protected:read"}, scopes(offered).Pin)
	require.Empty(t, scopes(legacy).Pin, "a legacy link does not grant ownership without an offer")
	client := repo.ListIssuerClientsRow{ID: offered, RemoteSessionIssuerID: issuer, ClientID: "offered", IssuerScopesSupported: []string{"fallback:write"}}
	id, requested, state := resolveClient(server, upstream, "", []repo.ListIssuerClientsRow{client}, nil, scopes)
	require.Equal(t, "offered", id)
	require.Equal(t, ClientBindingSingle, state)
	require.Equal(t, []string{"protected:read"}, requested, "XAA confirmation must not fall back to issuer metadata")
}
