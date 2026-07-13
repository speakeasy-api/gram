package activities_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	repo "github.com/speakeasy-api/gram/server/internal/background/activities/repo"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	openrouterrepo "github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter/repo"
)

// An org holding both a chat and an internal OpenRouter key must yield two
// monitoring rows (one per key), each naming its key type — the credits
// metrics loop consumes them as-is, and without the discriminator the two
// series would overwrite each other's gauges.
func TestGetOpenRouterCreditsMonitoringTargets_OneRowPerKeyType(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := infra.CloneTestDatabase(t, "targetstest")
	require.NoError(t, err)

	orgID := "org-" + uuid.NewString()[:8]
	_, err = orgrepo.New(conn).UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{
		ID:          orgID,
		Name:        "Key Type Org",
		Slug:        orgID,
		WorkosID:    pgtype.Text{},
		Whitelisted: pgtype.Bool{},
	})
	require.NoError(t, err)

	orKeys := openrouterrepo.New(conn)
	_, err = orKeys.CreateOpenRouterAPIKey(ctx, openrouterrepo.CreateOpenRouterAPIKeyParams{
		OrganizationID: orgID,
		KeyType:        "chat",
		Key:            "sk-chat",
		KeyHash:        "hash-chat",
		MonthlyCredits: 100,
	})
	require.NoError(t, err)
	_, err = orKeys.CreateOpenRouterAPIKey(ctx, openrouterrepo.CreateOpenRouterAPIKeyParams{
		OrganizationID: orgID,
		KeyType:        "internal",
		Key:            "sk-internal",
		KeyHash:        "hash-internal",
		MonthlyCredits: 100,
	})
	require.NoError(t, err)

	rows, err := repo.New(conn).GetOpenRouterCreditsMonitoringTargets(ctx, []string{"free"})
	require.NoError(t, err)

	byKeyType := map[string]string{}
	for _, row := range rows {
		if row.OrganizationID != orgID {
			continue
		}
		byKeyType[row.KeyType] = row.ApiKey
	}
	require.Equal(t, map[string]string{
		"chat":     "sk-chat",
		"internal": "sk-internal",
	}, byKeyType)

	// The single-key lookup discriminates by type: each read returns its own
	// row, never the sibling's.
	chatKey, err := orKeys.GetOpenRouterAPIKey(ctx, openrouterrepo.GetOpenRouterAPIKeyParams{
		OrganizationID: orgID,
		KeyType:        "chat",
	})
	require.NoError(t, err)
	require.Equal(t, "sk-chat", chatKey.Key)

	internalKey, err := orKeys.GetOpenRouterAPIKey(ctx, openrouterrepo.GetOpenRouterAPIKeyParams{
		OrganizationID: orgID,
		KeyType:        "internal",
	})
	require.NoError(t, err)
	require.Equal(t, "sk-internal", internalKey.Key)
}
