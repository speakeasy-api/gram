package testenv

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

// LinkRetiredOAuthProxy points a toolset at a new OAuth proxy server, as some
// toolsets created before the OAuth proxy was retired still are. No code writes
// or reads the link any more, so it is set with plain SQL. Once the
// toolsets.oauth_proxy_server_id column is dropped there is nothing to link and
// the call does nothing.
func LinkRetiredOAuthProxy(t *testing.T, ctx context.Context, conn *pgxpool.Pool, projectID, toolsetID uuid.UUID) {
	t.Helper()

	tag, err := conn.Exec(ctx, `
		WITH proxy AS (
			INSERT INTO oauth_proxy_servers (project_id, slug) VALUES ($1, $2) RETURNING id
		)
		UPDATE toolsets SET oauth_proxy_server_id = (SELECT id FROM proxy)
		WHERE id = $3 AND project_id = $1`,
		projectID, "retired-proxy-"+uuid.NewString()[:8], toolsetID)
	if pgErr := (*pgconn.PgError)(nil); errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UndefinedColumn && strings.Contains(pgErr.Message, "oauth_proxy_server_id") {
		return
	}
	require.NoError(t, err)
	require.Equal(t, int64(1), tag.RowsAffected())
}
