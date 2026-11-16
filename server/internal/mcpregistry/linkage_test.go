package mcpregistry

import (
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/speakeasy-api/gram/server/internal/mcpregistry/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestExpandLinkageRejectsInvalidOwnership(t *testing.T) {
	t.Parallel()
	ctx, s, db := newTestService(t)
	first, err := s.Create(ctx, []byte(basicRecord))
	require.NoError(t, err)
	other, err := repo.New(db).CreateServer(ctx, "example.test/other")
	require.NoError(t, err)
	unlinked := uuid.New()
	require.NoError(t, repo.New(db).InsertRegistryUnlinkedEntryFixture(ctx, repo.InsertRegistryUnlinkedEntryFixtureParams{ID: unlinked, Data: []byte(`{"server":{"name":"example.test/unlinked"}}`), Published: true}))
	for _, tc := range []struct {
		name, sql string
		args      []any
	}{
		{"missing parent", `UPDATE mcp_registry_entries SET server_id=$1 WHERE id=$2`, []any{uuid.New(), unlinked}},
		{"missing default", `UPDATE mcp_registry_servers SET default_entry_id=$1 WHERE id=$2`, []any{uuid.New(), other.ID}},
		{"foreign default", `UPDATE mcp_registry_servers SET default_entry_id=$1 WHERE id=$2`, []any{first.ID, other.ID}},
		{"unlinked default", `UPDATE mcp_registry_servers SET default_entry_id=$1 WHERE id=$2`, []any{unlinked, other.ID}},
		{"delete selected entry", `DELETE FROM mcp_registry_entries WHERE id=$1`, []any{first.ID}},
		{"reparent selected entry", `UPDATE mcp_registry_entries SET server_id=$1 WHERE id=$2`, []any{other.ID, first.ID}},
		{"unlink selected entry", `UPDATE mcp_registry_entries SET server_id=NULL WHERE id=$1`, []any{first.ID}},
	} {
		// Keep cases sequential: each rejected write must leave the shared fixture intact.
		tx := testenv.BeginTx(t, ctx, db)
		_, err := tx.Exec(ctx, tc.sql, tc.args...) //nolint:glint // notestingrawsql: exercise actual expand foreign keys in a disposable test transaction, including deferred commit rejection.
		if err == nil {
			err = tx.Commit(ctx)
		}
		_ = tx.Rollback(ctx)
		var violation *pgconn.PgError
		require.ErrorAs(t, err, &violation, tc.name)
		require.Equal(t, "23503", violation.Code, tc.name)
	}
	retained, err := s.Get(ctx, first.ID)
	require.NoError(t, err)
	require.Equal(t, first.ServerID, retained.ServerID)
}
