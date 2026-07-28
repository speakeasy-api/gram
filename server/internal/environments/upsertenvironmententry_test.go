package environments_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/environments"
	"github.com/speakeasy-api/gram/server/internal/environments/repo"
)

func TestUpsertEnvironmentEntry_ScopedByProject(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestEnvironmentService(t)

	env, err := ti.service.CreateEnvironment(ctx, &gen.CreateEnvironmentPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		OrganizationID:   "",
		Name:             "scoped-env",
		Description:      nil,
		Entries: []*gen.EnvironmentEntryInput{
			{Name: "KEY1", Value: new("value1"), IsSecret: new(false)},
		},
	})
	require.NoError(t, err)

	environmentID, err := uuid.Parse(env.ID)
	require.NoError(t, err)
	projectID, err := uuid.Parse(env.ProjectID)
	require.NoError(t, err)

	queries := repo.New(ti.conn)

	entry, err := queries.UpsertEnvironmentEntry(ctx, repo.UpsertEnvironmentEntryParams{
		EnvironmentID: environmentID,
		ProjectID:     projectID,
		Name:          "KEY1",
		Value:         "updated-value",
		IsSecret:      false,
	})
	require.NoError(t, err)
	require.Equal(t, "updated-value", entry.Value)

	_, err = queries.UpsertEnvironmentEntry(ctx, repo.UpsertEnvironmentEntryParams{
		EnvironmentID: environmentID,
		ProjectID:     uuid.New(),
		Name:          "KEY1",
		Value:         "cross-project-value",
		IsSecret:      false,
	})
	require.ErrorIs(t, err, pgx.ErrNoRows)

	entries, err := queries.ListEnvironmentEntries(ctx, repo.ListEnvironmentEntriesParams{
		ProjectID:     projectID,
		EnvironmentID: environmentID,
	})
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.Equal(t, "updated-value", entries[0].Value)
}
