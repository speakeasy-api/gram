package plugins_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	keysrepo "github.com/speakeasy-api/gram/server/internal/keys/repo"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

func TestResolvePluginPublishActor(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestPluginsService(t)
	fixtures := testrepo.New(ti.conn)
	repo := pluginsrepo.New(ti.conn)

	orgID := "org_publish_actor_" + uuid.NewString()
	require.NoError(t, fixtures.CreateOrganizationMetadataFixture(ctx, testrepo.CreateOrganizationMetadataFixtureParams{
		ID:                 orgID,
		Name:               "Publish Actor Org",
		Slug:               "publish-actor-" + uuid.NewString()[:8],
		GramAccountType:    "free",
		WorkosID:           pgtype.Text{},
		Whitelisted:        false,
		FreeTrialStartedAt: pgtype.Timestamptz{Time: time.Now(), Valid: true},
		FreeTrialEndsAt:    pgtype.Timestamptz{Time: time.Now().Add(24 * time.Hour), Valid: true},
		DisabledAt:         pgtype.Timestamptz{},
		CreatedAt:          pgtype.Timestamptz{Time: time.Now(), Valid: true},
	}))
	projectID, err := fixtures.CreateProjectFixture(ctx, testrepo.CreateProjectFixtureParams{
		ID:             uuid.New(),
		Name:           "publish actor",
		Slug:           "publish-actor",
		OrganizationID: orgID,
	})
	require.NoError(t, err)

	_, err = repo.CreateDefaultPlugin(ctx, pluginsrepo.CreateDefaultPluginParams{OrganizationID: orgID, ProjectID: projectID})
	require.NoError(t, err)

	addUser := func(id string) {
		require.NoError(t, fixtures.InsertUserFixture(ctx, testrepo.InsertUserFixtureParams{ID: id, Email: id + "@example.com", DisplayName: id}))
	}
	addMember := func(id string) {
		addUser(id)
		require.NoError(t, fixtures.CreateOrganizationUserRelationshipFixture(ctx, testrepo.CreateOrganizationUserRelationshipFixtureParams{
			OrganizationID: orgID,
			UserID:         pgtype.Text{String: id, Valid: true},
		}))
	}
	resolve := func(preferred string) string {
		actor, err := repo.ResolvePluginPublishActor(ctx, pluginsrepo.ResolvePluginPublishActorParams{
			OrganizationID:  orgID,
			PreferredUserID: preferred,
			ProjectID:       projectID,
		})
		require.NoError(t, err)
		if preferred == "" {
			candidates, err := repo.ListPluginPublishCandidates(ctx, pluginsrepo.ListPluginPublishCandidatesParams{AfterProjectID: uuid.Nil, ResultLimit: 100})
			require.NoError(t, err)
			found := false
			for _, candidate := range candidates {
				if candidate.ProjectID == projectID {
					found = true
					require.Equal(t, actor, candidate.CreatedByUserID)
				}
			}
			require.True(t, found, "sweep must retain candidates even without an actor")
		}
		return actor
	}

	outsider := "user_outsider_" + uuid.NewString()
	addUser(outsider)

	// No member at all: nothing to attribute keys to, whoever asked.
	require.Empty(t, resolve(outsider))
	require.Empty(t, resolve(""))
	require.Empty(t, resolve("system"))

	oldest := "user_oldest_" + uuid.NewString()
	newer := "user_newer_" + uuid.NewString()
	addMember(oldest)
	addMember(newer)

	// A member who made the change keeps the attribution.
	require.Equal(t, newer, resolve(newer))

	// A non-member (an admin from another organization), the sweep and the
	// placeholder all land on the oldest member.
	require.Equal(t, oldest, resolve(outsider))
	require.Equal(t, oldest, resolve(""))
	require.Equal(t, oldest, resolve("system"))

	mintKey := func(creator string) {
		_, err := keysrepo.New(ti.conn).CreateAPIKey(ctx, keysrepo.CreateAPIKeyParams{
			OrganizationID:  orgID,
			ProjectID:       uuid.NullUUID{UUID: projectID, Valid: true},
			CreatedByUserID: creator,
			Name:            "plugins-mcp-" + uuid.NewString()[:8],
			KeyPrefix:       "gram_test",
			KeyHash:         "hash-" + uuid.NewString(),
			Scopes:          []string{"consumer"},
		})
		require.NoError(t, err)
	}

	// A key minted under the historical placeholder does not qualify as a
	// prior attribution.
	mintKey("system")
	require.Equal(t, oldest, resolve(outsider))

	// Once the project has a key with a real creator, later publishes stay
	// attributed to the creator of the newest one rather than hopping to the
	// oldest member.
	mintKey(newer)
	require.Equal(t, newer, resolve(outsider))
	require.Equal(t, newer, resolve(""))

	// Multiple eligible creators pin the newest-key ordering in both paths.
	mintKey(oldest)
	require.Equal(t, oldest, resolve(outsider))
	require.Equal(t, oldest, resolve(""))

	// A key creator who left the organization no longer qualifies.
	require.NoError(t, fixtures.ForceSoftDeleteOrganizationUserRelationship(ctx, testrepo.ForceSoftDeleteOrganizationUserRelationshipParams{
		OrganizationID: orgID,
		UserID:         pgtype.Text{String: newer, Valid: true},
	}))
	require.Equal(t, oldest, resolve(newer))
	require.Equal(t, oldest, resolve(""))
}
