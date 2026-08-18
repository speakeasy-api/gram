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
	db        *pgxpool.Pool
	telemetry OAuthTelemetry
}

var _ platformoauth.Store = (*PostgresOAuthStore)(nil)

func NewPostgresOAuthStore(db *pgxpool.Pool) *PostgresOAuthStore {
	return &PostgresOAuthStore{db: db, telemetry: noopOAuthTelemetry{}}
}

func (s *PostgresOAuthStore) WithTelemetry(telemetry OAuthTelemetry) *PostgresOAuthStore {
	if telemetry != nil {
		s.telemetry = telemetry
	}
	return s
}

func (s *PostgresOAuthStore) recordTerminalTransition(ctx context.Context, reason platformoauth.ReauthorizationReason) {
	if s != nil && s.telemetry != nil {
		s.telemetry.RecordTerminalTransition(ctx, reason)
	}
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
	client, err := q.GetPlatformMCPOAuthClientForUpdate(ctx, clientID)
	if err != nil {
		return mapOAuthReadError(err)
	}
	if client.RevokedAt.Valid {
		return platformoauth.ErrRevoked
	}
	connections, err := q.ListPlatformMCPClientConnectionsForUpdate(ctx, client.ID)
	if err != nil {
		return fmt.Errorf("lock platform mcp client connections: %w", err)
	}
	for _, connection := range connections {
		if err := markConnectionTerminal(ctx, q, connection.OrganizationID, connection.ID, connection.ActiveGeneration, platformoauth.ReauthorizationReasonClientRevoked, now); err != nil {
			return err
		}
	}
	if _, err = q.RevokePlatformMCPOAuthClient(ctx, platformrepo.RevokePlatformMCPOAuthClientParams{ClientID: clientID, RevokedAt: timestamp(now)}); err != nil {
		return mapOAuthWriteError(err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit revoke platform mcp client: %w", err)
	}
	for range connections {
		s.recordTerminalTransition(ctx, platformoauth.ReauthorizationReasonClientRevoked)
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
	if connection.AuthorizationExpiresAt.IsZero() {
		return platformoauth.ErrExpired
	}
	_, err = platformrepo.New(s.db).CreatePlatformMCPConnection(ctx, platformrepo.CreatePlatformMCPConnectionParams{
		ID:                     id,
		OrganizationID:         connection.OrganizationID,
		SubjectUrn:             connection.Subject,
		OauthClientID:          client.ID,
		ActiveGeneration:       generation,
		AuthorizationExpiresAt: timestamp(connection.AuthorizationExpiresAt),
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
	return connectionFromRow(row, clientID), nil
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
	if connection.AuthorizationExpiresAt.IsZero() {
		connection.AuthorizationExpiresAt = input.Now.Add(platformoauth.AuthorizationLifetime)
	}
	grantConnectionID := connectionID
	grantGeneration := generation
	current, err := q.GetActivePlatformMCPConnection(ctx, platformrepo.GetActivePlatformMCPConnectionParams{
		OrganizationID: input.Connection.OrganizationID,
		SubjectUrn:     input.Connection.Subject,
		OauthClientID:  client.ID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		if _, err := q.CreatePlatformMCPConnection(ctx, platformrepo.CreatePlatformMCPConnectionParams{
			ID:                     connectionID,
			OrganizationID:         input.Connection.OrganizationID,
			SubjectUrn:             input.Connection.Subject,
			OauthClientID:          client.ID,
			ActiveGeneration:       generation,
			AuthorizationExpiresAt: timestamp(connection.AuthorizationExpiresAt),
		}); err != nil {
			return platformoauth.Connection{}, mapOAuthWriteError(err)
		}
	} else if err != nil {
		return platformoauth.Connection{}, mapOAuthReadError(err)
	} else {
		updated, err := q.RotatePlatformMCPConnectionGeneration(ctx, platformrepo.RotatePlatformMCPConnectionGenerationParams{ConnectionID: current.ID, OrganizationID: current.OrganizationID, ActiveGeneration: generation, ReauthorizedAt: timestamp(input.Now), AuthorizationExpiresAt: timestamp(connection.AuthorizationExpiresAt)})
		if err != nil {
			return platformoauth.Connection{}, mapOAuthWriteError(err)
		}
		if err := q.RevokePlatformMCPSessionFamily(ctx, platformrepo.RevokePlatformMCPSessionFamilyParams{OrganizationID: current.OrganizationID, ConnectionID: current.ID, ConnectionGeneration: current.ActiveGeneration, RevokedAt: timestamp(input.Now)}); err != nil {
			return platformoauth.Connection{}, fmt.Errorf("revoke reauthorized platform mcp session family: %w", err)
		}
		connection = connectionFromRow(updated, input.Connection.ClientID)
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
	if !connection.ReauthorizationRequiredAt.Valid {
		s.recordTerminalTransition(ctx, platformoauth.ReauthorizationReasonConnectionRevoked)
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

func (s *PostgresOAuthStore) ValidateGrant(ctx context.Context, input platformoauth.ValidateGrantInput) (platformoauth.Grant, error) {
	if s == nil || s.db == nil {
		return platformoauth.Grant{}, platformoauth.ErrNotFound
	}
	row, err := platformrepo.New(s.db).GetPlatformMCPAuthorizationGrantForValidation(ctx, platformrepo.GetPlatformMCPAuthorizationGrantForValidationParams{OrganizationID: input.OrganizationID, AuthorizationCodeHash: opaqueHash(input.Code)})
	if err != nil {
		return platformoauth.Grant{}, mapOAuthReadError(err)
	}
	return validateGrantRow(input, grantRow{
		ID: row.ID, OrganizationID: row.OrganizationID, ConnectionID: row.ConnectionID, ConnectionGeneration: row.ConnectionGeneration,
		RedirectURI: row.RedirectUri, CodeChallenge: row.CodeChallenge, ExpiresAt: row.ExpiresAt, ConsumedAt: row.ConsumedAt,
		RevokedAt: row.RevokedAt, Subject: row.SubjectUrn, ActiveGeneration: row.ActiveGeneration, ClientID: row.ClientID,
		AuthorizationExpiresAt: row.EffectiveAuthorizationExpiresAt,
	})
}

func (s *PostgresOAuthStore) ExchangeGrant(ctx context.Context, input platformoauth.ExchangeGrantInput) (platformoauth.Grant, error) {
	if s == nil || s.db == nil {
		return platformoauth.Grant{}, platformoauth.ErrNotFound
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return platformoauth.Grant{}, fmt.Errorf("begin exchange platform mcp grant: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	q := platformrepo.New(tx)
	row, err := q.GetPlatformMCPAuthorizationGrantForConsume(ctx, platformrepo.GetPlatformMCPAuthorizationGrantForConsumeParams{OrganizationID: input.OrganizationID, AuthorizationCodeHash: opaqueHash(input.Code)})
	if err != nil {
		return platformoauth.Grant{}, mapOAuthReadError(err)
	}
	grant, err := validateGrantRow(input.ConsumeGrantInput, grantRow{
		ID: row.ID, OrganizationID: row.OrganizationID, ConnectionID: row.ConnectionID, ConnectionGeneration: row.ConnectionGeneration,
		RedirectURI: row.RedirectUri, CodeChallenge: row.CodeChallenge, ExpiresAt: row.ExpiresAt, ConsumedAt: row.ConsumedAt,
		RevokedAt: row.RevokedAt, Subject: row.SubjectUrn, ActiveGeneration: row.ActiveGeneration, ClientID: row.ClientID,
		AuthorizationExpiresAt: row.EffectiveAuthorizationExpiresAt,
	})
	if err != nil {
		return platformoauth.Grant{}, err
	}
	if input.Session.ClientID != grant.ClientID || input.Session.Connection.ID != grant.Connection.ID || input.Session.Connection.ClientID != grant.Connection.ClientID || input.Session.Connection.Subject != grant.Connection.Subject || input.Session.Connection.OrganizationID != grant.Connection.OrganizationID {
		return platformoauth.Grant{}, platformoauth.ErrClientMismatch
	}
	if input.Session.Connection.Generation != grant.Connection.Generation {
		return platformoauth.Grant{}, platformoauth.ErrGeneration
	}
	if input.Session.ExpiresAt.After(grant.Connection.AuthorizationExpiresAt) || input.Session.RefreshExpiresAt.After(grant.Connection.AuthorizationExpiresAt) {
		return platformoauth.Grant{}, platformoauth.ErrExpired
	}
	if err := s.createSessionWithQueries(ctx, q, input.Session); err != nil {
		return platformoauth.Grant{}, err
	}
	if _, err := q.ConsumePlatformMCPAuthorizationGrant(ctx, platformrepo.ConsumePlatformMCPAuthorizationGrantParams{ID: row.ID, OrganizationID: input.OrganizationID, ConsumedAt: timestamp(input.Now)}); err != nil {
		return platformoauth.Grant{}, mapOAuthWriteError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return platformoauth.Grant{}, fmt.Errorf("commit exchange platform mcp grant: %w", err)
	}
	return grant, nil
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
	grant, err := validateGrantRow(input, grantRow{
		ID: row.ID, OrganizationID: row.OrganizationID, ConnectionID: row.ConnectionID, ConnectionGeneration: row.ConnectionGeneration,
		RedirectURI: row.RedirectUri, CodeChallenge: row.CodeChallenge, ExpiresAt: row.ExpiresAt, ConsumedAt: row.ConsumedAt,
		RevokedAt: row.RevokedAt, Subject: row.SubjectUrn, ActiveGeneration: row.ActiveGeneration, ClientID: row.ClientID,
		AuthorizationExpiresAt: row.EffectiveAuthorizationExpiresAt,
	})
	if err != nil {
		return platformoauth.Grant{}, err
	}
	if _, err := q.ConsumePlatformMCPAuthorizationGrant(ctx, platformrepo.ConsumePlatformMCPAuthorizationGrantParams{ID: row.ID, OrganizationID: input.OrganizationID, ConsumedAt: timestamp(input.Now)}); err != nil {
		return platformoauth.Grant{}, mapOAuthWriteError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return platformoauth.Grant{}, fmt.Errorf("commit consume platform mcp grant: %w", err)
	}
	return grant, nil
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
	terminalized := false
	if !row.ReauthorizationRequiredAt.Valid {
		err := markConnectionTerminal(ctx, q, row.OrganizationID, row.ConnectionID, row.ConnectionGeneration, platformoauth.ReauthorizationReasonRefreshReuse, now)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return false, err
		}
		terminalized = err == nil
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("commit reused platform mcp session family: %w", err)
	}
	if terminalized {
		s.recordTerminalTransition(ctx, platformoauth.ReauthorizationReasonRefreshReuse)
	}
	return true, nil
}

func (s *PostgresOAuthStore) PrepareRefresh(ctx context.Context, input platformoauth.PrepareRefreshInput) (platformoauth.Session, error) {
	if s == nil || s.db == nil {
		return platformoauth.Session{}, platformoauth.ErrNotFound
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return platformoauth.Session{}, fmt.Errorf("begin prepare platform mcp refresh: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := platformrepo.New(tx)
	row, err := q.GetPlatformMCPSessionForRefreshForUpdate(ctx, platformrepo.GetPlatformMCPSessionForRefreshForUpdateParams{OrganizationID: input.OrganizationID, RefreshTokenHash: input.RefreshHash})
	if err != nil {
		return platformoauth.Session{}, mapOAuthReadError(err)
	}
	if row.ClientID != input.ClientID {
		return platformoauth.Session{}, platformoauth.ErrClientMismatch
	}
	if row.RevokedAt.Valid {
		if !row.RotatedAt.Valid {
			return platformoauth.Session{}, platformoauth.ErrRevoked
		}
		if err := markConnectionTerminal(ctx, q, row.OrganizationID, row.ConnectionID, row.ConnectionGeneration, platformoauth.ReauthorizationReasonRefreshReuse, input.Now); err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return platformoauth.Session{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return platformoauth.Session{}, fmt.Errorf("commit reused platform mcp refresh: %w", err)
		}
		s.recordTerminalTransition(ctx, platformoauth.ReauthorizationReasonRefreshReuse)
		return platformoauth.Session{}, platformoauth.ErrAlreadyUsed
	}
	if row.ConnectionRevokedAt.Valid || row.ClientRevokedAt.Valid || row.ReauthorizationRequiredAt.Valid {
		return platformoauth.Session{}, platformoauth.ErrRevoked
	}
	if row.ActiveGeneration != row.ConnectionGeneration {
		return platformoauth.Session{}, platformoauth.ErrGeneration
	}
	if !row.EffectiveAuthorizationExpiresAt.Valid || !input.Now.Before(row.EffectiveAuthorizationExpiresAt.Time) {
		if err := markConnectionTerminal(ctx, q, row.OrganizationID, row.ConnectionID, row.ConnectionGeneration, platformoauth.ReauthorizationReasonAuthorizationExpired, input.Now); err != nil {
			return platformoauth.Session{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return platformoauth.Session{}, fmt.Errorf("commit expired platform mcp authorization: %w", err)
		}
		s.recordTerminalTransition(ctx, platformoauth.ReauthorizationReasonAuthorizationExpired)
		return platformoauth.Session{}, platformoauth.ErrExpired
	}
	if !row.RefreshExpiresAt.Valid || !input.Now.Before(row.RefreshExpiresAt.Time) {
		if err := markConnectionTerminal(ctx, q, row.OrganizationID, row.ConnectionID, row.ConnectionGeneration, platformoauth.ReauthorizationReasonRefreshIdleExpired, input.Now); err != nil {
			return platformoauth.Session{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return platformoauth.Session{}, fmt.Errorf("commit expired platform mcp refresh: %w", err)
		}
		s.recordTerminalTransition(ctx, platformoauth.ReauthorizationReasonRefreshIdleExpired)
		return platformoauth.Session{}, platformoauth.ErrExpired
	}
	session := sessionFromRefreshRow(row)
	if err := tx.Commit(ctx); err != nil {
		return platformoauth.Session{}, fmt.Errorf("commit prepared platform mcp refresh: %w", err)
	}
	return session, nil
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
		if !row.RotatedAt.Valid {
			return platformoauth.Session{}, platformoauth.ErrRevoked
		}
		if err := markConnectionTerminal(ctx, q, row.OrganizationID, row.ConnectionID, row.ConnectionGeneration, platformoauth.ReauthorizationReasonRefreshReuse, input.Now); err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return platformoauth.Session{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return platformoauth.Session{}, fmt.Errorf("commit reused platform mcp session family: %w", err)
		}
		s.recordTerminalTransition(ctx, platformoauth.ReauthorizationReasonRefreshReuse)
		return platformoauth.Session{}, platformoauth.ErrAlreadyUsed
	}
	if row.OrganizationID != input.OrganizationID || row.ClientID != input.ClientID || row.ConnectionGeneration.String() != input.Generation || row.ActiveGeneration != row.ConnectionGeneration || input.Replacement.Connection.ID != row.ConnectionID.String() || input.Replacement.Connection.Generation != row.ConnectionGeneration.String() {
		return platformoauth.Session{}, platformoauth.ErrGeneration
	}
	if row.ConnectionRevokedAt.Valid || row.ClientRevokedAt.Valid || row.ReauthorizationRequiredAt.Valid {
		return platformoauth.Session{}, platformoauth.ErrRevoked
	}
	if !row.EffectiveAuthorizationExpiresAt.Valid || !input.Now.Before(row.EffectiveAuthorizationExpiresAt.Time) {
		if err := markConnectionTerminal(ctx, q, row.OrganizationID, row.ConnectionID, row.ConnectionGeneration, platformoauth.ReauthorizationReasonAuthorizationExpired, input.Now); err != nil {
			return platformoauth.Session{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return platformoauth.Session{}, fmt.Errorf("commit expired platform mcp authorization: %w", err)
		}
		s.recordTerminalTransition(ctx, platformoauth.ReauthorizationReasonAuthorizationExpired)
		return platformoauth.Session{}, platformoauth.ErrExpired
	}
	if !row.RefreshExpiresAt.Valid || !input.Now.Before(row.RefreshExpiresAt.Time) {
		if err := markConnectionTerminal(ctx, q, row.OrganizationID, row.ConnectionID, row.ConnectionGeneration, platformoauth.ReauthorizationReasonRefreshIdleExpired, input.Now); err != nil {
			return platformoauth.Session{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return platformoauth.Session{}, fmt.Errorf("commit expired platform mcp session: %w", err)
		}
		s.recordTerminalTransition(ctx, platformoauth.ReauthorizationReasonRefreshIdleExpired)
		return platformoauth.Session{}, platformoauth.ErrExpired
	}
	if input.Replacement.ExpiresAt.After(row.EffectiveAuthorizationExpiresAt.Time) || input.Replacement.RefreshExpiresAt.After(row.EffectiveAuthorizationExpiresAt.Time) {
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

func (s *PostgresOAuthStore) MarkAuthorizationLost(ctx context.Context, organizationID, connectionID, generation string, now time.Time) error {
	if s == nil || s.db == nil {
		return platformoauth.ErrNotFound
	}
	id, err := uuid.Parse(connectionID)
	if err != nil {
		return platformoauth.ErrNotFound
	}
	activeGeneration, err := uuid.Parse(generation)
	if err != nil {
		return platformoauth.ErrGeneration
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin mark platform mcp authorization lost: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := markConnectionTerminal(ctx, platformrepo.New(tx), organizationID, id, activeGeneration, platformoauth.ReauthorizationReasonAuthorizationLost, now); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit mark platform mcp authorization lost: %w", err)
	}
	s.recordTerminalTransition(ctx, platformoauth.ReauthorizationReasonAuthorizationLost)
	return nil
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
	if err := markConnectionTerminal(ctx, q, row.OrganizationID, row.ConnectionID, row.ConnectionGeneration, platformoauth.ReauthorizationReasonConnectionRevoked, now); err != nil {
		return platformoauth.Session{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return platformoauth.Session{}, fmt.Errorf("commit revoke platform mcp session: %w", err)
	}
	s.recordTerminalTransition(ctx, platformoauth.ReauthorizationReasonConnectionRevoked)
	return platformoauth.Session{ID: row.ID.String(), ClientID: row.ClientID, JTI: row.Jti, RefreshHash: row.RefreshTokenHash, ExpiresAt: row.ExpiresAt.Time, RefreshExpiresAt: row.RefreshExpiresAt.Time, RevokedAt: &now, Connection: platformoauth.Connection{ID: row.ConnectionID.String(), ClientID: row.ClientID, Subject: row.SubjectUrn, OrganizationID: row.OrganizationID, Generation: row.ConnectionGeneration.String()}}, nil
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
	current, err := q.GetPlatformMCPConnectionForUpdate(ctx, platformrepo.GetPlatformMCPConnectionForUpdateParams{ID: id, OrganizationID: organizationID})
	if err != nil {
		return platformoauth.Connection{}, mapOAuthReadError(err)
	}
	if current.RevokedAt.Valid || current.ClientRevokedAt.Valid || current.ReauthorizationRequiredAt.Valid {
		return platformoauth.Connection{}, platformoauth.ErrRevoked
	}
	connection, err := q.RotatePlatformMCPConnectionGeneration(ctx, platformrepo.RotatePlatformMCPConnectionGenerationParams{ConnectionID: id, OrganizationID: organizationID, ActiveGeneration: newGeneration, ReauthorizedAt: timestamp(now), AuthorizationExpiresAt: timestamp(now.Add(platformoauth.AuthorizationLifetime))})
	if err != nil {
		return platformoauth.Connection{}, mapOAuthWriteError(err)
	}
	if err := q.RevokePlatformMCPSessionFamily(ctx, platformrepo.RevokePlatformMCPSessionFamilyParams{OrganizationID: organizationID, ConnectionID: id, ConnectionGeneration: current.ActiveGeneration, RevokedAt: timestamp(now)}); err != nil {
		return platformoauth.Connection{}, fmt.Errorf("revoke old platform mcp generation: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return platformoauth.Connection{}, fmt.Errorf("commit rotate platform mcp generation: %w", err)
	}
	return connectionFromRow(connection, current.ClientID), nil
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

type grantRow struct {
	ID                     uuid.UUID
	OrganizationID         string
	ConnectionID           uuid.UUID
	ConnectionGeneration   uuid.UUID
	RedirectURI            string
	CodeChallenge          string
	ExpiresAt              pgtype.Timestamptz
	ConsumedAt             pgtype.Timestamptz
	RevokedAt              pgtype.Timestamptz
	Subject                string
	ActiveGeneration       uuid.UUID
	ClientID               string
	AuthorizationExpiresAt pgtype.Timestamptz
}

func validateGrantRow(input platformoauth.ConsumeGrantInput, row grantRow) (platformoauth.Grant, error) {
	if row.ClientID != input.ClientID {
		return platformoauth.Grant{}, platformoauth.ErrClientMismatch
	}
	if row.RedirectURI != input.RedirectURI {
		return platformoauth.Grant{}, platformoauth.ErrRedirectURI
	}
	if !row.ExpiresAt.Valid || !input.Now.Before(row.ExpiresAt.Time) || !row.AuthorizationExpiresAt.Valid || !input.Now.Before(row.AuthorizationExpiresAt.Time) {
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
	return platformoauth.Grant{Code: input.Code, ClientID: row.ClientID, RedirectURI: row.RedirectURI, CodeChallenge: row.CodeChallenge, ExpiresAt: row.ExpiresAt.Time, Connection: platformoauth.Connection{ID: row.ConnectionID.String(), ClientID: row.ClientID, Subject: row.Subject, OrganizationID: row.OrganizationID, Generation: row.ConnectionGeneration.String(), AuthorizationExpiresAt: row.AuthorizationExpiresAt.Time}}, nil
}

func markConnectionTerminal(ctx context.Context, q *platformrepo.Queries, organizationID string, connectionID, generation uuid.UUID, reason platformoauth.ReauthorizationReason, now time.Time) error {
	// Lock the connection before its sessions so every terminal transition uses
	// the same connection -> session ordering as refresh and explicit revocation.
	if _, err := q.MarkPlatformMCPConnectionReauthorizationRequired(ctx, platformrepo.MarkPlatformMCPConnectionReauthorizationRequiredParams{OrganizationID: organizationID, ConnectionID: connectionID, ConnectionGeneration: generation, ReauthorizationRequiredAt: timestamp(now), ReauthorizationReason: text(string(reason))}); err != nil {
		return mapOAuthWriteError(err)
	}
	if err := q.RevokePlatformMCPSessionFamily(ctx, platformrepo.RevokePlatformMCPSessionFamilyParams{OrganizationID: organizationID, ConnectionID: connectionID, ConnectionGeneration: generation, RevokedAt: timestamp(now)}); err != nil {
		return fmt.Errorf("revoke terminal platform mcp session family: %w", err)
	}
	return nil
}

func connectionFromRow(row platformrepo.PlatformMcpConnection, clientID string) platformoauth.Connection {
	authorizationExpiresAt := row.AuthorizationExpiresAt.Time
	if !row.AuthorizationExpiresAt.Valid {
		authorizedAt := row.AuthorizedAt.Time
		if row.ReauthorizedAt.Valid {
			authorizedAt = row.ReauthorizedAt.Time
		}
		authorizationExpiresAt = authorizedAt.Add(platformoauth.AuthorizationLifetime)
	}
	return platformoauth.Connection{ID: row.ID.String(), ClientID: clientID, Subject: row.SubjectUrn, OrganizationID: row.OrganizationID, Generation: row.ActiveGeneration.String(), AuthorizationExpiresAt: authorizationExpiresAt, ReauthorizationRequiredAt: timePointer(row.ReauthorizationRequiredAt), ReauthorizationReason: platformoauth.ReauthorizationReason(row.ReauthorizationReason.String), RevokedAt: timePointer(row.RevokedAt)}
}

func sessionFromRow(row platformrepo.GetPlatformMCPSessionForRefreshRow) platformoauth.Session {
	return platformoauth.Session{ID: row.ID.String(), ClientID: row.ClientID, JTI: row.Jti, RefreshHash: row.RefreshTokenHash, ExpiresAt: row.ExpiresAt.Time, RefreshExpiresAt: row.RefreshExpiresAt.Time, Connection: platformoauth.Connection{ID: row.ConnectionID.String(), ClientID: row.ClientID, Subject: row.SubjectUrn, OrganizationID: row.OrganizationID, Generation: row.ConnectionGeneration.String()}}
}

func sessionFromRefreshRow(row platformrepo.GetPlatformMCPSessionForRefreshForUpdateRow) platformoauth.Session {
	return platformoauth.Session{ID: row.ID.String(), ClientID: row.ClientID, JTI: row.Jti, RefreshHash: row.RefreshTokenHash, ExpiresAt: row.ExpiresAt.Time, RefreshExpiresAt: row.RefreshExpiresAt.Time, RotatedAt: timePointer(row.RotatedAt), RevokedAt: timePointer(row.RevokedAt), Connection: platformoauth.Connection{ID: row.ConnectionID.String(), ClientID: row.ClientID, Subject: row.SubjectUrn, OrganizationID: row.OrganizationID, Generation: row.ConnectionGeneration.String(), AuthorizationExpiresAt: row.EffectiveAuthorizationExpiresAt.Time, RevokedAt: timePointer(row.ConnectionRevokedAt)}}
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
