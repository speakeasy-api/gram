package explore

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/explore/repo"
)

func TestExploreSavedQueryRepositoryScopesAndSoftDeletes(t *testing.T) {
	t.Parallel()

	conn := newExploreTestDB(t)
	ctx := t.Context()

	firstOrg := uuid.NewString()
	secondOrg := uuid.NewString()
	createOrganizationFixture(t, conn, firstOrg)
	createOrganizationFixture(t, conn, secondOrg)

	queries := repo.New(conn)
	created, err := queries.CreateExploreSavedQuery(ctx, repo.CreateExploreSavedQueryParams{
		OrganizationID: firstOrg,
		Name:           "Turn cost by response model",
		ChartType:      "bar",
		TimeWindow:     "7d",
		Spec:           []byte(`{"dataset":"turn_usage","calculations":[{"op":"SUM","column":"cost_usd"}],"group_by":["response_model"],"group_expressions":[{"name":"Is Claude","dimension":"response_model","op":"in","values":["claude"]}],"filters":[],"granularity_seconds":0,"sort_by":"","sort_desc":true,"limit":10}`),
	})
	require.NoError(t, err)

	rows, err := queries.ListExploreSavedQueries(ctx, firstOrg)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, created.ID, rows[0].ID)

	_, err = queries.GetExploreSavedQuery(ctx, repo.GetExploreSavedQueryParams{
		OrganizationID: secondOrg,
		ID:             created.ID,
	})
	require.ErrorIs(t, err, pgx.ErrNoRows)

	updated, err := queries.UpdateExploreSavedQuery(ctx, repo.UpdateExploreSavedQueryParams{
		Name:           "Turn cost by provider",
		ChartType:      "table",
		TimeWindow:     "30d",
		Spec:           []byte(`{"dataset":"turn_usage","calculations":[{"op":"SUM","column":"cost_usd"}],"group_by":["provider"],"group_expressions":[{"name":"Is Claude","dimension":"response_model","op":"in","values":["claude"]}],"filters":[],"granularity_seconds":0,"sort_by":"","sort_desc":true,"limit":10}`),
		OrganizationID: firstOrg,
		ID:             created.ID,
	})
	require.NoError(t, err)
	require.Equal(t, "Turn cost by provider", updated.Name)

	_, err = queries.SoftDeleteExploreSavedQuery(ctx, repo.SoftDeleteExploreSavedQueryParams{
		OrganizationID: secondOrg,
		ID:             created.ID,
	})
	require.ErrorIs(t, err, pgx.ErrNoRows)

	deleted, err := queries.SoftDeleteExploreSavedQuery(ctx, repo.SoftDeleteExploreSavedQueryParams{
		OrganizationID: firstOrg,
		ID:             created.ID,
	})
	require.NoError(t, err)
	require.True(t, deleted.Deleted)

	rows, err = queries.ListExploreSavedQueries(ctx, firstOrg)
	require.NoError(t, err)
	require.Empty(t, rows)
}

func TestSavedQueryDecoderRejectsUnknownSpecFields(t *testing.T) {
	t.Parallel()

	now := time.Now()
	_, err := savedQueryFromRow(repo.ExploreSavedQuery{
		ID:             uuid.New(),
		OrganizationID: uuid.NewString(),
		Name:           "Invalid query",
		ChartType:      "bar",
		TimeWindow:     "7d",
		Spec:           []byte(`{"dataset":"events","calculations":[],"unexpected":true,"group_by":[],"group_expressions":[],"filters":[],"granularity_seconds":0,"sort_by":"","sort_desc":true,"limit":10}`),
		CreatedAt:      pgtype.Timestamptz{Time: now, Valid: true, InfinityModifier: pgtype.Finite},
		UpdatedAt:      pgtype.Timestamptz{Time: now, Valid: true, InfinityModifier: pgtype.Finite},
		DeletedAt:      pgtype.Timestamptz{Time: time.Time{}, Valid: false, InfinityModifier: pgtype.Finite},
		Deleted:        false,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), `unknown field "unexpected"`)
}

func TestSavedQueryConditionalGroupsRoundTrip(t *testing.T) {
	t.Parallel()

	spec := savedQuerySpec{
		Dataset:      "turn_usage",
		Calculations: []Calculation{{Op: "SUM", Column: "cost_usd"}},
		GroupBy:      []string{"provider"},
		GroupExpressions: []GroupExpression{{
			Name:      "Is Claude",
			Dimension: "response_model",
			Op:        "in",
			Values:    []string{"claude"},
		}},
		Filters:            nil,
		GranularitySeconds: 0,
		SortBy:             "SUM(cost_usd)",
		SortDesc:           true,
		Limit:              10,
	}
	encoded, err := encodeSavedQuerySpec(spec)
	require.NoError(t, err)

	now := time.Now()
	decoded, err := savedQueryFromRow(repo.ExploreSavedQuery{
		ID:             uuid.New(),
		OrganizationID: uuid.NewString(),
		Name:           "Claude cost",
		ChartType:      "bar",
		TimeWindow:     "7d",
		Spec:           encoded,
		CreatedAt:      pgtype.Timestamptz{Time: now, Valid: true, InfinityModifier: pgtype.Finite},
		UpdatedAt:      pgtype.Timestamptz{Time: now, Valid: true, InfinityModifier: pgtype.Finite},
		DeletedAt:      pgtype.Timestamptz{Time: time.Time{}, Valid: false, InfinityModifier: pgtype.Finite},
		Deleted:        false,
	})
	require.NoError(t, err)
	require.Equal(t, spec, decoded.Spec)
}
