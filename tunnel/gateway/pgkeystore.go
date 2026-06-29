package gateway

import (
	"context"
	"errors"
	"fmt"
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
  AND status IN ('created', 'active')
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

func (r *PostgresKeyResolver) MarkConnected(ctx context.Context, tunnelID, keyHash, agentVersion string) error {
	tag, err := r.db.Exec(ctx, `
UPDATE tunnelled_mcp_servers
SET
  status = 'active',
  agent_version = NULLIF($3, ''),
  last_seen_at = clock_timestamp(),
  updated_at = clock_timestamp()
WHERE id = $1::uuid
  AND key_hash = $2
  AND status IN ('created', 'active')
  AND deleted IS FALSE
`, tunnelID, keyHash, agentVersion)
	if err != nil {
		return fmt.Errorf("mark tunnel connected: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

func (r *PostgresKeyResolver) IsActive(ctx context.Context, tunnelID, keyHash string) (bool, error) {
	var ok bool
	err := r.db.QueryRow(ctx, `
SELECT EXISTS (
  SELECT 1
  FROM tunnelled_mcp_servers
  WHERE id = $1::uuid
    AND key_hash = $2
    AND status = 'active'
    AND deleted IS FALSE
)
`, tunnelID, keyHash).Scan(&ok)
	if err != nil {
		return false, err
	}
	return ok, nil
}

var _ KeyResolver = (*PostgresKeyResolver)(nil)
var _ ConnectionRecorder = (*PostgresKeyResolver)(nil)
var _ ActiveTunnelChecker = (*PostgresKeyResolver)(nil)
