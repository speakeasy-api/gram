package adminmcp

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var errStaffGrant = errors.New("staff OAuth grant is invalid")
var errStaffRefreshReuse = errors.New("staff OAuth refresh token was reused")

type staffTokenConnection struct {
	ID            uuid.UUID
	ClientRowID   uuid.UUID
	Subject       string
	SessionEnc    string
	ResourceURI   string
	Scopes        []string
	Generation    uuid.UUID
	AuthorizedTil time.Time
}

type staffIssuedSession struct {
	ID          uuid.UUID
	JTI         string
	RefreshHash string
	ExpiresAt   time.Time
	RefreshTil  time.Time
}

type staffGrantStore interface {
	ValidateGrant(context.Context, string, string, string, string, time.Time) (staffTokenConnection, error)
	ExchangeGrant(context.Context, string, string, string, string, staffIssuedSession, time.Time) error
	PrepareRefresh(context.Context, string, string, time.Time) (staffTokenConnection, error)
	RotateRefresh(context.Context, string, string, staffTokenConnection, staffIssuedSession, time.Time) error
}

type postgresStaffGrantStore struct{ db *pgxpool.Pool }

func (s postgresStaffGrantStore) ValidateGrant(ctx context.Context, codeHash, clientID, redirectURI, verifier string, now time.Time) (staffTokenConnection, error) {
	if s.db == nil {
		return staffTokenConnection{}, errors.New("staff OAuth state unavailable")
	}
	var connection staffTokenConnection
	var challenge, storedRedirect string
	err := s.db.QueryRow(ctx, staffGrantQuery+`
 AND auth_grant.authorization_code_hash = $1 AND client.client_id = $2
`, codeHash, clientID).Scan(&connection.ID, &connection.ClientRowID, &connection.Subject, &connection.SessionEnc, &connection.ResourceURI, &connection.Scopes, &connection.Generation, &connection.AuthorizedTil, &challenge, &storedRedirect)
	if errors.Is(err, pgx.ErrNoRows) {
		return staffTokenConnection{}, errStaffGrant
	}
	if err != nil {
		return staffTokenConnection{}, fmt.Errorf("lookup staff authorization grant: %w", err)
	}
	if !validStaffVerifier(verifier) || !matchesStaffChallenge(verifier, challenge) || !now.Before(connection.AuthorizedTil) || storedRedirect != redirectURI {
		return staffTokenConnection{}, errStaffGrant
	}
	return connection, nil
}

const staffGrantQuery = `
SELECT connection.id, connection.oauth_client_id, connection.subject_urn,
 connection.admin_session_id_enc, connection.resource_uri, connection.scopes,
 connection.active_generation, connection.authorization_expires_at, auth_grant.code_challenge, auth_grant.redirect_uri
FROM admin_mcp_authorization_grants AS auth_grant
JOIN admin_mcp_connections AS connection
 ON connection.id = auth_grant.connection_id AND connection.oauth_client_id = auth_grant.oauth_client_id
JOIN admin_mcp_oauth_clients AS client ON client.id = auth_grant.oauth_client_id
 AND client.revoked_at IS NULL AND (client.client_secret_expires_at IS NULL OR client.client_secret_expires_at > clock_timestamp())
WHERE auth_grant.consumed_at IS NULL AND auth_grant.revoked_at IS NULL
 AND auth_grant.expires_at > clock_timestamp() AND connection.revoked_at IS NULL
 AND connection.reauthorization_required_at IS NULL
 AND connection.active_generation = auth_grant.connection_generation
 AND connection.authorization_expires_at > clock_timestamp()
 AND connection.scopes = auth_grant.scopes AND connection.resource_uri = auth_grant.resource_uri
`

func (s postgresStaffGrantStore) ExchangeGrant(ctx context.Context, codeHash, clientID, redirectURI, verifier string, session staffIssuedSession, now time.Time) error {
	if s.db == nil {
		return errors.New("staff OAuth state unavailable")
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin staff code exchange: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var connection staffTokenConnection
	var challenge, storedRedirect string
	err = tx.QueryRow(ctx, `
SELECT connection.id, connection.oauth_client_id, connection.subject_urn,
 connection.admin_session_id_enc, connection.resource_uri, connection.scopes,
 connection.active_generation, connection.authorization_expires_at,
 auth_grant.code_challenge, auth_grant.redirect_uri
FROM admin_mcp_authorization_grants AS auth_grant
JOIN admin_mcp_connections AS connection
 ON connection.id = auth_grant.connection_id AND connection.oauth_client_id = auth_grant.oauth_client_id
JOIN admin_mcp_oauth_clients AS client ON client.id = auth_grant.oauth_client_id
WHERE auth_grant.authorization_code_hash = $1 AND client.client_id = $2
 AND auth_grant.consumed_at IS NULL AND auth_grant.revoked_at IS NULL
 AND auth_grant.expires_at > clock_timestamp() AND connection.revoked_at IS NULL
 AND connection.reauthorization_required_at IS NULL
 AND connection.active_generation = auth_grant.connection_generation
 AND connection.authorization_expires_at > clock_timestamp()
 AND connection.scopes = auth_grant.scopes AND connection.resource_uri = auth_grant.resource_uri
 AND client.revoked_at IS NULL
 AND (client.client_secret_expires_at IS NULL OR client.client_secret_expires_at > clock_timestamp())
FOR UPDATE OF auth_grant, connection
`, codeHash, clientID).Scan(&connection.ID, &connection.ClientRowID, &connection.Subject, &connection.SessionEnc, &connection.ResourceURI, &connection.Scopes, &connection.Generation, &connection.AuthorizedTil, &challenge, &storedRedirect)
	if errors.Is(err, pgx.ErrNoRows) {
		return errStaffGrant
	}
	if err != nil {
		return fmt.Errorf("lock staff authorization grant: %w", err)
	}
	if storedRedirect != redirectURI || !validStaffVerifier(verifier) || !matchesStaffChallenge(verifier, challenge) || !validStaffIssuedSession(session, connection, now) {
		return errStaffGrant
	}
	_, err = tx.Exec(ctx, `UPDATE admin_mcp_authorization_grants SET consumed_at = $2, updated_at = $2 WHERE authorization_code_hash = $1`, codeHash, now)
	if err != nil {
		return fmt.Errorf("consume staff authorization grant: %w", err)
	}
	if err := insertStaffSession(ctx, tx, connection, session); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit staff code exchange: %w", err)
	}
	return nil
}

func validStaffIssuedSession(session staffIssuedSession, connection staffTokenConnection, now time.Time) bool {
	return session.ID != uuid.Nil && session.JTI != "" && session.RefreshHash != "" &&
		session.ExpiresAt.After(now) && !session.ExpiresAt.After(connection.AuthorizedTil) &&
		session.RefreshTil.After(now) && !session.RefreshTil.After(connection.AuthorizedTil)
}

func insertStaffSession(ctx context.Context, tx pgx.Tx, connection staffTokenConnection, session staffIssuedSession) error {
	_, err := tx.Exec(ctx, `
INSERT INTO admin_mcp_sessions
 (id, connection_id, oauth_client_id, connection_generation, jti, refresh_token_hash, expires_at, refresh_expires_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
`, session.ID, connection.ID, connection.ClientRowID, connection.Generation, session.JTI, session.RefreshHash, session.ExpiresAt, session.RefreshTil)
	if err != nil {
		return fmt.Errorf("persist staff MCP session: %w", err)
	}
	return nil
}

func (s postgresStaffGrantStore) PrepareRefresh(ctx context.Context, refreshHash, clientID string, now time.Time) (staffTokenConnection, error) {
	if s.db == nil {
		return staffTokenConnection{}, errors.New("staff OAuth state unavailable")
	}
	var connection staffTokenConnection
	var rotatedAt, revokedAt, reauthorizationRequiredAt *time.Time
	var refreshTil time.Time
	err := s.db.QueryRow(ctx, `
SELECT connection.id, connection.oauth_client_id, connection.subject_urn,
 connection.admin_session_id_enc, connection.resource_uri, connection.scopes,
 connection.active_generation, connection.authorization_expires_at,
 session.rotated_at, session.revoked_at, session.refresh_expires_at,
 connection.reauthorization_required_at
FROM admin_mcp_sessions AS session
JOIN admin_mcp_connections AS connection
 ON connection.id = session.connection_id AND connection.oauth_client_id = session.oauth_client_id
JOIN admin_mcp_oauth_clients AS client ON client.id = session.oauth_client_id
WHERE session.refresh_token_hash = $1 AND client.client_id = $2
 AND client.revoked_at IS NULL AND (client.client_secret_expires_at IS NULL OR client.client_secret_expires_at > clock_timestamp())
 AND connection.revoked_at IS NULL
 AND connection.active_generation = session.connection_generation
`, refreshHash, clientID).Scan(&connection.ID, &connection.ClientRowID, &connection.Subject, &connection.SessionEnc, &connection.ResourceURI, &connection.Scopes, &connection.Generation, &connection.AuthorizedTil, &rotatedAt, &revokedAt, &refreshTil, &reauthorizationRequiredAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return staffTokenConnection{}, errStaffGrant
	}
	if err != nil {
		return staffTokenConnection{}, fmt.Errorf("lookup staff refresh session: %w", err)
	}
	if rotatedAt != nil {
		return staffTokenConnection{}, s.terminalizeRefreshReuse(ctx, refreshHash, clientID, now)
	}
	if revokedAt != nil || reauthorizationRequiredAt != nil || !now.Before(refreshTil) || !now.Before(connection.AuthorizedTil) {
		return staffTokenConnection{}, errStaffGrant
	}
	return connection, nil
}

func (s postgresStaffGrantStore) terminalizeRefreshReuse(ctx context.Context, refreshHash, clientID string, now time.Time) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin staff refresh reuse check: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var connectionID, sessionGeneration, activeGeneration uuid.UUID
	var rotatedAt *time.Time
	err = tx.QueryRow(ctx, `
SELECT connection.id, session.connection_generation, connection.active_generation, session.rotated_at
FROM admin_mcp_sessions AS session
JOIN admin_mcp_connections AS connection ON connection.id = session.connection_id
JOIN admin_mcp_oauth_clients AS client ON client.id = session.oauth_client_id
WHERE session.refresh_token_hash = $1 AND client.client_id = $2
FOR UPDATE OF connection, session
`, refreshHash, clientID).Scan(&connectionID, &sessionGeneration, &activeGeneration, &rotatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return errStaffGrant
	}
	if err != nil {
		return fmt.Errorf("lock staff refresh reuse: %w", err)
	}
	if rotatedAt == nil || activeGeneration != sessionGeneration {
		return errStaffGrant
	}
	_, err = tx.Exec(ctx, `
UPDATE admin_mcp_connections SET reauthorization_required_at = $3,
 reauthorization_reason = 'refresh_reuse', updated_at = $3
WHERE id = $1 AND active_generation = $2 AND revoked_at IS NULL
`, connectionID, sessionGeneration, now)
	if err != nil {
		return fmt.Errorf("terminalize reused staff refresh token: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit staff refresh reuse: %w", err)
	}
	return errStaffRefreshReuse
}

func (s postgresStaffGrantStore) RotateRefresh(ctx context.Context, refreshHash, clientID string, expected staffTokenConnection, replacement staffIssuedSession, now time.Time) error {
	if s.db == nil {
		return errors.New("staff OAuth state unavailable")
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin staff refresh rotation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var sessionID, sessionGeneration uuid.UUID
	var connection staffTokenConnection
	var clientRevokedAt, clientSecretExpiresAt, connectionRevokedAt, reauthorizationRequiredAt *time.Time
	var rotatedAt, revokedAt *time.Time
	var refreshTil time.Time
	err = tx.QueryRow(ctx, `
SELECT session.id, connection.id, connection.oauth_client_id,
 connection.subject_urn, connection.admin_session_id_enc, connection.resource_uri,
 connection.scopes, connection.active_generation, connection.authorization_expires_at,
 session.connection_generation, session.rotated_at, session.revoked_at, session.refresh_expires_at,
 client.revoked_at, client.client_secret_expires_at, connection.revoked_at, connection.reauthorization_required_at
FROM admin_mcp_sessions AS session
JOIN admin_mcp_connections AS connection
 ON connection.id = session.connection_id AND connection.oauth_client_id = session.oauth_client_id
JOIN admin_mcp_oauth_clients AS client ON client.id = session.oauth_client_id
WHERE session.refresh_token_hash = $1 AND client.client_id = $2
FOR UPDATE OF session, connection
`, refreshHash, clientID).Scan(&sessionID, &connection.ID, &connection.ClientRowID, &connection.Subject, &connection.SessionEnc, &connection.ResourceURI, &connection.Scopes, &connection.Generation, &connection.AuthorizedTil, &sessionGeneration, &rotatedAt, &revokedAt, &refreshTil, &clientRevokedAt, &clientSecretExpiresAt, &connectionRevokedAt, &reauthorizationRequiredAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return errStaffGrant
	}
	if err != nil {
		return fmt.Errorf("lock staff refresh session: %w", err)
	}
	if rotatedAt != nil {
		// Only terminalize the generation that issued the reused token, not a
		// later staff reauthorization that happened while this request waited.
		_, err = tx.Exec(ctx, `
UPDATE admin_mcp_connections SET reauthorization_required_at = $3,
 reauthorization_reason = 'refresh_reuse', updated_at = $3
WHERE id = $1 AND active_generation = $2 AND revoked_at IS NULL
`, connection.ID, sessionGeneration, now)
		if err != nil {
			return fmt.Errorf("terminalize reused staff refresh token: %w", err)
		}
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit staff refresh reuse: %w", err)
		}
		return errStaffRefreshReuse
	}
	if clientRevokedAt != nil || (clientSecretExpiresAt != nil && !now.Before(*clientSecretExpiresAt)) || connectionRevokedAt != nil || reauthorizationRequiredAt != nil || revokedAt != nil || !now.Before(refreshTil) || !now.Before(connection.AuthorizedTil) || connection.ID != expected.ID || connection.Generation != expected.Generation || connection.Generation != sessionGeneration || connection.Subject != expected.Subject || connection.ResourceURI != expected.ResourceURI || connection.SessionEnc != expected.SessionEnc || !validStaffIssuedSession(replacement, connection, now) {
		return errStaffGrant
	}
	if err := insertStaffSession(ctx, tx, connection, replacement); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE admin_mcp_sessions SET rotated_at = $2, revoked_at = $2, replaced_by_session_id = $3, updated_at = $2 WHERE id = $1`, sessionID, now, replacement.ID)
	if err != nil {
		return fmt.Errorf("rotate staff refresh session: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit staff refresh rotation: %w", err)
	}
	return nil
}

func validStaffVerifier(verifier string) bool {
	if len(verifier) < 43 || len(verifier) > 128 {
		return false
	}
	for _, c := range verifier {
		if (c < 'A' || c > 'Z') && (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' && c != '_' && c != '.' && c != '~' {
			return false
		}
	}
	return true
}

func matchesStaffChallenge(verifier, challenge string) bool {
	sum := sha256.Sum256([]byte(verifier))
	encoded := base64.RawURLEncoding.EncodeToString(sum[:])
	return subtle.ConstantTimeCompare([]byte(encoded), []byte(challenge)) == 1
}
