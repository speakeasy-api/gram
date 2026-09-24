package adminmcp

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type postgresStaffAuthorizationStore struct{ db *pgxpool.Pool }

// Authorize rotates the active connection generation and creates a one-use
// grant in the same transaction, invalidating every earlier access session.
func (s postgresStaffAuthorizationStore) Authorize(ctx context.Context, input staffAuthorization) error {
	if s.db == nil || input.Subject == "" || input.ClientID == "" || input.SessionEnc == "" || input.ResourceURI == "" || input.CodeHash == "" {
		return fmt.Errorf("staff authorization state is incomplete")
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin staff MCP authorization: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var clientID uuid.UUID
	err = tx.QueryRow(ctx, `
SELECT id FROM admin_mcp_oauth_clients
WHERE client_id = $1 AND revoked_at IS NULL AND (client_secret_expires_at IS NULL OR client_secret_expires_at > clock_timestamp())
FOR UPDATE
`, input.ClientID).Scan(&clientID)
	if errors.Is(err, pgx.ErrNoRows) {
		return errStaffGrant
	}
	if err != nil {
		return fmt.Errorf("lock staff MCP client: %w", err)
	}
	var connectionID uuid.UUID
	err = tx.QueryRow(ctx, `
INSERT INTO admin_mcp_connections
 (subject_urn, oauth_client_id, admin_session_id_enc, scopes, resource_uri, authorization_expires_at)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (subject_urn, oauth_client_id) WHERE revoked_at IS NULL
DO UPDATE SET admin_session_id_enc = EXCLUDED.admin_session_id_enc,
 scopes = EXCLUDED.scopes, resource_uri = EXCLUDED.resource_uri,
 active_generation = generate_uuidv7(), authorization_expires_at = EXCLUDED.authorization_expires_at,
 authorized_at = clock_timestamp(), reauthorized_at = clock_timestamp(),
 reauthorization_required_at = NULL, reauthorization_reason = NULL,
 updated_at = clock_timestamp()
RETURNING id
`, input.Subject, clientID, input.SessionEnc, input.Scopes, input.ResourceURI, input.ExpiresAt).Scan(&connectionID)
	if err != nil {
		return fmt.Errorf("authorize staff MCP connection: %w", err)
	}
	_, err = tx.Exec(ctx, `
INSERT INTO admin_mcp_authorization_grants
 (authorization_code_hash, oauth_client_id, connection_id, connection_generation,
  redirect_uri, code_challenge, scopes, resource_uri, expires_at)
SELECT $1, $2, id, active_generation, $3, $4, $5, $6, $7
FROM admin_mcp_connections WHERE id = $8 AND revoked_at IS NULL
`, input.CodeHash, clientID, input.RedirectURI, input.CodeChallenge, input.Scopes, input.ResourceURI, input.GrantExpires, connectionID)
	if err != nil {
		return fmt.Errorf("create staff MCP authorization grant: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit staff MCP authorization: %w", err)
	}
	return nil
}
