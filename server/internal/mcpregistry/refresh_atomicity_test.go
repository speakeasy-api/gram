package mcpregistry

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCatalogRefreshLateWriteFailureRollsBack(t *testing.T) {
	t.Parallel()
	ctx, s, db := newTestService(t)
	source := refreshSourceFunc(func(_ context.Context) (CatalogBundle, error) {
		return refreshTestBundle(strings.Replace(basicRecord, "example.test/demo", "example.test/first", 1), strings.Replace(basicRecord, "example.test/demo", "example.test/second", 1)), nil
	})
	r := NewCatalogRefresher(s, "local", "production", source, nil)
	plan, err := r.Preview(ctx, "staff-owner")
	require.NoError(t, err)
	require.Len(t, plan.Changes, 2)
	require.Equal(t, "create", plan.Changes[0].Action)
	require.Equal(t, "create", plan.Changes[1].Action)
	// Install the database failure only after a successful preview. The first
	// write executes normally; PostgreSQL rejects the second in the real Apply tx.
	_, err = db.Exec(ctx, `CREATE FUNCTION reject_second_refresh_fixture() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.data #>> '{server,name}' = 'example.test/second' THEN RAISE EXCEPTION 'injected late refresh failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER reject_second_refresh_fixture BEFORE INSERT ON mcp_registry_entries FOR EACH ROW EXECUTE FUNCTION reject_second_refresh_fixture()`) //nolint:glint // notestingrawsql: inject a database write failure after preview to prove transaction rollback, not a production query.
	require.NoError(t, err)
	_, err = r.Apply(ctx, "staff-owner", plan.ID)
	require.ErrorContains(t, err, "injected late refresh failure")
	for _, name := range []string{"example.test/first", "example.test/second"} {
		_, err = s.GetByName(ctx, name)
		require.ErrorIs(t, err, ErrNotFound)
	}
	tx, err := db.Begin(ctx) //nolint:glint // notestingrawsql: inspect persisted transaction boundaries with production storage helpers.
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(ctx) }()
	state, _, err := refreshReadState(ctx, tx, "production")
	require.NoError(t, err)
	require.Empty(t, state.Baseline)
	require.Nil(t, state.LastFullSuccessAt)
	require.Empty(t, state.LastFullSuccessRevision)
	require.Equal(t, "failed", state.LastAttemptState, "only bounded failed-attempt diagnostics persist outside the rolled-back apply")
	data, receipt, _, err := refreshLoadPlan(ctx, tx, plan.ID, "production", "staff-owner")
	require.NoError(t, err)
	require.True(t, json.Valid(data))
	require.Empty(t, receipt)
	require.NoError(t, tx.Rollback(ctx))
	_, err = db.Exec(ctx, `DROP TRIGGER reject_second_refresh_fixture ON mcp_registry_entries; DROP FUNCTION reject_second_refresh_fixture()`) //nolint:glint // notestingrawsql: remove the injected database failure so the exact stored plan can retry.
	require.NoError(t, err)
	result, err := r.Apply(ctx, "staff-owner", plan.ID)
	require.NoError(t, err)
	require.Equal(t, "completed", result.State)
	require.Equal(t, plan.SourceRevision, result.SourceRevision)
}
