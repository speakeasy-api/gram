//nolint:exhaustruct // Database row projections intentionally omit lifecycle fields not selected by each query.
package platformmcp

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	platformoauth "github.com/speakeasy-api/gram/server/internal/platformmcp/oauth"
	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
)

// PostgresOAuthStore persists the Platform MCP-owned OAuth lifecycle. It keeps opaque
// codes and refresh tokens hashed, and uses transactions for every transition
// that locks or invalidates a grant/session family.
type PostgresOAuthStore struct {
	db *pgxpool.Pool
}

var _ platformoauth.Store = (*PostgresOAuthStore)(nil)

func NewPostgresOAuthStore(db *pgxpool.Pool) *PostgresOAuthStore {
	return &PostgresOAuthStore{db: db}
}

func (s *PostgresOAuthStore) RegisterClient(ctx context.Context, client platformoauth.Client) error {
	if s == nil || s.db == nil || client.ID == "" || client.Name == "" || len(client.RedirectURIs) == 0 {
		return platformoauth.ErrNotFound
	}
	_, err := platformrepo.New(s.db).CreatePlatformMCPOAuthClient(ctx, platformrepo.CreatePlatformMCPOAuthClientParams{
		ClientID:              client.ID,
		ClientSecretHash:      text(client.SecretHash),
		ClientName:            client.Name,
		RedirectUris:          client.RedirectURIs,
		ClientSecretExpiresAt: optionalTimestamp(client.SecretExpiresAt),
	})
	return mapOAuthWriteError(err)
}

func (s *PostgresOAuthStore) GetClient(ctx context.Context, clientID string) (platformoauth.Client, error) {
	if s == nil || s.db == nil {
		return platformoauth.Client{}, platformoauth.ErrNotFound
	}
	row, err := platformrepo.New(s.db).GetActivePlatformMCPOAuthClientByClientID(ctx, clientID)
	if err != nil {
		return platformoauth.Client{}, mapOAuthReadError(err)
	}
	return platformoauth.Client{ID: row.ClientID, SecretHash: row.ClientSecretHash.String, Name: row.ClientName, RedirectURIs: row.RedirectUris, SecretExpiresAt: timePointer(row.ClientSecretExpiresAt)}, nil
}

func (s *PostgresOAuthStore) RevokeClient(ctx context.Context, clientID string, now time.Time) error {
	if s == nil || s.db == nil {
		return platformoauth.ErrNotFound
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin revoke platform mcp client: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	q := platformrepo.New(tx)
	_, err = q.RevokePlatformMCPOAuthClient(ctx, platformrepo.RevokePlatformMCPOAuthClientParams{ClientID: clientID, RevokedAt: timestamp(now)})
	if err != nil {
		return mapOAuthReadError(err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit revoke platform mcp client: %w", err)
	}
	return nil
}

func (s *PostgresOAuthStore) RegisterConnection(ctx context.Context, connection platformoauth.Connection) error {
	if s == nil || s.db == nil {
		return platformoauth.ErrNotFound
	}
	id, err := uuid.Parse(connection.ID)
	if err != nil {
		return platformoauth.ErrNotFound
	}
	client, err := platformrepo.New(s.db).GetActivePlatformMCPOAuthClientByClientID(ctx, connection.ClientID)
	if err != nil {
		return mapOAuthReadError(err)
	}
	generation, err := uuid.Parse(connection.Generation)
	if err != nil {
		return platformoauth.ErrGeneration
	}
	_, err = platformrepo.New(s.db).CreatePlatformMCPConnection(ctx, platformrepo.CreatePlatformMCPConnectionParams{
		ID:               id,
		OrganizationID:   connection.OrganizationID,
		SubjectUrn:       connection.Subject,
		OauthClientID:    client.ID,
		ActiveGeneration: generation,
	})
	return mapOAuthWriteError(err)
}

func (s *PostgresOAuthStore) GetConnection(ctx context.Context, organizationID, subject, clientID string) (platformoauth.Connection, error) {
	if s == nil || s.db == nil {
		return platformoauth.Connection{}, platformoauth.ErrNotFound
	}
	client, err := platformrepo.New(s.db).GetActivePlatformMCPOAuthClientByClientID(ctx, clientID)
	if err != nil {
		return platformoauth.Connection{}, mapOAuthReadError(err)
	}
	row, err := platformrepo.New(s.db).GetActivePlatformMCPConnection(ctx, platformrepo.GetActivePlatformMCPConnectionParams{OrganizationID: organizationID, SubjectUrn: subject, OauthClientID: client.ID})
	if err != nil {
		return platformoauth.Connection{}, mapOAuthReadError(err)
	}
	return platformoauth.Connection{ID: row.ID.String(), ClientID: clientID, Subject: row.SubjectUrn, OrganizationID: row.OrganizationID, Generation: row.ActiveGeneration.String()}, nil
}

func (s *PostgresOAuthStore) AuthorizeConnection(ctx context.Context, input platformoauth.AuthorizeConnectionInput) (platformoauth.Connection, error) {
	if s == nil || s.db == nil {
		return platformoauth.Connection{}, platformoauth.ErrNotFound
	}
	if !validPKCES256Challenge(input.Grant.CodeChallenge) {
		return platformoauth.Connection{}, platformoauth.ErrPKCE
	}
	connectionID, err := uuid.Parse(input.Connection.ID)
	if err != nil {
		return platformoauth.Connection{}, platformoauth.ErrNotFound
	}
	generation, err := uuid.Parse(input.Connection.Generation)
	if err != nil {
		return platformoauth.Connection{}, platformoauth.ErrGeneration
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return platformoauth.Connection{}, fmt.Errorf("begin authorize platform mcp connection: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := platformrepo.New(tx)

	client, err := q.GetActivePlatformMCPOAuthClientByClientID(ctx, input.Connection.ClientID)
	if err != nil {
		return platformoauth.Connection{}, mapOAuthReadError(err)
	}
	if err := q.LockPlatformMCPConnectionAuthorization(ctx, platformrepo.LockPlatformMCPConnectionAuthorizationParams{
		OrganizationID: input.Connection.OrganizationID,
		SubjectUrn:     input.Connection.Subject,
		OauthClientID:  client.ClientID,
	}); err != nil {
		return platformoauth.Connection{}, fmt.Errorf("lock platform mcp connection authorization: %w", err)
	}

	connection := input.Connection
	grantConnectionID := connectionID
	grantGeneration := generation
	current, err := q.GetActivePlatformMCPConnection(ctx, platformrepo.GetActivePlatformMCPConnectionParams{
		OrganizationID: input.Connection.OrganizationID,
		SubjectUrn:     input.Connection.Subject,
		OauthClientID:  client.ID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		if _, err := q.CreatePlatformMCPConnection(ctx, platformrepo.CreatePlatformMCPConnectionParams{
			ID:               connectionID,
			OrganizationID:   input.Connection.OrganizationID,
			SubjectUrn:       input.Connection.Subject,
			OauthClientID:    client.ID,
			ActiveGeneration: generation,
		}); err != nil {
			return platformoauth.Connection{}, mapOAuthWriteError(err)
		}
	} else if err != nil {
		return platformoauth.Connection{}, mapOAuthReadError(err)
	} else {
		if err := q.RevokePlatformMCPSessionFamily(ctx, platformrepo.RevokePlatformMCPSessionFamilyParams{OrganizationID: current.OrganizationID, ConnectionID: current.ID, ConnectionGeneration: current.ActiveGeneration, RevokedAt: timestamp(input.Now)}); err != nil {
			return platformoauth.Connection{}, fmt.Errorf("revoke reauthorized platform mcp session family: %w", err)
		}
		updated, err := q.RotatePlatformMCPConnectionGeneration(ctx, platformrepo.RotatePlatformMCPConnectionGenerationParams{ConnectionID: current.ID, OrganizationID: current.OrganizationID, ActiveGeneration: generation, ReauthorizedAt: timestamp(input.Now)})
		if err != nil {
			return platformoauth.Connection{}, mapOAuthWriteError(err)
		}
		connection.ID = updated.ID.String()
		connection.Generation = updated.ActiveGeneration.String()
		grantConnectionID = updated.ID
		grantGeneration = updated.ActiveGeneration
	}

	if input.Grant.ClientID != connection.ClientID {
		return platformoauth.Connection{}, platformoauth.ErrClientMismatch
	}
	if _, err := q.CreatePlatformMCPAuthorizationGrant(ctx, platformrepo.CreatePlatformMCPAuthorizationGrantParams{
		OrganizationID:        connection.OrganizationID,
		AuthorizationCodeHash: opaqueHash(input.Grant.Code),
		OauthClientID:         client.ID,
		ConnectionID:          grantConnectionID,
		ConnectionGeneration:  grantGeneration,
		RedirectUri:           input.Grant.RedirectURI,
		CodeChallenge:         input.Grant.CodeChallenge,
		ExpiresAt:             timestamp(input.Grant.ExpiresAt),
	}); err != nil {
		return platformoauth.Connection{}, mapOAuthWriteError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return platformoauth.Connection{}, fmt.Errorf("commit authorize platform mcp connection: %w", err)
	}
	return connection, nil
}

func (s *PostgresOAuthStore) RevokeConnection(ctx context.Context, organizationID, connectionID string, now time.Time) error {
	if s == nil || s.db == nil {
		return platformoauth.ErrNotFound
	}
	id, err := uuid.Parse(connectionID)
	if err != nil {
		return platformoauth.ErrNotFound
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin revoke platform mcp connection: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := platformrepo.New(tx)
	connection, err := q.GetPlatformMCPConnectionForUpdate(ctx, platformrepo.GetPlatformMCPConnectionForUpdateParams{ID: id, OrganizationID: organizationID})
	if err != nil {
		return mapOAuthReadError(err)
	}
	if connection.RevokedAt.Valid || connection.ClientRevokedAt.Valid {
		return platformoauth.ErrRevoked
	}
	if err := q.RevokePlatformMCPSessionFamily(ctx, platformrepo.RevokePlatformMCPSessionFamilyParams{OrganizationID: organizationID, ConnectionID: id, ConnectionGeneration: connection.ActiveGeneration, RevokedAt: timestamp(now)}); err != nil {
		return fmt.Errorf("revoke platform mcp connection sessions: %w", err)
	}
	if _, err := q.RevokePlatformMCPConnection(ctx, platformrepo.RevokePlatformMCPConnectionParams{ID: id, OrganizationID: organizationID, RevokedAt: timestamp(now)}); err != nil {
		return mapOAuthWriteError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit revoke platform mcp connection: %w", err)
	}
	return nil
}

func (s *PostgresOAuthStore) IssueGrant(ctx context.Context, grant platformoauth.Grant) error {
	if s == nil || s.db == nil {
		return platformoauth.ErrNotFound
	}
	connectionID, err := uuid.Parse(grant.Connection.ID)
	if err != nil {
		return platformoauth.ErrNotFound
	}
	generation, err := uuid.Parse(grant.Connection.Generation)
	if err != nil {
		return platformoauth.ErrGeneration
	}
	connection, err := platformrepo.New(s.db).GetActivePlatformMCPConnectionByID(ctx, platformrepo.GetActivePlatformMCPConnectionByIDParams{ID: connectionID, OrganizationID: grant.Connection.OrganizationID})
	if err != nil {
		return mapOAuthReadError(err)
	}
	if connection.ClientID != grant.ClientID || connection.SubjectUrn != grant.Connection.Subject || connection.OrganizationID != grant.Connection.OrganizationID {
		return platformoauth.ErrClientMismatch
	}
	if connection.ActiveGeneration != generation {
		return platformoauth.ErrGeneration
	}
	_, err = platformrepo.New(s.db).CreatePlatformMCPAuthorizationGrant(ctx, platformrepo.CreatePlatformMCPAuthorizationGrantParams{
		OrganizationID:        grant.Connection.OrganizationID,
		AuthorizationCodeHash: opaqueHash(grant.Code),
		OauthClientID:         connection.OauthClientID,
		ConnectionID:          connectionID,
		ConnectionGeneration:  generation,
		RedirectUri:           grant.RedirectURI,
		CodeChallenge:         grant.CodeChallenge,
		ExpiresAt:             timestamp(grant.ExpiresAt),
	})
	return mapOAuthWriteError(err)
}

func (s *PostgresOAuthStore) ConsumeGrant(ctx context.Context, input platformoauth.ConsumeGrantInput) (platformoauth.Grant, error) {
	if s == nil || s.db == nil {
		return platformoauth.Grant{}, platformoauth.ErrNotFound
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return platformoauth.Grant{}, fmt.Errorf("begin consume platform mcp grant: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	q := platformrepo.New(tx)
	row, err := q.GetPlatformMCPAuthorizationGrantForConsume(ctx, platformrepo.GetPlatformMCPAuthorizationGrantForConsumeParams{OrganizationID: input.OrganizationID, AuthorizationCodeHash: opaqueHash(input.Code)})
	if err != nil {
		return platformoauth.Grant{}, mapOAuthReadError(err)
	}
	if row.ClientID != input.ClientID {
		return platformoauth.Grant{}, platformoauth.ErrClientMismatch
	}
	if row.RedirectUri != input.RedirectURI {
		return platformoauth.Grant{}, platformoauth.ErrRedirectURI
	}
	if !row.ExpiresAt.Valid || input.Now.After(row.ExpiresAt.Time) {
		return platformoauth.Grant{}, platformoauth.ErrExpired
	}
	if row.ConsumedAt.Valid || row.RevokedAt.Valid {
		return platformoauth.Grant{}, platformoauth.ErrAlreadyUsed
	}
	if row.ConnectionGeneration != row.ActiveGeneration {
		return platformoauth.Grant{}, platformoauth.ErrGeneration
	}
	if err := verifyPKCE(input.CodeVerifier, row.CodeChallenge); err != nil {
		return platformoauth.Grant{}, err
	}
	if _, err := q.ConsumePlatformMCPAuthorizationGrant(ctx, platformrepo.ConsumePlatformMCPAuthorizationGrantParams{ID: row.ID, OrganizationID: input.OrganizationID, ConsumedAt: timestamp(input.Now)}); err != nil {
		return platformoauth.Grant{}, mapOAuthWriteError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return platformoauth.Grant{}, fmt.Errorf("commit consume platform mcp grant: %w", err)
	}
	return platformoauth.Grant{Code: input.Code, ClientID: row.ClientID, RedirectURI: row.RedirectUri, CodeChallenge: row.CodeChallenge, ExpiresAt: row.ExpiresAt.Time, Connection: platformoauth.Connection{ID: row.ConnectionID.String(), ClientID: row.ClientID, Subject: row.SubjectUrn, OrganizationID: row.OrganizationID, Generation: row.ConnectionGeneration.String()}}, nil
}

func (s *PostgresOAuthStore) CreateSession(ctx context.Context, session platformoauth.Session) error {
	if s == nil || s.db == nil {
		return platformoauth.ErrNotFound
	}
	q := platformrepo.New(s.db)
	if err := validateSessionConnection(ctx, q, session); err != nil {
		return err
	}
	return s.createSessionWithQueries(ctx, q, session)
}

func (s *PostgresOAuthStore) GetSessionByRefreshHash(ctx context.Context, organizationID, refreshHash string) (platformoauth.Session, error) {
	if s == nil || s.db == nil {
		return platformoauth.Session{}, platformoauth.ErrNotFound
	}
	row, err := platformrepo.New(s.db).GetPlatformMCPSessionForRefresh(ctx, platformrepo.GetPlatformMCPSessionForRefreshParams{OrganizationID: organizationID, RefreshTokenHash: refreshHash})
	if err != nil {
		return platformoauth.Session{}, mapOAuthReadError(err)
	}
	return sessionFromRow(row), nil
}

func (s *PostgresOAuthStore) DetectRefreshReuse(ctx context.Context, organizationID, refreshHash string, now time.Time) (bool, error) {
	if s == nil || s.db == nil {
		return false, platformoauth.ErrNotFound
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("begin detect platform mcp refresh reuse: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := platformrepo.New(tx)
	row, err := q.GetPlatformMCPSessionForRefreshForUpdate(ctx, platformrepo.GetPlatformMCPSessionForRefreshForUpdateParams{OrganizationID: organizationID, RefreshTokenHash: refreshHash})
	if err != nil {
		return false, mapOAuthReadError(err)
	}
	if !row.RevokedAt.Valid {
		if err := tx.Commit(ctx); err != nil {
			return false, fmt.Errorf("commit active platform mcp refresh check: %w", err)
		}
		return false, nil
	}
	if err := q.RevokePlatformMCPSessionFamily(ctx, platformrepo.RevokePlatformMCPSessionFamilyParams{OrganizationID: organizationID, ConnectionID: row.ConnectionID, ConnectionGeneration: row.ConnectionGeneration, RevokedAt: timestamp(now)}); err != nil {
		return false, fmt.Errorf("revoke reused platform mcp session family: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("commit reused platform mcp session family: %w", err)
	}
	return true, nil
}

func (s *PostgresOAuthStore) RotateSession(ctx context.Context, input platformoauth.RotateSessionInput) (platformoauth.Session, error) {
	if s == nil || s.db == nil {
		return platformoauth.Session{}, platformoauth.ErrNotFound
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return platformoauth.Session{}, fmt.Errorf("begin rotate platform mcp session: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := platformrepo.New(tx)
	row, err := q.GetPlatformMCPSessionForRefreshForUpdate(ctx, platformrepo.GetPlatformMCPSessionForRefreshForUpdateParams{OrganizationID: input.OrganizationID, RefreshTokenHash: input.RefreshHash})
	if errors.Is(err, pgx.ErrNoRows) {
		return platformoauth.Session{}, platformoauth.ErrAlreadyUsed
	}
	if err != nil {
		return platformoauth.Session{}, fmt.Errorf("lock platform mcp session: %w", err)
	}
	if row.RevokedAt.Valid {
		if err := q.RevokePlatformMCPSessionFamily(ctx, platformrepo.RevokePlatformMCPSessionFamilyParams{OrganizationID: input.OrganizationID, ConnectionID: row.ConnectionID, ConnectionGeneration: row.ConnectionGeneration, RevokedAt: timestamp(input.Now)}); err != nil {
			return platformoauth.Session{}, fmt.Errorf("revoke reused platform mcp session family: %w", err)
		}
		if err := tx.Commit(ctx); err != nil {
			return platformoauth.Session{}, fmt.Errorf("commit reused platform mcp session family: %w", err)
		}
		return platformoauth.Session{}, platformoauth.ErrAlreadyUsed
	}
	if row.OrganizationID != input.OrganizationID || row.ClientID != input.ClientID || row.ConnectionGeneration.String() != input.Generation || row.ActiveGeneration != row.ConnectionGeneration || input.Replacement.Connection.ID != row.ConnectionID.String() {
		return platformoauth.Session{}, platformoauth.ErrGeneration
	}
	if !row.RefreshExpiresAt.Valid || input.Now.After(row.RefreshExpiresAt.Time) {
		if _, err := q.RevokePlatformMCPSession(ctx, platformrepo.RevokePlatformMCPSessionParams{ID: row.ID, OrganizationID: input.OrganizationID, RevokedAt: timestamp(input.Now)}); err != nil {
			return platformoauth.Session{}, mapOAuthWriteError(err)
		}
		if err := tx.Commit(ctx); err != nil {
			return platformoauth.Session{}, fmt.Errorf("commit expired platform mcp session: %w", err)
		}
		return platformoauth.Session{}, platformoauth.ErrExpired
	}
	if err := validateSessionConnection(ctx, q, input.Replacement); err != nil {
		return platformoauth.Session{}, err
	}
	if err := s.createSessionWithQueries(ctx, q, input.Replacement); err != nil {
		return platformoauth.Session{}, err
	}
	replacementID, err := uuid.Parse(input.Replacement.ID)
	if err != nil {
		return platformoauth.Session{}, platformoauth.ErrNotFound
	}
	if _, err := q.RotatePlatformMCPSession(ctx, platformrepo.RotatePlatformMCPSessionParams{ID: row.ID, OrganizationID: input.OrganizationID, ReplacedBySessionID: uuid.NullUUID{UUID: replacementID, Valid: true}, RotatedAt: timestamp(input.Now)}); err != nil {
		return platformoauth.Session{}, mapOAuthWriteError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return platformoauth.Session{}, fmt.Errorf("commit rotate platform mcp session: %w", err)
	}
	return platformoauth.Session{ID: row.ID.String(), ClientID: row.ClientID, JTI: row.Jti, RefreshHash: row.RefreshTokenHash, ExpiresAt: row.ExpiresAt.Time, RefreshExpiresAt: row.RefreshExpiresAt.Time, Connection: platformoauth.Connection{ID: row.ConnectionID.String(), ClientID: row.ClientID, Subject: row.SubjectUrn, OrganizationID: row.OrganizationID, Generation: row.ConnectionGeneration.String()}}, nil
}

func (s *PostgresOAuthStore) RevokeSession(ctx context.Context, organizationID, refreshHash, clientID string, now time.Time) (platformoauth.Session, error) {
	if s == nil || s.db == nil {
		return platformoauth.Session{}, platformoauth.ErrNotFound
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return platformoauth.Session{}, fmt.Errorf("begin revoke platform mcp session: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := platformrepo.New(tx)
	row, err := q.GetPlatformMCPSessionForRefreshForUpdate(ctx, platformrepo.GetPlatformMCPSessionForRefreshForUpdateParams{OrganizationID: organizationID, RefreshTokenHash: refreshHash})
	if errors.Is(err, pgx.ErrNoRows) {
		return platformoauth.Session{}, platformoauth.ErrNotFound
	}
	if err != nil {
		return platformoauth.Session{}, fmt.Errorf("lookup platform mcp session for revoke: %w", err)
	}
	if row.ClientID != clientID {
		return platformoauth.Session{}, platformoauth.ErrNotFound
	}
	if _, err := q.RevokePlatformMCPSession(ctx, platformrepo.RevokePlatformMCPSessionParams{ID: row.ID, OrganizationID: organizationID, RevokedAt: timestamp(now)}); err != nil {
		return platformoauth.Session{}, mapOAuthWriteError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return platformoauth.Session{}, fmt.Errorf("commit revoke platform mcp session: %w", err)
	}
	return platformoauth.Session{ID: row.ID.String(), ClientID: row.ClientID, JTI: row.Jti, RefreshHash: row.RefreshTokenHash, ExpiresAt: row.ExpiresAt.Time, RefreshExpiresAt: row.RefreshExpiresAt.Time, Connection: platformoauth.Connection{ID: row.ConnectionID.String(), ClientID: row.ClientID, Subject: row.SubjectUrn, OrganizationID: row.OrganizationID, Generation: row.ConnectionGeneration.String()}}, nil
}

func (s *PostgresOAuthStore) RevokeAccessSession(ctx context.Context, organizationID, jti, clientID string, now time.Time) (platformoauth.Session, error) {
	if s == nil || s.db == nil {
		return platformoauth.Session{}, platformoauth.ErrNotFound
	}
	client, err := platformrepo.New(s.db).GetActivePlatformMCPOAuthClientByClientID(ctx, clientID)
	if err != nil {
		return platformoauth.Session{}, mapOAuthReadError(err)
	}
	row, err := platformrepo.New(s.db).RevokePlatformMCPSessionByJTI(ctx, platformrepo.RevokePlatformMCPSessionByJTIParams{OrganizationID: organizationID, Jti: jti, OauthClientID: client.ID, RevokedAt: timestamp(now)})
	if err != nil {
		return platformoauth.Session{}, mapOAuthReadError(err)
	}
	return platformoauth.Session{ID: row.ID.String(), JTI: row.Jti, RefreshHash: row.RefreshTokenHash}, nil
}

func (s *PostgresOAuthStore) RotateConnectionGeneration(ctx context.Context, organizationID, connectionID, generation string, now time.Time) (platformoauth.Connection, error) {
	if s == nil || s.db == nil {
		return platformoauth.Connection{}, platformoauth.ErrNotFound
	}
	id, err := uuid.Parse(connectionID)
	if err != nil {
		return platformoauth.Connection{}, platformoauth.ErrNotFound
	}
	newGeneration, err := uuid.Parse(generation)
	if err != nil {
		return platformoauth.Connection{}, platformoauth.ErrGeneration
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return platformoauth.Connection{}, fmt.Errorf("begin rotate platform mcp generation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := platformrepo.New(tx)
	current, err := q.GetActivePlatformMCPConnectionByID(ctx, platformrepo.GetActivePlatformMCPConnectionByIDParams{ID: id, OrganizationID: organizationID})
	if err != nil {
		return platformoauth.Connection{}, mapOAuthReadError(err)
	}
	if err := q.RevokePlatformMCPSessionFamily(ctx, platformrepo.RevokePlatformMCPSessionFamilyParams{OrganizationID: organizationID, ConnectionID: id, ConnectionGeneration: current.ActiveGeneration, RevokedAt: timestamp(now)}); err != nil {
		return platformoauth.Connection{}, fmt.Errorf("revoke old platform mcp generation: %w", err)
	}
	connection, err := q.RotatePlatformMCPConnectionGeneration(ctx, platformrepo.RotatePlatformMCPConnectionGenerationParams{ConnectionID: id, OrganizationID: organizationID, ActiveGeneration: newGeneration, ReauthorizedAt: timestamp(now)})
	if err != nil {
		return platformoauth.Connection{}, mapOAuthWriteError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return platformoauth.Connection{}, fmt.Errorf("commit rotate platform mcp generation: %w", err)
	}
	return platformoauth.Connection{ID: connection.ID.String(), ClientID: current.ClientID, Subject: current.SubjectUrn, OrganizationID: current.OrganizationID, Generation: connection.ActiveGeneration.String()}, nil
}

func (s *PostgresOAuthStore) createSessionWithQueries(ctx context.Context, q *platformrepo.Queries, session platformoauth.Session) error {
	id, err := uuid.Parse(session.ID)
	if err != nil {
		return platformoauth.ErrNotFound
	}
	connectionID, err := uuid.Parse(session.Connection.ID)
	if err != nil {
		return platformoauth.ErrNotFound
	}
	generation, err := uuid.Parse(session.Connection.Generation)
	if err != nil {
		return platformoauth.ErrGeneration
	}
	connection, err := q.GetActivePlatformMCPConnectionByID(ctx, platformrepo.GetActivePlatformMCPConnectionByIDParams{ID: connectionID, OrganizationID: session.Connection.OrganizationID})
	if err != nil {
		return mapOAuthReadError(err)
	}
	_, err = q.CreatePlatformMCPSession(ctx, platformrepo.CreatePlatformMCPSessionParams{ID: id, OrganizationID: session.Connection.OrganizationID, ConnectionID: connectionID, OauthClientID: connection.OauthClientID, ConnectionGeneration: generation, Jti: session.JTI, RefreshTokenHash: session.RefreshHash, ExpiresAt: timestamp(session.ExpiresAt), RefreshExpiresAt: timestamp(session.RefreshExpiresAt)})
	return mapOAuthWriteError(err)
}

func validateSessionConnection(ctx context.Context, q *platformrepo.Queries, session platformoauth.Session) error {
	connectionID, err := uuid.Parse(session.Connection.ID)
	if err != nil {
		return platformoauth.ErrNotFound
	}
	generation, err := uuid.Parse(session.Connection.Generation)
	if err != nil {
		return platformoauth.ErrGeneration
	}
	connection, err := q.GetActivePlatformMCPConnectionByID(ctx, platformrepo.GetActivePlatformMCPConnectionByIDParams{ID: connectionID, OrganizationID: session.Connection.OrganizationID})
	if err != nil {
		return mapOAuthReadError(err)
	}
	if connection.ClientID != session.ClientID || connection.SubjectUrn != session.Connection.Subject || connection.OrganizationID != session.Connection.OrganizationID {
		return platformoauth.ErrClientMismatch
	}
	if connection.ActiveGeneration != generation {
		return platformoauth.ErrGeneration
	}
	return nil
}

func sessionFromRow(row platformrepo.GetPlatformMCPSessionForRefreshRow) platformoauth.Session {
	return platformoauth.Session{ID: row.ID.String(), ClientID: row.ClientID, JTI: row.Jti, RefreshHash: row.RefreshTokenHash, ExpiresAt: row.ExpiresAt.Time, RefreshExpiresAt: row.RefreshExpiresAt.Time, Connection: platformoauth.Connection{ID: row.ConnectionID.String(), ClientID: row.ClientID, Subject: row.SubjectUrn, OrganizationID: row.OrganizationID, Generation: row.ConnectionGeneration.String()}}
}

func text(value string) pgtype.Text {
	return pgtype.Text{String: value, Valid: value != ""}
}

func timePointer(value pgtype.Timestamptz) *time.Time {
	if !value.Valid {
		return nil
	}
	return &value.Time
}

func optionalTimestamp(value *time.Time) pgtype.Timestamptz {
	if value == nil {
		return pgtype.Timestamptz{}
	}
	return timestamp(*value)
}

func timestamp(value time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: value, Valid: true}
}

func opaqueHash(value string) string {
	hash := sha256.Sum256([]byte(value))
	return hex.EncodeToString(hash[:])
}

func verifyPKCE(verifier, challenge string) error {
	if !validPKCEVerifier(verifier) || !validPKCES256Challenge(challenge) {
		return platformoauth.ErrPKCE
	}
	hash := sha256.Sum256([]byte(verifier))
	if base64.RawURLEncoding.EncodeToString(hash[:]) == challenge {
		return nil
	}
	return platformoauth.ErrPKCE
}

func validPKCEVerifier(verifier string) bool {
	if len(verifier) < 43 || len(verifier) > 128 {
		return false
	}
	return strings.IndexFunc(verifier, func(r rune) bool {
		return (r < 'A' || r > 'Z') && (r < 'a' || r > 'z') && (r < '0' || r > '9') && !strings.ContainsRune("-._~", r)
	}) == -1
}

func validPKCES256Challenge(challenge string) bool {
	if len(challenge) != 43 {
		return false
	}
	return strings.IndexFunc(challenge, func(r rune) bool {
		return (r < 'A' || r > 'Z') && (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' && r != '_'
	}) == -1
}

func mapOAuthReadError(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return platformoauth.ErrNotFound
	}
	return fmt.Errorf("read platform oauth state: %w", err)
}

func mapOAuthWriteError(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("write platform oauth state: %w", err)
}
