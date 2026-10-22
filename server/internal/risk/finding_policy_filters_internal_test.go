package risk

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"

	riskv1 "github.com/speakeasy-api/gram/infra/gen/gram/risk/v1"
)

type missingPolicyDB struct {
	queries int
}

func (d *missingPolicyDB) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, nil
}

func (d *missingPolicyDB) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, pgx.ErrNoRows
}

func (d *missingPolicyDB) QueryRow(context.Context, string, ...any) pgx.Row {
	d.queries++
	return missingRow{}
}

func (d *missingPolicyDB) CopyFrom(context.Context, pgx.Identifier, []string, pgx.CopyFromSource) (int64, error) {
	return 0, nil
}

type missingRow struct{}

func (missingRow) Scan(...any) error { return pgx.ErrNoRows }

func TestFindingPolicyFilterResolver_MissingPolicyQueriesOncePerBatch(t *testing.T) {
	t.Parallel()

	db := &missingPolicyDB{}
	resolver := newFindingPolicyFilterResolver(db)
	message := riskv1.Finding_builder{
		ProjectId:    new(uuid.NewString()),
		RiskPolicyId: new(uuid.NewString()),
	}.Build()

	batch := make(map[findingPolicyFilterKey]struct{})
	for range 3 {
		filters, err := resolver.filtersFor(t.Context(), message, batch)
		require.NoError(t, err)
		require.True(t, filters.disabledRules.Empty())
	}
	require.Equal(t, 1, db.queries)

	_, err := resolver.filtersFor(t.Context(), message, make(map[findingPolicyFilterKey]struct{}))
	require.NoError(t, err)
	require.Equal(t, 2, db.queries, "a missing policy is looked up again in the next batch")
}
