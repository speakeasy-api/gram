// Package remotemcptest provides helpers for seeding remote_mcp_servers
// rows in tests across packages that depend on a remote MCP server FK.
package remotemcptest

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
)

// SeedServer inserts a remote_mcp_servers row, generating a UUIDv7 id when
// one isn't supplied. CreateServer requires an explicit id (the production
// impl generates the id first so the slug can be computed from its last
// four chars); auto-generating it here keeps test call sites short and
// removes a class of accidental uuid.Nil collisions when a test seeds
// multiple servers in the same DB.
func SeedServer(
	t *testing.T,
	ctx context.Context,
	conn *pgxpool.Pool,
	params repo.CreateServerParams,
) repo.RemoteMcpServer {
	t.Helper()

	if params.ID == uuid.Nil {
		params.ID = uuid.Must(uuid.NewV7())
	}

	server, err := repo.New(conn).CreateServer(ctx, params)
	require.NoError(t, err)
	return server
}

// SeedHeader inserts a remote_mcp_server_headers row directly, bypassing the
// encryption wrapper. The value is stored verbatim, so callers must not seed a
// secret header — reading one back would try to decrypt plaintext. Use the
// createServerHeader endpoint when a test needs a real secret.
func SeedHeader(
	t *testing.T,
	ctx context.Context,
	conn *pgxpool.Pool,
	params repo.CreateServerHeaderParams,
) repo.RemoteMcpServerHeader {
	t.Helper()

	require.False(t, params.IsSecret, "SeedHeader cannot seed secret headers: the value would not be encrypted")

	header, err := repo.New(conn).CreateServerHeader(ctx, params)
	require.NoError(t, err)
	return header
}
