//nolint:exhaustruct // Database row projections intentionally omit lifecycle fields not selected by each query.
package adminmcp

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	adminoauth "github.com/speakeasy-api/gram/server/internal/adminmcp/oauth"
	adminrepo "github.com/speakeasy-api/gram/server/internal/adminmcp/repo"
)

// PostgresOAuthStore persists the Admin-owned OAuth lifecycle. It keeps opaque
// codes and refresh tokens hashed, and uses transactions for every transition
// that locks or invalidates a grant/session family.
type PostgresOAuthStore struct {
	db *pgxpool.Pool
}

var _ adminoauth.Store = (*PostgresOAuthStore)(nil)

func NewPostgresOAuthStore(db *pgxpool.Pool) *PostgresOAuthStore {
	return &PostgresOAuthStore{db: db}
}

func (s *PostgresOAuthStore) RegisterClient(ctx context.Context, client adminoauth.Client) error {
	if s == nil || s.db == nil || client.ID == "" || client.Name == "" || len(client.RedirectURIs) == 0 {
		return adminoauth.ErrNotFound
	}
	_, err := adminrepo.New(s.db).CreateAdminMCPOAuthClient(ctx, adminrepo.CreateAdminMCPOAuthClientParams{
		ClientID:              client.ID,
		ClientSecretHash:      text(client.SecretHash),
		ClientName:            client.Name,
		RedirectUris:          client.RedirectURIs,
		ClientSecretExpiresAt: optionalTimestamp(client.SecretExpiresAt),
	})
	return mapOAuthWriteError(err)
}

func (s *PostgresOAuthStore) GetClient(ctx context.Context, clientID string) (adminoauth.Client, error) {
	if s == nil || s.db == nil {
		return adminoauth.Client{}, adminoauth.ErrNotFound
	}
	row, err := adminrepo.New(s.db).GetActiveAdminMCPOAuthClientByClientID(ctx, clientID)
	if err != nil {
		return adminoauth.Client{}, mapOAuthReadError(err)
	}
	return adminoauth.Client{ID: row.ClientID, SecretHash: row.ClientSecretHash.String, Name: row.ClientName, RedirectURIs: row.RedirectUris, SecretExpiresAt: timePointer(row.ClientSecretExpiresAt)}, nil
}

func (s *PostgresOAuthStore) RevokeClient(ctx context.Context, clientID string, now time.Time) error {
	if s == nil || s.db == nil {
		return adminoauth.ErrNotFound
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin revoke admin client: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	q := adminrepo.New(tx)
	_, err = q.RevokeAdminMCPOAuthClient(ctx, adminrepo.RevokeAdminMCPOAuthClientParams{ClientID: clientID, RevokedAt: timestamp(now)})
	if err != nil {
		return mapOAuthReadError(err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit revoke admin client: %w", err)
	}
	return nil
}

func (s *PostgresOAuthStore) RegisterConnection(ctx context.Context, connection adminoauth.Connection) error {
	if s == nil || s.db == nil {
		return adminoauth.ErrNotFound
	}
	id, err := uuid.Parse(connection.ID)
	if err != nil {
		return adminoauth.ErrNotFound
	}
	client, err := adminrepo.New(s.db).GetActiveAdminMCPOAuthClientByClientID(ctx, connection.ClientID)
	if err != nil {
		return mapOAuthReadError(err)
	}
	generation, err := uuid.Parse(connection.Generation)
	if err != nil {
		return adminoauth.ErrGeneration
	}
	_, err = adminrepo.New(s.db).CreateAdminMCPConnection(ctx, adminrepo.CreateAdminMCPConnectionParams{
		ID:               id,
		OrganizationID:   connection.OrganizationID,
		SubjectUrn:       connection.Subject,
		OauthClientID:    client.ID,
		ActiveGeneration: generation,
	})
	return mapOAuthWriteError(err)
}

func (s *PostgresOAuthStore) GetConnection(ctx context.Context, organizationID, subject, clientID string) (adminoauth.Connection, error) {
	if s == nil || s.db == nil {
		return adminoauth.Connection{}, adminoauth.ErrNotFound
	}
	client, err := adminrepo.New(s.db).GetActiveAdminMCPOAuthClientByClientID(ctx, clientID)
	if err != nil {
		return adminoauth.Connection{}, mapOAuthReadError(err)
	}
	row, err := adminrepo.New(s.db).GetActiveAdminMCPConnection(ctx, adminrepo.GetActiveAdminMCPConnectionParams{OrganizationID: organizationID, SubjectUrn: subject, OauthClientID: client.ID})
	if err != nil {
		return adminoauth.Connection{}, mapOAuthReadError(err)
	}
	return adminoauth.Connection{ID: row.ID.String(), ClientID: clientID, Subject: row.SubjectUrn, OrganizationID: row.OrganizationID, Generation: row.ActiveGeneration.String()}, nil
}

func (s *PostgresOAuthStore) AuthorizeConnection(ctx context.Context, input adminoauth.AuthorizeConnectionInput) (adminoauth.Connection, error) {
	if s == nil || s.db == nil {
		return adminoauth.Connection{}, adminoauth.ErrNotFound
	}
	connectionID, err := uuid.Parse(input.Connection.ID)
	if err != nil {
		return adminoauth.Connection{}, adminoauth.ErrNotFound
	}
	generation, err := uuid.Parse(input.Connection.Generation)
	if err != nil {
		return adminoauth.Connection{}, adminoauth.ErrGeneration
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return adminoauth.Connection{}, fmt.Errorf("begin authorize admin connection: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := adminrepo.New(tx)

	client, err := q.GetActiveAdminMCPOAuthClientByClientID(ctx, input.Connection.ClientID)
	if err != nil {
		return adminoauth.Connection{}, mapOAuthReadError(err)
	}
	if err := q.LockAdminMCPConnectionAuthorization(ctx, adminrepo.LockAdminMCPConnectionAuthorizationParams{
		OrganizationID: input.Connection.OrganizationID,
		SubjectUrn:     input.Connection.Subject,
		OauthClientID:  client.ClientID,
	}); err != nil {
		return adminoauth.Connection{}, fmt.Errorf("lock admin connection authorization: %w", err)
	}

	connection := input.Connection
	grantConnectionID := connectionID
	grantGeneration := generation
	current, err := q.GetActiveAdminMCPConnection(ctx, adminrepo.GetActiveAdminMCPConnectionParams{
		OrganizationID: input.Connection.OrganizationID,
		SubjectUrn:     input.Connection.Subject,
		OauthClientID:  client.ID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		if _, err := q.CreateAdminMCPConnection(ctx, adminrepo.CreateAdminMCPConnectionParams{
			ID:               connectionID,
			OrganizationID:   input.Connection.OrganizationID,
			SubjectUrn:       input.Connection.Subject,
			OauthClientID:    client.ID,
			ActiveGeneration: generation,
		}); err != nil {
			return adminoauth.Connection{}, mapOAuthWriteError(err)
		}
	} else if err != nil {
		return adminoauth.Connection{}, mapOAuthReadError(err)
	} else {
		if err := q.RevokeAdminMCPSessionFamily(ctx, adminrepo.RevokeAdminMCPSessionFamilyParams{OrganizationID: current.OrganizationID, ConnectionID: current.ID, ConnectionGeneration: current.ActiveGeneration, RevokedAt: timestamp(input.Now)}); err != nil {
			return adminoauth.Connection{}, fmt.Errorf("revoke reauthorized admin session family: %w", err)
		}
		updated, err := q.RotateAdminMCPConnectionGeneration(ctx, adminrepo.RotateAdminMCPConnectionGenerationParams{ConnectionID: current.ID, OrganizationID: current.OrganizationID, ActiveGeneration: generation, ReauthorizedAt: timestamp(input.Now)})
		if err != nil {
			return adminoauth.Connection{}, mapOAuthWriteError(err)
		}
		connection.ID = updated.ID.String()
		connection.Generation = updated.ActiveGeneration.String()
		grantConnectionID = updated.ID
		grantGeneration = updated.ActiveGeneration
	}

	if input.Grant.ClientID != connection.ClientID {
		return adminoauth.Connection{}, adminoauth.ErrClientMismatch
	}
	if _, err := q.CreateAdminMCPAuthorizationGrant(ctx, adminrepo.CreateAdminMCPAuthorizationGrantParams{
		OrganizationID:        connection.OrganizationID,
		AuthorizationCodeHash: opaqueHash(input.Grant.Code),
		OauthClientID:         client.ID,
		ConnectionID:          grantConnectionID,
		ConnectionGeneration:  grantGeneration,
		RedirectUri:           input.Grant.RedirectURI,
		CodeChallenge:         input.Grant.CodeChallenge,
		ExpiresAt:             timestamp(input.Grant.ExpiresAt),
	}); err != nil {
		return adminoauth.Connection{}, mapOAuthWriteError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return adminoauth.Connection{}, fmt.Errorf("commit authorize admin connection: %w", err)
	}
	return connection, nil
}

func (s *PostgresOAuthStore) RevokeConnection(ctx context.Context, organizationID, connectionID string, now time.Time) error {
	if s == nil || s.db == nil {
		return adminoauth.ErrNotFound
	}
	id, err := uuid.Parse(connectionID)
	if err != nil {
		return adminoauth.ErrNotFound
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin revoke admin connection: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := adminrepo.New(tx)
	connection, err := q.GetAdminMCPConnectionForUpdate(ctx, adminrepo.GetAdminMCPConnectionForUpdateParams{ID: id, OrganizationID: organizationID})
	if err != nil {
		return mapOAuthReadError(err)
	}
	if connection.RevokedAt.Valid || connection.ClientRevokedAt.Valid {
		return adminoauth.ErrRevoked
	}
	if err := q.RevokeAdminMCPSessionFamily(ctx, adminrepo.RevokeAdminMCPSessionFamilyParams{OrganizationID: organizationID, ConnectionID: id, ConnectionGeneration: connection.ActiveGeneration, RevokedAt: timestamp(now)}); err != nil {
		return fmt.Errorf("revoke admin connection sessions: %w", err)
	}
	if _, err := q.RevokeAdminMCPConnection(ctx, adminrepo.RevokeAdminMCPConnectionParams{ID: id, OrganizationID: organizationID, RevokedAt: timestamp(now)}); err != nil {
		return mapOAuthWriteError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit revoke admin connection: %w", err)
	}
	return nil
}

func (s *PostgresOAuthStore) IssueGrant(ctx context.Context, grant adminoauth.Grant) error {
	if s == nil || s.db == nil {
		return adminoauth.ErrNotFound
	}
	connectionID, err := uuid.Parse(grant.Connection.ID)
	if err != nil {
		return adminoauth.ErrNotFound
	}
	generation, err := uuid.Parse(grant.Connection.Generation)
	if err != nil {
		return adminoauth.ErrGeneration
	}
	connection, err := adminrepo.New(s.db).GetActiveAdminMCPConnectionByID(ctx, adminrepo.GetActiveAdminMCPConnectionByIDParams{ID: connectionID, OrganizationID: grant.Connection.OrganizationID})
	if err != nil {
		return mapOAuthReadError(err)
	}
	if connection.ClientID != grant.ClientID || connection.SubjectUrn != grant.Connection.Subject || connection.OrganizationID != grant.Connection.OrganizationID {
		return adminoauth.ErrClientMismatch
	}
	if connection.ActiveGeneration != generation {
		return adminoauth.ErrGeneration
	}
	_, err = adminrepo.New(s.db).CreateAdminMCPAuthorizationGrant(ctx, adminrepo.CreateAdminMCPAuthorizationGrantParams{
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

func (s *PostgresOAuthStore) ConsumeGrant(ctx context.Context, input adminoauth.ConsumeGrantInput) (adminoauth.Grant, error) {
	if s == nil || s.db == nil {
		return adminoauth.Grant{}, adminoauth.ErrNotFound
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return adminoauth.Grant{}, fmt.Errorf("begin consume admin grant: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	q := adminrepo.New(tx)
	row, err := q.GetAdminMCPAuthorizationGrantForConsume(ctx, adminrepo.GetAdminMCPAuthorizationGrantForConsumeParams{OrganizationID: input.OrganizationID, AuthorizationCodeHash: opaqueHash(input.Code)})
	if err != nil {
		return adminoauth.Grant{}, mapOAuthReadError(err)
	}
	if row.ClientID != input.ClientID {
		return adminoauth.Grant{}, adminoauth.ErrClientMismatch
	}
	if row.RedirectUri != input.RedirectURI {
		return adminoauth.Grant{}, adminoauth.ErrRedirectURI
	}
	if !row.ExpiresAt.Valid || input.Now.After(row.ExpiresAt.Time) {
		return adminoauth.Grant{}, adminoauth.ErrExpired
	}
	if row.ConsumedAt.Valid || row.RevokedAt.Valid {
		return adminoauth.Grant{}, adminoauth.ErrAlreadyUsed
	}
	if row.ConnectionGeneration != row.ActiveGeneration {
		return adminoauth.Grant{}, adminoauth.ErrGeneration
	}
	if err := verifyPKCE(input.CodeVerifier, row.CodeChallenge); err != nil {
		return adminoauth.Grant{}, err
	}
	if _, err := q.ConsumeAdminMCPAuthorizationGrant(ctx, adminrepo.ConsumeAdminMCPAuthorizationGrantParams{ID: row.ID, OrganizationID: input.OrganizationID, ConsumedAt: timestamp(input.Now)}); err != nil {
		return adminoauth.Grant{}, mapOAuthWriteError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return adminoauth.Grant{}, fmt.Errorf("commit consume admin grant: %w", err)
	}
	return adminoauth.Grant{Code: input.Code, ClientID: row.ClientID, RedirectURI: row.RedirectUri, CodeChallenge: row.CodeChallenge, ExpiresAt: row.ExpiresAt.Time, Connection: adminoauth.Connection{ID: row.ConnectionID.String(), ClientID: row.ClientID, Subject: row.SubjectUrn, OrganizationID: row.OrganizationID, Generation: row.ConnectionGeneration.String()}}, nil
}

func (s *PostgresOAuthStore) CreateSession(ctx context.Context, session adminoauth.Session) error {
	if s == nil || s.db == nil {
		return adminoauth.ErrNotFound
	}
	q := adminrepo.New(s.db)
	if err := validateSessionConnection(ctx, q, session); err != nil {
		return err
	}
	return s.createSessionWithQueries(ctx, q, session)
}

func (s *PostgresOAuthStore) GetSessionByRefreshHash(ctx context.Context, organizationID, refreshHash string) (adminoauth.Session, error) {
	if s == nil || s.db == nil {
		return adminoauth.Session{}, adminoauth.ErrNotFound
	}
	row, err := adminrepo.New(s.db).GetAdminMCPSessionForRefresh(ctx, adminrepo.GetAdminMCPSessionForRefreshParams{OrganizationID: organizationID, RefreshTokenHash: refreshHash})
	if err != nil {
		return adminoauth.Session{}, mapOAuthReadError(err)
	}
	return sessionFromRow(row), nil
}

func (s *PostgresOAuthStore) DetectRefreshReuse(ctx context.Context, organizationID, refreshHash string, now time.Time) (bool, error) {
	if s == nil || s.db == nil {
		return false, adminoauth.ErrNotFound
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("begin detect admin refresh reuse: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := adminrepo.New(tx)
	row, err := q.GetAdminMCPSessionForRefreshForUpdate(ctx, adminrepo.GetAdminMCPSessionForRefreshForUpdateParams{OrganizationID: organizationID, RefreshTokenHash: refreshHash})
	if err != nil {
		return false, mapOAuthReadError(err)
	}
	if !row.RevokedAt.Valid {
		if err := tx.Commit(ctx); err != nil {
			return false, fmt.Errorf("commit active admin refresh check: %w", err)
		}
		return false, nil
	}
	if err := q.RevokeAdminMCPSessionFamily(ctx, adminrepo.RevokeAdminMCPSessionFamilyParams{OrganizationID: organizationID, ConnectionID: row.ConnectionID, ConnectionGeneration: row.ConnectionGeneration, RevokedAt: timestamp(now)}); err != nil {
		return false, fmt.Errorf("revoke reused admin session family: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("commit reused admin session family: %w", err)
	}
	return true, nil
}

func (s *PostgresOAuthStore) RotateSession(ctx context.Context, input adminoauth.RotateSessionInput) (adminoauth.Session, error) {
	if s == nil || s.db == nil {
		return adminoauth.Session{}, adminoauth.ErrNotFound
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return adminoauth.Session{}, fmt.Errorf("begin rotate admin session: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := adminrepo.New(tx)
	row, err := q.GetAdminMCPSessionForRefreshForUpdate(ctx, adminrepo.GetAdminMCPSessionForRefreshForUpdateParams{OrganizationID: input.OrganizationID, RefreshTokenHash: input.RefreshHash})
	if errors.Is(err, pgx.ErrNoRows) {
		return adminoauth.Session{}, adminoauth.ErrAlreadyUsed
	}
	if err != nil {
		return adminoauth.Session{}, fmt.Errorf("lock admin session: %w", err)
	}
	if row.RevokedAt.Valid {
		if err := q.RevokeAdminMCPSessionFamily(ctx, adminrepo.RevokeAdminMCPSessionFamilyParams{OrganizationID: input.OrganizationID, ConnectionID: row.ConnectionID, ConnectionGeneration: row.ConnectionGeneration, RevokedAt: timestamp(input.Now)}); err != nil {
			return adminoauth.Session{}, fmt.Errorf("revoke reused admin session family: %w", err)
		}
		if err := tx.Commit(ctx); err != nil {
			return adminoauth.Session{}, fmt.Errorf("commit reused admin session family: %w", err)
		}
		return adminoauth.Session{}, adminoauth.ErrAlreadyUsed
	}
	if row.OrganizationID != input.OrganizationID || row.ClientID != input.ClientID || row.ConnectionGeneration.String() != input.Generation || row.ActiveGeneration != row.ConnectionGeneration || input.Replacement.Connection.ID != row.ConnectionID.String() {
		return adminoauth.Session{}, adminoauth.ErrGeneration
	}
	if !row.RefreshExpiresAt.Valid || input.Now.After(row.RefreshExpiresAt.Time) {
		if _, err := q.RevokeAdminMCPSession(ctx, adminrepo.RevokeAdminMCPSessionParams{ID: row.ID, OrganizationID: input.OrganizationID, RevokedAt: timestamp(input.Now)}); err != nil {
			return adminoauth.Session{}, mapOAuthWriteError(err)
		}
		if err := tx.Commit(ctx); err != nil {
			return adminoauth.Session{}, fmt.Errorf("commit expired admin session: %w", err)
		}
		return adminoauth.Session{}, adminoauth.ErrExpired
	}
	if err := validateSessionConnection(ctx, q, input.Replacement); err != nil {
		return adminoauth.Session{}, err
	}
	if err := s.createSessionWithQueries(ctx, q, input.Replacement); err != nil {
		return adminoauth.Session{}, err
	}
	replacementID, err := uuid.Parse(input.Replacement.ID)
	if err != nil {
		return adminoauth.Session{}, adminoauth.ErrNotFound
	}
	if _, err := q.RotateAdminMCPSession(ctx, adminrepo.RotateAdminMCPSessionParams{ID: row.ID, OrganizationID: input.OrganizationID, ReplacedBySessionID: uuid.NullUUID{UUID: replacementID, Valid: true}, RotatedAt: timestamp(input.Now)}); err != nil {
		return adminoauth.Session{}, mapOAuthWriteError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return adminoauth.Session{}, fmt.Errorf("commit rotate admin session: %w", err)
	}
	return adminoauth.Session{ID: row.ID.String(), ClientID: row.ClientID, JTI: row.Jti, RefreshHash: row.RefreshTokenHash, ExpiresAt: row.ExpiresAt.Time, RefreshExpiresAt: row.RefreshExpiresAt.Time, Connection: adminoauth.Connection{ID: row.ConnectionID.String(), ClientID: row.ClientID, Subject: row.SubjectUrn, OrganizationID: row.OrganizationID, Generation: row.ConnectionGeneration.String()}}, nil
}

func (s *PostgresOAuthStore) RevokeSession(ctx context.Context, organizationID, refreshHash, clientID string, now time.Time) (adminoauth.Session, error) {
	if s == nil || s.db == nil {
		return adminoauth.Session{}, adminoauth.ErrNotFound
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return adminoauth.Session{}, fmt.Errorf("begin revoke admin session: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := adminrepo.New(tx)
	row, err := q.GetAdminMCPSessionForRefreshForUpdate(ctx, adminrepo.GetAdminMCPSessionForRefreshForUpdateParams{OrganizationID: organizationID, RefreshTokenHash: refreshHash})
	if errors.Is(err, pgx.ErrNoRows) {
		return adminoauth.Session{}, adminoauth.ErrNotFound
	}
	if err != nil {
		return adminoauth.Session{}, fmt.Errorf("lookup admin session for revoke: %w", err)
	}
	if row.ClientID != clientID {
		return adminoauth.Session{}, adminoauth.ErrNotFound
	}
	if _, err := q.RevokeAdminMCPSession(ctx, adminrepo.RevokeAdminMCPSessionParams{ID: row.ID, OrganizationID: organizationID, RevokedAt: timestamp(now)}); err != nil {
		return adminoauth.Session{}, mapOAuthWriteError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return adminoauth.Session{}, fmt.Errorf("commit revoke admin session: %w", err)
	}
	return adminoauth.Session{ID: row.ID.String(), ClientID: row.ClientID, JTI: row.Jti, RefreshHash: row.RefreshTokenHash, ExpiresAt: row.ExpiresAt.Time, RefreshExpiresAt: row.RefreshExpiresAt.Time, Connection: adminoauth.Connection{ID: row.ConnectionID.String(), ClientID: row.ClientID, Subject: row.SubjectUrn, OrganizationID: row.OrganizationID, Generation: row.ConnectionGeneration.String()}}, nil
}

func (s *PostgresOAuthStore) RevokeAccessSession(ctx context.Context, organizationID, jti, clientID string, now time.Time) (adminoauth.Session, error) {
	if s == nil || s.db == nil {
		return adminoauth.Session{}, adminoauth.ErrNotFound
	}
	client, err := adminrepo.New(s.db).GetActiveAdminMCPOAuthClientByClientID(ctx, clientID)
	if err != nil {
		return adminoauth.Session{}, mapOAuthReadError(err)
	}
	row, err := adminrepo.New(s.db).RevokeAdminMCPSessionByJTI(ctx, adminrepo.RevokeAdminMCPSessionByJTIParams{OrganizationID: organizationID, Jti: jti, OauthClientID: client.ID, RevokedAt: timestamp(now)})
	if err != nil {
		return adminoauth.Session{}, mapOAuthReadError(err)
	}
	return adminoauth.Session{ID: row.ID.String(), JTI: row.Jti, RefreshHash: row.RefreshTokenHash}, nil
}

func (s *PostgresOAuthStore) RotateConnectionGeneration(ctx context.Context, organizationID, connectionID, generation string, now time.Time) (adminoauth.Connection, error) {
	if s == nil || s.db == nil {
		return adminoauth.Connection{}, adminoauth.ErrNotFound
	}
	id, err := uuid.Parse(connectionID)
	if err != nil {
		return adminoauth.Connection{}, adminoauth.ErrNotFound
	}
	newGeneration, err := uuid.Parse(generation)
	if err != nil {
		return adminoauth.Connection{}, adminoauth.ErrGeneration
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return adminoauth.Connection{}, fmt.Errorf("begin rotate admin generation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := adminrepo.New(tx)
	current, err := q.GetActiveAdminMCPConnectionByID(ctx, adminrepo.GetActiveAdminMCPConnectionByIDParams{ID: id, OrganizationID: organizationID})
	if err != nil {
		return adminoauth.Connection{}, mapOAuthReadError(err)
	}
	if err := q.RevokeAdminMCPSessionFamily(ctx, adminrepo.RevokeAdminMCPSessionFamilyParams{OrganizationID: organizationID, ConnectionID: id, ConnectionGeneration: current.ActiveGeneration, RevokedAt: timestamp(now)}); err != nil {
		return adminoauth.Connection{}, fmt.Errorf("revoke old admin generation: %w", err)
	}
	connection, err := q.RotateAdminMCPConnectionGeneration(ctx, adminrepo.RotateAdminMCPConnectionGenerationParams{ConnectionID: id, OrganizationID: organizationID, ActiveGeneration: newGeneration, ReauthorizedAt: timestamp(now)})
	if err != nil {
		return adminoauth.Connection{}, mapOAuthWriteError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return adminoauth.Connection{}, fmt.Errorf("commit rotate admin generation: %w", err)
	}
	return adminoauth.Connection{ID: connection.ID.String(), ClientID: current.ClientID, Subject: current.SubjectUrn, OrganizationID: current.OrganizationID, Generation: connection.ActiveGeneration.String()}, nil
}

func (s *PostgresOAuthStore) createSessionWithQueries(ctx context.Context, q *adminrepo.Queries, session adminoauth.Session) error {
	id, err := uuid.Parse(session.ID)
	if err != nil {
		return adminoauth.ErrNotFound
	}
	connectionID, err := uuid.Parse(session.Connection.ID)
	if err != nil {
		return adminoauth.ErrNotFound
	}
	generation, err := uuid.Parse(session.Connection.Generation)
	if err != nil {
		return adminoauth.ErrGeneration
	}
	connection, err := q.GetActiveAdminMCPConnectionByID(ctx, adminrepo.GetActiveAdminMCPConnectionByIDParams{ID: connectionID, OrganizationID: session.Connection.OrganizationID})
	if err != nil {
		return mapOAuthReadError(err)
	}
	_, err = q.CreateAdminMCPSession(ctx, adminrepo.CreateAdminMCPSessionParams{ID: id, OrganizationID: session.Connection.OrganizationID, ConnectionID: connectionID, OauthClientID: connection.OauthClientID, ConnectionGeneration: generation, Jti: session.JTI, RefreshTokenHash: session.RefreshHash, ExpiresAt: timestamp(session.ExpiresAt), RefreshExpiresAt: timestamp(session.RefreshExpiresAt)})
	return mapOAuthWriteError(err)
}

func validateSessionConnection(ctx context.Context, q *adminrepo.Queries, session adminoauth.Session) error {
	connectionID, err := uuid.Parse(session.Connection.ID)
	if err != nil {
		return adminoauth.ErrNotFound
	}
	generation, err := uuid.Parse(session.Connection.Generation)
	if err != nil {
		return adminoauth.ErrGeneration
	}
	connection, err := q.GetActiveAdminMCPConnectionByID(ctx, adminrepo.GetActiveAdminMCPConnectionByIDParams{ID: connectionID, OrganizationID: session.Connection.OrganizationID})
	if err != nil {
		return mapOAuthReadError(err)
	}
	if connection.ClientID != session.ClientID || connection.SubjectUrn != session.Connection.Subject || connection.OrganizationID != session.Connection.OrganizationID {
		return adminoauth.ErrClientMismatch
	}
	if connection.ActiveGeneration != generation {
		return adminoauth.ErrGeneration
	}
	return nil
}

func sessionFromRow(row adminrepo.GetAdminMCPSessionForRefreshRow) adminoauth.Session {
	return adminoauth.Session{ID: row.ID.String(), ClientID: row.ClientID, JTI: row.Jti, RefreshHash: row.RefreshTokenHash, ExpiresAt: row.ExpiresAt.Time, RefreshExpiresAt: row.RefreshExpiresAt.Time, Connection: adminoauth.Connection{ID: row.ConnectionID.String(), ClientID: row.ClientID, Subject: row.SubjectUrn, OrganizationID: row.OrganizationID, Generation: row.ConnectionGeneration.String()}}
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
	hash := sha256.Sum256([]byte(verifier))
	if base64.RawURLEncoding.EncodeToString(hash[:]) == challenge {
		return nil
	}
	return adminoauth.ErrPKCE
}

func mapOAuthReadError(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return adminoauth.ErrNotFound
	}
	return fmt.Errorf("read admin oauth state: %w", err)
}

func mapOAuthWriteError(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("write admin oauth state: %w", err)
}
