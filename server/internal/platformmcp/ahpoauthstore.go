//nolint:exhaustruct // Database row projections intentionally omit lifecycle fields not selected by each query.
package platformmcp

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	ahprepo "github.com/speakeasy-api/gram/server/internal/platformmcp/ahprepo"
	platformoauth "github.com/speakeasy-api/gram/server/internal/platformmcp/oauth"
)

// PostgresAHPOAuthStore persists the AHP-owned OAuth lifecycle. It keeps opaque
// codes and refresh tokens hashed, and uses transactions for every transition
// that locks or invalidates a grant/session family.
type PostgresAHPOAuthStore struct {
	db        *pgxpool.Pool
	telemetry OAuthTelemetry
}

var _ platformoauth.Store = (*PostgresAHPOAuthStore)(nil)

func NewPostgresAHPOAuthStore(db *pgxpool.Pool) *PostgresAHPOAuthStore {
	return &PostgresAHPOAuthStore{db: db, telemetry: noopOAuthTelemetry{}}
}

func (s *PostgresAHPOAuthStore) WithTelemetry(telemetry OAuthTelemetry) *PostgresAHPOAuthStore {
	if telemetry != nil {
		s.telemetry = telemetry
	}
	return s
}

func (s *PostgresAHPOAuthStore) recordTerminalTransition(ctx context.Context, reason platformoauth.ReauthorizationReason) {
	if s != nil && s.telemetry != nil {
		s.telemetry.RecordTerminalTransition(ctx, reason)
	}
}

func (s *PostgresAHPOAuthStore) RegisterClient(ctx context.Context, client platformoauth.Client) error {
	if s == nil || s.db == nil || client.ID == "" || client.Name == "" || len(client.RedirectURIs) == 0 {
		return platformoauth.ErrNotFound
	}
	_, err := ahprepo.New(s.db).CreateAHPOAuthClient(ctx, ahprepo.CreateAHPOAuthClientParams{
		ClientID:              client.ID,
		ClientSecretHash:      text(client.SecretHash),
		ClientName:            client.Name,
		RedirectUris:          client.RedirectURIs,
		ClientSecretExpiresAt: optionalTimestamp(client.SecretExpiresAt),
	})
	return mapOAuthWriteError(err)
}

func (s *PostgresAHPOAuthStore) GetClient(ctx context.Context, clientID string) (platformoauth.Client, error) {
	if s == nil || s.db == nil {
		return platformoauth.Client{}, platformoauth.ErrNotFound
	}
	row, err := ahprepo.New(s.db).GetActiveAHPOAuthClientByClientID(ctx, clientID)
	if err != nil {
		return platformoauth.Client{}, mapOAuthReadError(err)
	}
	return ahpOauthClientFromRow(row), nil
}

func (s *PostgresAHPOAuthStore) UpsertClientFromCIMD(ctx context.Context, input platformoauth.UpsertCIMDClientInput) (platformoauth.Client, error) {
	if s == nil || s.db == nil || input.ClientID == "" || input.Name == "" || len(input.RedirectURIs) == 0 {
		return platformoauth.Client{}, platformoauth.ErrNotFound
	}
	row, err := ahprepo.New(s.db).UpsertAHPOAuthClientFromCIMD(ctx, ahprepo.UpsertAHPOAuthClientFromCIMDParams{
		ClientID:             input.ClientID,
		ClientName:           input.Name,
		RedirectUris:         input.RedirectURIs,
		CacheTtlSeconds:      input.CacheTTL.Seconds(),
		ClientIDMetadataEtag: text(input.ETag),
	})
	if err != nil {
		// No rows means the guarded DO UPDATE refused the write: the
		// client_id is held by a secret-bearing registration or a revoked
		// row. Both are "no such CIMD client" to the caller.
		return platformoauth.Client{}, mapOAuthReadError(err)
	}
	return ahpOauthClientFromRow(row), nil
}

func (s *PostgresAHPOAuthStore) TouchClientCIMDCache(ctx context.Context, input platformoauth.TouchCIMDCacheInput) (platformoauth.Client, error) {
	if s == nil || s.db == nil || input.ClientID == "" {
		return platformoauth.Client{}, platformoauth.ErrNotFound
	}
	row, err := ahprepo.New(s.db).UpdateAHPOAuthClientCIMDCache(ctx, ahprepo.UpdateAHPOAuthClientCIMDCacheParams{
		ClientID:             input.ClientID,
		CacheTtlSeconds:      input.CacheTTL.Seconds(),
		ClientIDMetadataEtag: text(input.ETag),
	})
	if err != nil {
		return platformoauth.Client{}, mapOAuthReadError(err)
	}
	return ahpOauthClientFromRow(row), nil
}

func ahpOauthClientFromRow(row ahprepo.AhpOauthClient) platformoauth.Client {
	return platformoauth.Client{
		ID:              row.ClientID,
		SecretHash:      row.ClientSecretHash.String,
		Name:            row.ClientName,
		RedirectURIs:    row.RedirectUris,
		SecretExpiresAt: timePointer(row.ClientSecretExpiresAt),
		RevokedAt:       timePointer(row.RevokedAt),
		MetadataURI:     row.ClientIDMetadataUri.String,
		CacheExpiresAt:  timePointer(row.ClientIDMetadataCacheExpiresAt),
		ETag:            row.ClientIDMetadataEtag.String,
	}
}

func (s *PostgresAHPOAuthStore) RevokeClient(ctx context.Context, clientID string, now time.Time) error {
	if s == nil || s.db == nil {
		return platformoauth.ErrNotFound
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin revoke AHP client: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	q := ahprepo.New(tx)
	client, err := q.GetAHPOAuthClientForUpdate(ctx, clientID)
	if err != nil {
		return mapOAuthReadError(err)
	}
	if client.RevokedAt.Valid {
		return platformoauth.ErrRevoked
	}
	connections, err := q.ListAHPClientConnectionsForUpdate(ctx, client.ID)
	if err != nil {
		return fmt.Errorf("lock AHP client connections: %w", err)
	}
	for _, connection := range connections {
		if err := ahpMarkConnectionTerminal(ctx, q, connection.OrganizationID, connection.ID, connection.ActiveGeneration, platformoauth.ReauthorizationReasonClientRevoked, now); err != nil {
			return err
		}
	}
	if _, err = q.RevokeAHPOAuthClient(ctx, ahprepo.RevokeAHPOAuthClientParams{ClientID: clientID, RevokedAt: timestamp(now)}); err != nil {
		return mapOAuthWriteError(err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit revoke AHP client: %w", err)
	}
	for range connections {
		s.recordTerminalTransition(ctx, platformoauth.ReauthorizationReasonClientRevoked)
	}
	return nil
}

func (s *PostgresAHPOAuthStore) RegisterConnection(ctx context.Context, connection platformoauth.Connection) error {
	projectID, bindingErr := uuid.Parse(connection.ProjectID)
	if bindingErr != nil || connection.ResourceURI == "" {
		return platformoauth.ErrNotFound
	}
	if s == nil || s.db == nil {
		return platformoauth.ErrNotFound
	}
	id, err := uuid.Parse(connection.ID)
	if err != nil {
		return platformoauth.ErrNotFound
	}
	client, err := ahprepo.New(s.db).GetActiveAHPOAuthClientByClientID(ctx, connection.ClientID)
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
	_, err = ahprepo.New(s.db).CreateAHPConnection(ctx, ahprepo.CreateAHPConnectionParams{
		ProjectID: projectID, ResourceUri: connection.ResourceURI,
		ID:                     id,
		OrganizationID:         connection.OrganizationID,
		SubjectUrn:             connection.Subject,
		OauthClientID:          client.ID,
		ActiveGeneration:       generation,
		AuthorizationExpiresAt: timestamp(connection.AuthorizationExpiresAt),
	})
	return mapOAuthWriteError(err)
}

func (s *PostgresAHPOAuthStore) GetConnection(context.Context, string, string, string) (platformoauth.Connection, error) {
	return platformoauth.Connection{}, platformoauth.ErrNotFound
}

func (s *PostgresAHPOAuthStore) GetBoundConnection(ctx context.Context, organizationID, subject, clientID, project, resource string) (platformoauth.Connection, error) {
	projectID, err := uuid.Parse(project)
	if err != nil || resource == "" {
		return platformoauth.Connection{}, platformoauth.ErrNotFound
	}
	if s == nil || s.db == nil {
		return platformoauth.Connection{}, platformoauth.ErrNotFound
	}
	client, err := ahprepo.New(s.db).GetActiveAHPOAuthClientByClientID(ctx, clientID)
	if err != nil {
		return platformoauth.Connection{}, mapOAuthReadError(err)
	}
	row, err := ahprepo.New(s.db).GetActiveAHPConnection(ctx, ahprepo.GetActiveAHPConnectionParams{
		ProjectID: projectID, ResourceUri: resource, OrganizationID: organizationID, SubjectUrn: subject, OauthClientID: client.ID})
	if err != nil {
		return platformoauth.Connection{}, mapOAuthReadError(err)
	}
	return ahpConnectionFromRow(row, clientID), nil
}

func (s *PostgresAHPOAuthStore) AuthorizeConnection(ctx context.Context, input platformoauth.AuthorizeConnectionInput) (platformoauth.Connection, error) {
	projectID, bindingErr := uuid.Parse(input.Connection.ProjectID)
	if bindingErr != nil || input.Connection.ResourceURI == "" {
		return platformoauth.Connection{}, platformoauth.ErrNotFound
	}
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
		return platformoauth.Connection{}, fmt.Errorf("begin authorize AHP connection: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := ahprepo.New(tx)

	client, err := q.GetActiveAHPOAuthClientByClientID(ctx, input.Connection.ClientID)
	if err != nil {
		return platformoauth.Connection{}, mapOAuthReadError(err)
	}
	if err := q.LockAHPConnectionAuthorization(ctx, ahprepo.LockAHPConnectionAuthorizationParams{
		ProjectID: projectID, ResourceUri: input.Connection.ResourceURI,
		OrganizationID: input.Connection.OrganizationID,
		SubjectUrn:     input.Connection.Subject,
		OauthClientID:  client.ClientID,
	}); err != nil {
		return platformoauth.Connection{}, fmt.Errorf("lock AHP connection authorization: %w", err)
	}

	connection := input.Connection
	if connection.AuthorizationExpiresAt.IsZero() {
		connection.AuthorizationExpiresAt = input.Now.Add(platformoauth.AuthorizationLifetime)
	}
	grantConnectionID := connectionID
	grantGeneration := generation
	current, err := q.GetActiveAHPConnection(ctx, ahprepo.GetActiveAHPConnectionParams{
		ProjectID: projectID, ResourceUri: input.Connection.ResourceURI,
		OrganizationID: input.Connection.OrganizationID,
		SubjectUrn:     input.Connection.Subject,
		OauthClientID:  client.ID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		if _, err := q.CreateAHPConnection(ctx, ahprepo.CreateAHPConnectionParams{
			ProjectID: projectID, ResourceUri: input.Connection.ResourceURI,
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
		updated, err := q.RotateAHPConnectionGeneration(ctx, ahprepo.RotateAHPConnectionGenerationParams{ConnectionID: current.ID, OrganizationID: current.OrganizationID, ActiveGeneration: generation, ReauthorizedAt: timestamp(input.Now), AuthorizationExpiresAt: timestamp(connection.AuthorizationExpiresAt)})
		if err != nil {
			return platformoauth.Connection{}, mapOAuthWriteError(err)
		}
		if err := q.RevokeAHPSessionFamily(ctx, ahprepo.RevokeAHPSessionFamilyParams{OrganizationID: current.OrganizationID, ConnectionID: current.ID, ConnectionGeneration: current.ActiveGeneration, RevokedAt: timestamp(input.Now)}); err != nil {
			return platformoauth.Connection{}, fmt.Errorf("revoke reauthorized AHP session family: %w", err)
		}
		connection = ahpConnectionFromRow(updated, input.Connection.ClientID)
		grantConnectionID = updated.ID
		grantGeneration = updated.ActiveGeneration
	}

	if input.Grant.ClientID != connection.ClientID {
		return platformoauth.Connection{}, platformoauth.ErrClientMismatch
	}
	if _, err := q.CreateAHPAuthorizationGrant(ctx, ahprepo.CreateAHPAuthorizationGrantParams{
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
		return platformoauth.Connection{}, fmt.Errorf("commit authorize AHP connection: %w", err)
	}
	return connection, nil
}

func (s *PostgresAHPOAuthStore) RevokeConnection(ctx context.Context, organizationID, connectionID string, now time.Time) error {
	if s == nil || s.db == nil {
		return platformoauth.ErrNotFound
	}
	id, err := uuid.Parse(connectionID)
	if err != nil {
		return platformoauth.ErrNotFound
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin revoke AHP connection: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := ahprepo.New(tx)
	connection, err := q.GetAHPConnectionForUpdate(ctx, ahprepo.GetAHPConnectionForUpdateParams{ID: id, OrganizationID: organizationID})
	if err != nil {
		return mapOAuthReadError(err)
	}
	if connection.RevokedAt.Valid || connection.ClientRevokedAt.Valid {
		return platformoauth.ErrRevoked
	}
	if err := q.RevokeAHPSessionFamily(ctx, ahprepo.RevokeAHPSessionFamilyParams{OrganizationID: organizationID, ConnectionID: id, ConnectionGeneration: connection.ActiveGeneration, RevokedAt: timestamp(now)}); err != nil {
		return fmt.Errorf("revoke AHP connection sessions: %w", err)
	}
	if _, err := q.RevokeAHPConnection(ctx, ahprepo.RevokeAHPConnectionParams{ID: id, OrganizationID: organizationID, RevokedAt: timestamp(now)}); err != nil {
		return mapOAuthWriteError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit revoke AHP connection: %w", err)
	}
	if !connection.ReauthorizationRequiredAt.Valid {
		s.recordTerminalTransition(ctx, platformoauth.ReauthorizationReasonConnectionRevoked)
	}
	return nil
}

func (s *PostgresAHPOAuthStore) IssueGrant(ctx context.Context, grant platformoauth.Grant) error {
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
	connection, err := ahprepo.New(s.db).GetActiveAHPConnectionByID(ctx, ahprepo.GetActiveAHPConnectionByIDParams{ID: connectionID, OrganizationID: grant.Connection.OrganizationID})
	if err != nil {
		return mapOAuthReadError(err)
	}
	if connection.ProjectID.String() != grant.Connection.ProjectID || connection.ResourceUri != grant.Connection.ResourceURI || connection.ClientID != grant.ClientID || connection.SubjectUrn != grant.Connection.Subject || connection.OrganizationID != grant.Connection.OrganizationID {
		return platformoauth.ErrClientMismatch
	}
	if connection.ActiveGeneration != generation {
		return platformoauth.ErrGeneration
	}
	_, err = ahprepo.New(s.db).CreateAHPAuthorizationGrant(ctx, ahprepo.CreateAHPAuthorizationGrantParams{
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

func (s *PostgresAHPOAuthStore) ValidateGrant(ctx context.Context, input platformoauth.ValidateGrantInput) (platformoauth.Grant, error) {
	if s == nil || s.db == nil {
		return platformoauth.Grant{}, platformoauth.ErrNotFound
	}
	row, err := ahprepo.New(s.db).GetAHPAuthorizationGrantForValidation(ctx, ahprepo.GetAHPAuthorizationGrantForValidationParams{OrganizationID: input.OrganizationID, AuthorizationCodeHash: opaqueHash(input.Code)})
	if err != nil {
		return platformoauth.Grant{}, mapOAuthReadError(err)
	}
	return ahpValidateGrantRow(input, ahpGrantRow{
		ID: row.ID, OrganizationID: row.OrganizationID, ProjectID: row.ProjectID.String(), ResourceUri: row.ResourceUri, ConnectionID: row.ConnectionID, ConnectionGeneration: row.ConnectionGeneration,
		RedirectURI: row.RedirectUri, CodeChallenge: row.CodeChallenge, ExpiresAt: row.ExpiresAt, ConsumedAt: row.ConsumedAt,
		RevokedAt: row.RevokedAt, Subject: row.SubjectUrn, ActiveGeneration: row.ActiveGeneration, ClientID: row.ClientID,
		AuthorizationExpiresAt: row.EffectiveAuthorizationExpiresAt,
	})
}

func (s *PostgresAHPOAuthStore) ExchangeGrant(ctx context.Context, input platformoauth.ExchangeGrantInput) (platformoauth.Grant, error) {
	if s == nil || s.db == nil {
		return platformoauth.Grant{}, platformoauth.ErrNotFound
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return platformoauth.Grant{}, fmt.Errorf("begin exchange AHP grant: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	q := ahprepo.New(tx)
	row, err := q.GetAHPAuthorizationGrantForConsume(ctx, ahprepo.GetAHPAuthorizationGrantForConsumeParams{OrganizationID: input.OrganizationID, AuthorizationCodeHash: opaqueHash(input.Code)})
	if err != nil {
		return platformoauth.Grant{}, mapOAuthReadError(err)
	}
	grant, err := ahpValidateGrantRow(input.ConsumeGrantInput, ahpGrantRow{
		ID: row.ID, OrganizationID: row.OrganizationID, ProjectID: row.ProjectID.String(), ResourceUri: row.ResourceUri, ConnectionID: row.ConnectionID, ConnectionGeneration: row.ConnectionGeneration,
		RedirectURI: row.RedirectUri, CodeChallenge: row.CodeChallenge, ExpiresAt: row.ExpiresAt, ConsumedAt: row.ConsumedAt,
		RevokedAt: row.RevokedAt, Subject: row.SubjectUrn, ActiveGeneration: row.ActiveGeneration, ClientID: row.ClientID,
		AuthorizationExpiresAt: row.EffectiveAuthorizationExpiresAt,
	})
	if err != nil {
		return platformoauth.Grant{}, err
	}
	if input.Session.Connection.ProjectID != grant.Connection.ProjectID || input.Session.Connection.ResourceURI != grant.Connection.ResourceURI || input.Session.ClientID != grant.ClientID || input.Session.Connection.ID != grant.Connection.ID || input.Session.Connection.ClientID != grant.Connection.ClientID || input.Session.Connection.Subject != grant.Connection.Subject || input.Session.Connection.OrganizationID != grant.Connection.OrganizationID {
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
	if _, err := q.ConsumeAHPAuthorizationGrant(ctx, ahprepo.ConsumeAHPAuthorizationGrantParams{ID: row.ID, OrganizationID: input.OrganizationID, ConsumedAt: timestamp(input.Now)}); err != nil {
		return platformoauth.Grant{}, mapOAuthWriteError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return platformoauth.Grant{}, fmt.Errorf("commit exchange AHP grant: %w", err)
	}
	return grant, nil
}

func (s *PostgresAHPOAuthStore) ConsumeGrant(ctx context.Context, input platformoauth.ConsumeGrantInput) (platformoauth.Grant, error) {
	if s == nil || s.db == nil {
		return platformoauth.Grant{}, platformoauth.ErrNotFound
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return platformoauth.Grant{}, fmt.Errorf("begin consume AHP grant: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	q := ahprepo.New(tx)
	row, err := q.GetAHPAuthorizationGrantForConsume(ctx, ahprepo.GetAHPAuthorizationGrantForConsumeParams{OrganizationID: input.OrganizationID, AuthorizationCodeHash: opaqueHash(input.Code)})
	if err != nil {
		return platformoauth.Grant{}, mapOAuthReadError(err)
	}
	grant, err := ahpValidateGrantRow(input, ahpGrantRow{
		ID: row.ID, OrganizationID: row.OrganizationID, ProjectID: row.ProjectID.String(), ResourceUri: row.ResourceUri, ConnectionID: row.ConnectionID, ConnectionGeneration: row.ConnectionGeneration,
		RedirectURI: row.RedirectUri, CodeChallenge: row.CodeChallenge, ExpiresAt: row.ExpiresAt, ConsumedAt: row.ConsumedAt,
		RevokedAt: row.RevokedAt, Subject: row.SubjectUrn, ActiveGeneration: row.ActiveGeneration, ClientID: row.ClientID,
		AuthorizationExpiresAt: row.EffectiveAuthorizationExpiresAt,
	})
	if err != nil {
		return platformoauth.Grant{}, err
	}
	if _, err := q.ConsumeAHPAuthorizationGrant(ctx, ahprepo.ConsumeAHPAuthorizationGrantParams{ID: row.ID, OrganizationID: input.OrganizationID, ConsumedAt: timestamp(input.Now)}); err != nil {
		return platformoauth.Grant{}, mapOAuthWriteError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return platformoauth.Grant{}, fmt.Errorf("commit consume AHP grant: %w", err)
	}
	return grant, nil
}

func (s *PostgresAHPOAuthStore) CreateSession(ctx context.Context, session platformoauth.Session) error {
	if s == nil || s.db == nil {
		return platformoauth.ErrNotFound
	}
	q := ahprepo.New(s.db)
	if err := ahpValidateSessionConnection(ctx, q, session); err != nil {
		return err
	}
	return s.createSessionWithQueries(ctx, q, session)
}

func (s *PostgresAHPOAuthStore) GetSessionByRefreshHash(ctx context.Context, organizationID, refreshHash string) (platformoauth.Session, error) {
	if s == nil || s.db == nil {
		return platformoauth.Session{}, platformoauth.ErrNotFound
	}
	row, err := ahprepo.New(s.db).GetAHPSessionForRefresh(ctx, ahprepo.GetAHPSessionForRefreshParams{OrganizationID: organizationID, RefreshTokenHash: refreshHash})
	if err != nil {
		return platformoauth.Session{}, mapOAuthReadError(err)
	}
	return ahpSessionFromRow(row), nil
}

func (s *PostgresAHPOAuthStore) DetectRefreshReuse(ctx context.Context, organizationID, refreshHash string, now time.Time) (bool, error) {
	if s == nil || s.db == nil {
		return false, platformoauth.ErrNotFound
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("begin detect AHP refresh reuse: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := ahprepo.New(tx)
	row, err := q.GetAHPSessionForRefreshForUpdate(ctx, ahprepo.GetAHPSessionForRefreshForUpdateParams{OrganizationID: organizationID, RefreshTokenHash: refreshHash})
	if err != nil {
		return false, mapOAuthReadError(err)
	}
	if !row.RevokedAt.Valid {
		if err := tx.Commit(ctx); err != nil {
			return false, fmt.Errorf("commit active AHP refresh check: %w", err)
		}
		return false, nil
	}
	terminalized, err := ahpTerminalizeRefreshReuse(ctx, q, row, now)
	if err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("commit reused AHP session family: %w", err)
	}
	if terminalized {
		s.recordTerminalTransition(ctx, platformoauth.ReauthorizationReasonRefreshReuse)
	}
	return true, nil
}

func (s *PostgresAHPOAuthStore) PrepareRefresh(ctx context.Context, input platformoauth.PrepareRefreshInput) (platformoauth.Session, error) {
	if s == nil || s.db == nil {
		return platformoauth.Session{}, platformoauth.ErrNotFound
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return platformoauth.Session{}, fmt.Errorf("begin prepare AHP refresh: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := ahprepo.New(tx)
	row, err := q.GetAHPSessionForRefreshForUpdate(ctx, ahprepo.GetAHPSessionForRefreshForUpdateParams{OrganizationID: input.OrganizationID, RefreshTokenHash: input.RefreshHash})
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
		terminalized, err := ahpTerminalizeRefreshReuse(ctx, q, row, input.Now)
		if err != nil {
			return platformoauth.Session{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return platformoauth.Session{}, fmt.Errorf("commit reused AHP refresh: %w", err)
		}
		if terminalized {
			s.recordTerminalTransition(ctx, platformoauth.ReauthorizationReasonRefreshReuse)
		}
		return platformoauth.Session{}, platformoauth.ErrAlreadyUsed
	}
	if row.ConnectionRevokedAt.Valid || row.ClientRevokedAt.Valid || row.ReauthorizationRequiredAt.Valid {
		return platformoauth.Session{}, platformoauth.ErrRevoked
	}
	if row.ActiveGeneration != row.ConnectionGeneration {
		return platformoauth.Session{}, platformoauth.ErrGeneration
	}
	if !row.EffectiveAuthorizationExpiresAt.Valid || !input.Now.Before(row.EffectiveAuthorizationExpiresAt.Time) {
		if err := ahpMarkConnectionTerminal(ctx, q, row.OrganizationID, row.ConnectionID, row.ConnectionGeneration, platformoauth.ReauthorizationReasonAuthorizationExpired, input.Now); err != nil {
			return platformoauth.Session{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return platformoauth.Session{}, fmt.Errorf("commit expired AHP authorization: %w", err)
		}
		s.recordTerminalTransition(ctx, platformoauth.ReauthorizationReasonAuthorizationExpired)
		return platformoauth.Session{}, platformoauth.ErrExpired
	}
	if !row.RefreshExpiresAt.Valid || !input.Now.Before(row.RefreshExpiresAt.Time) {
		if err := ahpMarkConnectionTerminal(ctx, q, row.OrganizationID, row.ConnectionID, row.ConnectionGeneration, platformoauth.ReauthorizationReasonRefreshIdleExpired, input.Now); err != nil {
			return platformoauth.Session{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return platformoauth.Session{}, fmt.Errorf("commit expired AHP refresh: %w", err)
		}
		s.recordTerminalTransition(ctx, platformoauth.ReauthorizationReasonRefreshIdleExpired)
		return platformoauth.Session{}, platformoauth.ErrExpired
	}
	session := ahpSessionFromRefreshRow(row)
	if err := tx.Commit(ctx); err != nil {
		return platformoauth.Session{}, fmt.Errorf("commit prepared AHP refresh: %w", err)
	}
	return session, nil
}

func (s *PostgresAHPOAuthStore) RotateSession(ctx context.Context, input platformoauth.RotateSessionInput) (platformoauth.Session, error) {
	if s == nil || s.db == nil {
		return platformoauth.Session{}, platformoauth.ErrNotFound
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return platformoauth.Session{}, fmt.Errorf("begin rotate AHP session: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := ahprepo.New(tx)
	row, err := q.GetAHPSessionForRefreshForUpdate(ctx, ahprepo.GetAHPSessionForRefreshForUpdateParams{OrganizationID: input.OrganizationID, RefreshTokenHash: input.RefreshHash})
	if errors.Is(err, pgx.ErrNoRows) {
		return platformoauth.Session{}, platformoauth.ErrAlreadyUsed
	}
	if err != nil {
		return platformoauth.Session{}, fmt.Errorf("lock AHP session: %w", err)
	}
	if row.RevokedAt.Valid {
		if !row.RotatedAt.Valid {
			return platformoauth.Session{}, platformoauth.ErrRevoked
		}
		terminalized, err := ahpTerminalizeRefreshReuse(ctx, q, row, input.Now)
		if err != nil {
			return platformoauth.Session{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return platformoauth.Session{}, fmt.Errorf("commit reused AHP session family: %w", err)
		}
		if terminalized {
			s.recordTerminalTransition(ctx, platformoauth.ReauthorizationReasonRefreshReuse)
		}
		return platformoauth.Session{}, platformoauth.ErrAlreadyUsed
	}
	if row.OrganizationID != input.OrganizationID || row.ClientID != input.ClientID || row.ConnectionGeneration.String() != input.Generation || row.ActiveGeneration != row.ConnectionGeneration || input.Replacement.Connection.ID != row.ConnectionID.String() || input.Replacement.Connection.Generation != row.ConnectionGeneration.String() {
		return platformoauth.Session{}, platformoauth.ErrGeneration
	}
	if row.ConnectionRevokedAt.Valid || row.ClientRevokedAt.Valid || row.ReauthorizationRequiredAt.Valid {
		return platformoauth.Session{}, platformoauth.ErrRevoked
	}
	if !row.EffectiveAuthorizationExpiresAt.Valid || !input.Now.Before(row.EffectiveAuthorizationExpiresAt.Time) {
		if err := ahpMarkConnectionTerminal(ctx, q, row.OrganizationID, row.ConnectionID, row.ConnectionGeneration, platformoauth.ReauthorizationReasonAuthorizationExpired, input.Now); err != nil {
			return platformoauth.Session{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return platformoauth.Session{}, fmt.Errorf("commit expired AHP authorization: %w", err)
		}
		s.recordTerminalTransition(ctx, platformoauth.ReauthorizationReasonAuthorizationExpired)
		return platformoauth.Session{}, platformoauth.ErrExpired
	}
	if !row.RefreshExpiresAt.Valid || !input.Now.Before(row.RefreshExpiresAt.Time) {
		if err := ahpMarkConnectionTerminal(ctx, q, row.OrganizationID, row.ConnectionID, row.ConnectionGeneration, platformoauth.ReauthorizationReasonRefreshIdleExpired, input.Now); err != nil {
			return platformoauth.Session{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return platformoauth.Session{}, fmt.Errorf("commit expired AHP session: %w", err)
		}
		s.recordTerminalTransition(ctx, platformoauth.ReauthorizationReasonRefreshIdleExpired)
		return platformoauth.Session{}, platformoauth.ErrExpired
	}
	if input.Replacement.ExpiresAt.After(row.EffectiveAuthorizationExpiresAt.Time) || input.Replacement.RefreshExpiresAt.After(row.EffectiveAuthorizationExpiresAt.Time) {
		return platformoauth.Session{}, platformoauth.ErrExpired
	}
	if err := ahpValidateSessionConnection(ctx, q, input.Replacement); err != nil {
		return platformoauth.Session{}, err
	}
	if err := s.createSessionWithQueries(ctx, q, input.Replacement); err != nil {
		return platformoauth.Session{}, err
	}
	replacementID, err := uuid.Parse(input.Replacement.ID)
	if err != nil {
		return platformoauth.Session{}, platformoauth.ErrNotFound
	}
	if _, err := q.RotateAHPSession(ctx, ahprepo.RotateAHPSessionParams{ID: row.ID, OrganizationID: input.OrganizationID, ReplacedBySessionID: uuid.NullUUID{UUID: replacementID, Valid: true}, RotatedAt: timestamp(input.Now)}); err != nil {
		return platformoauth.Session{}, mapOAuthWriteError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return platformoauth.Session{}, fmt.Errorf("commit rotate AHP session: %w", err)
	}
	return platformoauth.Session{ID: row.ID.String(), ClientID: row.ClientID, JTI: row.Jti, RefreshHash: row.RefreshTokenHash, ExpiresAt: row.ExpiresAt.Time, RefreshExpiresAt: row.RefreshExpiresAt.Time, Connection: platformoauth.Connection{ID: row.ConnectionID.String(), ClientID: row.ClientID, Subject: row.SubjectUrn, OrganizationID: row.OrganizationID, ProjectID: row.ProjectID.String(), ResourceURI: row.ResourceUri, Generation: row.ConnectionGeneration.String()}}, nil
}

func (s *PostgresAHPOAuthStore) MarkAuthorizationLost(ctx context.Context, organizationID, connectionID, generation string, now time.Time) error {
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
		return fmt.Errorf("begin mark AHP authorization lost: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := ahpMarkConnectionTerminal(ctx, ahprepo.New(tx), organizationID, id, activeGeneration, platformoauth.ReauthorizationReasonAuthorizationLost, now); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit mark AHP authorization lost: %w", err)
	}
	s.recordTerminalTransition(ctx, platformoauth.ReauthorizationReasonAuthorizationLost)
	return nil
}

func (s *PostgresAHPOAuthStore) RevokeSession(ctx context.Context, organizationID, refreshHash, clientID string, now time.Time) (platformoauth.Session, error) {
	if s == nil || s.db == nil {
		return platformoauth.Session{}, platformoauth.ErrNotFound
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return platformoauth.Session{}, fmt.Errorf("begin revoke AHP session: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := ahprepo.New(tx)
	row, err := q.GetAHPSessionForRefreshForUpdate(ctx, ahprepo.GetAHPSessionForRefreshForUpdateParams{OrganizationID: organizationID, RefreshTokenHash: refreshHash})
	if errors.Is(err, pgx.ErrNoRows) {
		return platformoauth.Session{}, platformoauth.ErrNotFound
	}
	if err != nil {
		return platformoauth.Session{}, fmt.Errorf("lookup AHP session for revoke: %w", err)
	}
	if row.ClientID != clientID {
		return platformoauth.Session{}, platformoauth.ErrNotFound
	}
	if err := ahpMarkConnectionTerminal(ctx, q, row.OrganizationID, row.ConnectionID, row.ConnectionGeneration, platformoauth.ReauthorizationReasonConnectionRevoked, now); err != nil {
		return platformoauth.Session{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return platformoauth.Session{}, fmt.Errorf("commit revoke AHP session: %w", err)
	}
	s.recordTerminalTransition(ctx, platformoauth.ReauthorizationReasonConnectionRevoked)
	return platformoauth.Session{ID: row.ID.String(), ClientID: row.ClientID, JTI: row.Jti, RefreshHash: row.RefreshTokenHash, ExpiresAt: row.ExpiresAt.Time, RefreshExpiresAt: row.RefreshExpiresAt.Time, RevokedAt: &now, Connection: platformoauth.Connection{ID: row.ConnectionID.String(), ClientID: row.ClientID, Subject: row.SubjectUrn, OrganizationID: row.OrganizationID, ProjectID: row.ProjectID.String(), ResourceURI: row.ResourceUri, Generation: row.ConnectionGeneration.String()}}, nil
}

func (s *PostgresAHPOAuthStore) RevokeAccessSession(ctx context.Context, organizationID, jti, clientID string, now time.Time) (platformoauth.Session, error) {
	if s == nil || s.db == nil {
		return platformoauth.Session{}, platformoauth.ErrNotFound
	}
	client, err := ahprepo.New(s.db).GetActiveAHPOAuthClientByClientID(ctx, clientID)
	if err != nil {
		return platformoauth.Session{}, mapOAuthReadError(err)
	}
	row, err := ahprepo.New(s.db).RevokeAHPSessionByJTI(ctx, ahprepo.RevokeAHPSessionByJTIParams{OrganizationID: organizationID, Jti: jti, OauthClientID: client.ID, RevokedAt: timestamp(now)})
	if err != nil {
		return platformoauth.Session{}, mapOAuthReadError(err)
	}
	return platformoauth.Session{ID: row.ID.String(), JTI: row.Jti, RefreshHash: row.RefreshTokenHash}, nil
}

func (s *PostgresAHPOAuthStore) RotateConnectionGeneration(ctx context.Context, organizationID, connectionID, generation string, now time.Time) (platformoauth.Connection, error) {
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
		return platformoauth.Connection{}, fmt.Errorf("begin rotate AHP generation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := ahprepo.New(tx)
	current, err := q.GetAHPConnectionForUpdate(ctx, ahprepo.GetAHPConnectionForUpdateParams{ID: id, OrganizationID: organizationID})
	if err != nil {
		return platformoauth.Connection{}, mapOAuthReadError(err)
	}
	if current.RevokedAt.Valid || current.ClientRevokedAt.Valid || current.ReauthorizationRequiredAt.Valid {
		return platformoauth.Connection{}, platformoauth.ErrRevoked
	}
	connection, err := q.RotateAHPConnectionGeneration(ctx, ahprepo.RotateAHPConnectionGenerationParams{ConnectionID: id, OrganizationID: organizationID, ActiveGeneration: newGeneration, ReauthorizedAt: timestamp(now), AuthorizationExpiresAt: timestamp(now.Add(platformoauth.AuthorizationLifetime))})
	if err != nil {
		return platformoauth.Connection{}, mapOAuthWriteError(err)
	}
	if err := q.RevokeAHPSessionFamily(ctx, ahprepo.RevokeAHPSessionFamilyParams{OrganizationID: organizationID, ConnectionID: id, ConnectionGeneration: current.ActiveGeneration, RevokedAt: timestamp(now)}); err != nil {
		return platformoauth.Connection{}, fmt.Errorf("revoke old AHP generation: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return platformoauth.Connection{}, fmt.Errorf("commit rotate AHP generation: %w", err)
	}
	return ahpConnectionFromRow(connection, current.ClientID), nil
}

func (s *PostgresAHPOAuthStore) createSessionWithQueries(ctx context.Context, q *ahprepo.Queries, session platformoauth.Session) error {
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
	connection, err := q.GetActiveAHPConnectionByID(ctx, ahprepo.GetActiveAHPConnectionByIDParams{ID: connectionID, OrganizationID: session.Connection.OrganizationID})
	if err != nil {
		return mapOAuthReadError(err)
	}
	_, err = q.CreateAHPSession(ctx, ahprepo.CreateAHPSessionParams{ID: id, OrganizationID: session.Connection.OrganizationID, ConnectionID: connectionID, OauthClientID: connection.OauthClientID, ConnectionGeneration: generation, Jti: session.JTI, RefreshTokenHash: session.RefreshHash, ExpiresAt: timestamp(session.ExpiresAt), RefreshExpiresAt: timestamp(session.RefreshExpiresAt)})
	return mapOAuthWriteError(err)
}

func ahpTerminalizeRefreshReuse(ctx context.Context, q *ahprepo.Queries, row ahprepo.GetAHPSessionForRefreshForUpdateRow, now time.Time) (bool, error) {
	if row.ReauthorizationRequiredAt.Valid {
		return false, nil
	}
	err := ahpMarkConnectionTerminal(ctx, q, row.OrganizationID, row.ConnectionID, row.ConnectionGeneration, platformoauth.ReauthorizationReasonRefreshReuse, now)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func ahpValidateSessionConnection(ctx context.Context, q *ahprepo.Queries, session platformoauth.Session) error {
	connectionID, err := uuid.Parse(session.Connection.ID)
	if err != nil {
		return platformoauth.ErrNotFound
	}
	generation, err := uuid.Parse(session.Connection.Generation)
	if err != nil {
		return platformoauth.ErrGeneration
	}
	connection, err := q.GetActiveAHPConnectionByID(ctx, ahprepo.GetActiveAHPConnectionByIDParams{ID: connectionID, OrganizationID: session.Connection.OrganizationID})
	if err != nil {
		return mapOAuthReadError(err)
	}
	if connection.ProjectID.String() != session.Connection.ProjectID || connection.ResourceUri != session.Connection.ResourceURI || connection.ClientID != session.ClientID || connection.SubjectUrn != session.Connection.Subject || connection.OrganizationID != session.Connection.OrganizationID {
		return platformoauth.ErrClientMismatch
	}
	if connection.ActiveGeneration != generation {
		return platformoauth.ErrGeneration
	}
	return nil
}

type ahpGrantRow struct {
	ID                     uuid.UUID
	OrganizationID         string
	ProjectID              string
	ResourceUri            string
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

func ahpValidateGrantRow(input platformoauth.ConsumeGrantInput, row ahpGrantRow) (platformoauth.Grant, error) {
	grant, err := validateGrantRow(input, grantRow{
		ID: row.ID, OrganizationID: row.OrganizationID, ConnectionID: row.ConnectionID,
		ConnectionGeneration: row.ConnectionGeneration, RedirectURI: row.RedirectURI,
		CodeChallenge: row.CodeChallenge, ExpiresAt: row.ExpiresAt, ConsumedAt: row.ConsumedAt,
		RevokedAt: row.RevokedAt, Subject: row.Subject, ActiveGeneration: row.ActiveGeneration,
		ClientID: row.ClientID, AuthorizationExpiresAt: row.AuthorizationExpiresAt,
	})
	if err != nil {
		return platformoauth.Grant{}, err
	}
	grant.Connection.ProjectID = row.ProjectID
	grant.Connection.ResourceURI = row.ResourceUri
	return grant, nil
}

func ahpMarkConnectionTerminal(ctx context.Context, q *ahprepo.Queries, organizationID string, connectionID, generation uuid.UUID, reason platformoauth.ReauthorizationReason, now time.Time) error {
	// Lock the connection before its sessions so every terminal transition uses
	// the same connection -> session ordering as refresh and explicit revocation.
	if _, err := q.MarkAHPConnectionReauthorizationRequired(ctx, ahprepo.MarkAHPConnectionReauthorizationRequiredParams{OrganizationID: organizationID, ConnectionID: connectionID, ConnectionGeneration: generation, ReauthorizationRequiredAt: timestamp(now), ReauthorizationReason: text(string(reason))}); err != nil {
		return mapOAuthWriteError(err)
	}
	if err := q.RevokeAHPSessionFamily(ctx, ahprepo.RevokeAHPSessionFamilyParams{OrganizationID: organizationID, ConnectionID: connectionID, ConnectionGeneration: generation, RevokedAt: timestamp(now)}); err != nil {
		return fmt.Errorf("revoke terminal AHP session family: %w", err)
	}
	return nil
}

func ahpConnectionFromRow(row ahprepo.AhpConnection, clientID string) platformoauth.Connection {
	authorizationExpiresAt := row.AuthorizationExpiresAt.Time
	if !row.AuthorizationExpiresAt.Valid {
		authorizedAt := row.AuthorizedAt.Time
		if row.ReauthorizedAt.Valid {
			authorizedAt = row.ReauthorizedAt.Time
		}
		authorizationExpiresAt = authorizedAt.Add(platformoauth.AuthorizationLifetime)
	}
	return platformoauth.Connection{ID: row.ID.String(), ClientID: clientID, Subject: row.SubjectUrn, OrganizationID: row.OrganizationID, ProjectID: row.ProjectID.String(), ResourceURI: row.ResourceUri, Generation: row.ActiveGeneration.String(), AuthorizationExpiresAt: authorizationExpiresAt, ReauthorizationRequiredAt: timePointer(row.ReauthorizationRequiredAt), ReauthorizationReason: platformoauth.ReauthorizationReason(row.ReauthorizationReason.String), RevokedAt: timePointer(row.RevokedAt)}
}

func ahpSessionFromRow(row ahprepo.GetAHPSessionForRefreshRow) platformoauth.Session {
	return platformoauth.Session{ID: row.ID.String(), ClientID: row.ClientID, JTI: row.Jti, RefreshHash: row.RefreshTokenHash, ExpiresAt: row.ExpiresAt.Time, RefreshExpiresAt: row.RefreshExpiresAt.Time, Connection: platformoauth.Connection{ID: row.ConnectionID.String(), ClientID: row.ClientID, Subject: row.SubjectUrn, OrganizationID: row.OrganizationID, ProjectID: row.ProjectID.String(), ResourceURI: row.ResourceUri, Generation: row.ConnectionGeneration.String()}}
}

func ahpSessionFromRefreshRow(row ahprepo.GetAHPSessionForRefreshForUpdateRow) platformoauth.Session {
	return platformoauth.Session{ID: row.ID.String(), ClientID: row.ClientID, JTI: row.Jti, RefreshHash: row.RefreshTokenHash, ExpiresAt: row.ExpiresAt.Time, RefreshExpiresAt: row.RefreshExpiresAt.Time, RotatedAt: timePointer(row.RotatedAt), RevokedAt: timePointer(row.RevokedAt), Connection: platformoauth.Connection{ID: row.ConnectionID.String(), ClientID: row.ClientID, Subject: row.SubjectUrn, OrganizationID: row.OrganizationID, ProjectID: row.ProjectID.String(), ResourceURI: row.ResourceUri, Generation: row.ConnectionGeneration.String(), AuthorizationExpiresAt: row.EffectiveAuthorizationExpiresAt.Time, RevokedAt: timePointer(row.ConnectionRevokedAt)}}
}

func (s *PostgresAHPOAuthStore) GetActiveSession(ctx context.Context, organizationID, jti string) (platformoauth.Session, error) {
	if s == nil || s.db == nil {
		return platformoauth.Session{}, platformoauth.ErrNotFound
	}
	row, err := ahprepo.New(s.db).GetActiveAHPSessionByJTI(ctx, ahprepo.GetActiveAHPSessionByJTIParams{OrganizationID: organizationID, Jti: jti})
	if err != nil {
		return platformoauth.Session{}, mapOAuthReadError(err)
	}
	return platformoauth.Session{ID: row.ID.String(), ClientID: row.ClientID, JTI: row.Jti, ExpiresAt: row.ExpiresAt.Time, RefreshExpiresAt: row.RefreshExpiresAt.Time, Connection: platformoauth.Connection{ID: row.ConnectionID.String(), ClientID: row.ClientID, OrganizationID: organizationID, Subject: row.SubjectUrn, ProjectID: row.ProjectID.String(), ResourceURI: row.ResourceUri, Generation: row.ConnectionGeneration.String()}}, nil
}
