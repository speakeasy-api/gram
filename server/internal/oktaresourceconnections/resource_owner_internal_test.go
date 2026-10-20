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
