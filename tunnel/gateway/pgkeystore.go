package gateway

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/tunnel/wire"
)

// PostgresKeyResolver resolves tunnel keys against durable
// tunnelled_mcp_servers rows.
type PostgresKeyResolver struct {
	db *pgxpool.Pool
}

// NewPostgresKeyResolver wraps the gateway's Postgres pool.
func NewPostgresKeyResolver(db *pgxpool.Pool) *PostgresKeyResolver {
	return &PostgresKeyResolver{db: db}
}

// Close releases the underlying pool.
func (r *PostgresKeyResolver) Close() {
	if r != nil && r.db != nil {
		r.db.Close()
	}
}

// Resolve validates a bearer value and returns the active tunnel ID bound to
// that key hash.
func (r *PostgresKeyResolver) Resolve(ctx context.Context, bearer string) (string, bool, error) {
	key := strings.TrimSpace(strings.TrimPrefix(bearer, "Bearer "))
	if !wire.HasKeyPrefix(key) {
		return "", false, nil
	}

	var tunnelID string
	err := r.db.QueryRow(ctx, `
SELECT id::text
FROM tunnelled_mcp_servers
WHERE key_hash = $1
  AND status = 'active'
  AND deleted IS FALSE
LIMIT 1
`, wire.HashKey(key)).Scan(&tunnelID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return tunnelID, true, nil
}

var _ KeyResolver = (*PostgresKeyResolver)(nil)
