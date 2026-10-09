package hostedmcp

import (
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestCreateServerError(t *testing.T) {
	t.Parallel()

	slugConflict := &pgconn.PgError{Code: pgerrcode.UniqueViolation, ConstraintName: "mcp_servers_project_id_slug_key"}
	for _, tt := range []struct {
		name      string
		cause     error
		collision bool
	}{
		{name: "server slug", cause: slugConflict, collision: true},
		{name: "wrapped server slug", cause: fmt.Errorf("insert: %w", slugConflict), collision: true},
		{name: "canonical identity", cause: &pgconn.PgError{Code: pgerrcode.UniqueViolation, ConstraintName: "mcp_servers_pkey"}, collision: false},
		{name: "other database error", cause: &pgconn.PgError{Code: pgerrcode.CheckViolation, ConstraintName: "mcp_servers_project_id_slug_key"}, collision: false},
		{name: "connection error", cause: errors.New("connection lost"), collision: false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := createServerError(tt.cause)
			require.ErrorIs(t, err, tt.cause)
			require.Equal(t, tt.collision, errors.Is(err, ErrAddressInUse))
			var shareable *oops.ShareableError
			require.ErrorAs(t, err, &shareable)
			require.Equal(t, oops.CodeConflict, shareable.Code)
			if tt.collision {
				require.Contains(t, err.Error(), "project server slug is already in use")
				var pgErr *pgconn.PgError
				require.ErrorAs(t, err, &pgErr)
				require.Same(t, slugConflict, pgErr)
			} else {
				require.Contains(t, err.Error(), "create hosted MCP server")
			}
		})
	}
}
