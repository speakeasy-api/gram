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

	"github.com/speakeasy-api/gram/server/internal/adminmcp/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
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
	row, err := repo.New(s.db).GetAuthorizationGrant(ctx, repo.GetAuthorizationGrantParams{AuthorizationCodeHash: codeHash, ClientID: clientID})
	if errors.Is(err, pgx.ErrNoRows) {
		return staffTokenConnection{}, errStaffGrant
	}
	if err != nil {
		return staffTokenConnection{}, fmt.Errorf("lookup staff authorization grant: %w", err)
	}
	connection := tokenConnectionFromRow(row.AdminMcpConnection)
	if !validStaffVerifier(verifier) || !matchesStaffChallenge(verifier, row.CodeChallenge) || !now.Before(connection.AuthorizedTil) || row.RedirectUri != redirectURI {
		return staffTokenConnection{}, errStaffGrant
	}
	return connection, nil
}

func tokenConnectionFromRow(row repo.AdminMcpConnection) staffTokenConnection {
	return staffTokenConnection{
		ID:            row.ID,
		ClientRowID:   row.OauthClientID,
		Subject:       row.SubjectUrn,
		SessionEnc:    row.AdminSessionIDEnc,
		ResourceURI:   row.ResourceUri,
		Scopes:        row.Scopes,
		Generation:    row.ActiveGeneration,
		AuthorizedTil: row.AuthorizationExpiresAt.Time,
	}
}

func (s postgresStaffGrantStore) ExchangeGrant(ctx context.Context, codeHash, clientID, redirectURI, verifier string, session staffIssuedSession, now time.Time) error {
	if s.db == nil {
		return errors.New("staff OAuth state unavailable")
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin staff code exchange: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := repo.New(tx)
	row, err := q.LockAuthorizationGrant(ctx, repo.LockAuthorizationGrantParams{AuthorizationCodeHash: codeHash, ClientID: clientID})
	if errors.Is(err, pgx.ErrNoRows) {
		return errStaffGrant
	}
	if err != nil {
		return fmt.Errorf("lock staff authorization grant: %w", err)
	}
	connection := tokenConnectionFromRow(row.AdminMcpConnection)
	if row.RedirectUri != redirectURI || !validStaffVerifier(verifier) || !matchesStaffChallenge(verifier, row.CodeChallenge) || !validStaffIssuedSession(session, connection, now) {
		return errStaffGrant
	}
	err = q.ConsumeAuthorizationGrant(ctx, repo.ConsumeAuthorizationGrantParams{Now: conv.ToPGTimestamptz(now), AuthorizationCodeHash: codeHash})
	if err != nil {
		return fmt.Errorf("consume staff authorization grant: %w", err)
	}
	if err := insertStaffSession(ctx, q, connection, session); err != nil {
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

func insertStaffSession(ctx context.Context, q *repo.Queries, connection staffTokenConnection, session staffIssuedSession) error {
	err := q.InsertStaffSession(ctx, repo.InsertStaffSessionParams{
		ID:                   session.ID,
		ConnectionID:         connection.ID,
		OauthClientID:        connection.ClientRowID,
		ConnectionGeneration: connection.Generation,
		Jti:                  session.JTI,
		RefreshTokenHash:     session.RefreshHash,
		ExpiresAt:            conv.ToPGTimestamptz(session.ExpiresAt),
		RefreshExpiresAt:     conv.ToPGTimestamptz(session.RefreshTil),
	})
	if err != nil {
		return fmt.Errorf("persist staff MCP session: %w", err)
	}
	return nil
}

func (s postgresStaffGrantStore) PrepareRefresh(ctx context.Context, refreshHash, clientID string, now time.Time) (staffTokenConnection, error) {
	if s.db == nil {
		return staffTokenConnection{}, errors.New("staff OAuth state unavailable")
	}
	row, err := repo.New(s.db).GetRefreshSession(ctx, repo.GetRefreshSessionParams{RefreshTokenHash: refreshHash, ClientID: clientID})
	if errors.Is(err, pgx.ErrNoRows) {
		return staffTokenConnection{}, errStaffGrant
	}
	if err != nil {
		return staffTokenConnection{}, fmt.Errorf("lookup staff refresh session: %w", err)
	}
	if row.RotatedAt.Valid {
		return staffTokenConnection{}, s.terminalizeRefreshReuse(ctx, refreshHash, clientID, now)
	}
	connection := tokenConnectionFromRow(row.AdminMcpConnection)
	if row.RevokedAt.Valid || row.AdminMcpConnection.ReauthorizationRequiredAt.Valid || !now.Before(row.RefreshExpiresAt.Time) || !now.Before(connection.AuthorizedTil) {
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
	q := repo.New(tx)
	row, err := q.LockReusedRefreshSession(ctx, repo.LockReusedRefreshSessionParams{RefreshTokenHash: refreshHash, ClientID: clientID})
	if errors.Is(err, pgx.ErrNoRows) {
		return errStaffGrant
	}
	if err != nil {
		return fmt.Errorf("lock staff refresh reuse: %w", err)
	}
	if !row.RotatedAt.Valid || row.ActiveGeneration != row.ConnectionGeneration {
		return errStaffGrant
	}
	err = q.RequireReauthorizationForRefreshReuse(ctx, repo.RequireReauthorizationForRefreshReuseParams{Now: conv.ToPGTimestamptz(now), ID: row.ConnectionID, Generation: row.ConnectionGeneration})
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
	q := repo.New(tx)
	row, err := q.LockRefreshSession(ctx, repo.LockRefreshSessionParams{RefreshTokenHash: refreshHash, ClientID: clientID})
	if errors.Is(err, pgx.ErrNoRows) {
		return errStaffGrant
	}
	if err != nil {
		return fmt.Errorf("lock staff refresh session: %w", err)
	}
	connection := tokenConnectionFromRow(row.AdminMcpConnection)
	if row.RotatedAt.Valid {
		err = q.RequireReauthorizationForRefreshReuse(ctx, repo.RequireReauthorizationForRefreshReuseParams{Now: conv.ToPGTimestamptz(now), ID: connection.ID, Generation: row.SessionGeneration})
		if err != nil {
			return fmt.Errorf("terminalize reused staff refresh token: %w", err)
		}
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit staff refresh reuse: %w", err)
		}
		return errStaffRefreshReuse
	}
	if row.ClientRevokedAt.Valid || (row.ClientSecretExpiresAt.Valid && !now.Before(row.ClientSecretExpiresAt.Time)) || row.AdminMcpConnection.RevokedAt.Valid || row.AdminMcpConnection.ReauthorizationRequiredAt.Valid || row.SessionRevokedAt.Valid || !now.Before(row.RefreshExpiresAt.Time) || !now.Before(connection.AuthorizedTil) || connection.ID != expected.ID || connection.Generation != expected.Generation || connection.Generation != row.SessionGeneration || connection.Subject != expected.Subject || connection.ResourceURI != expected.ResourceURI || connection.SessionEnc != expected.SessionEnc || !validStaffIssuedSession(replacement, connection, now) {
		return errStaffGrant
	}
	if err := insertStaffSession(ctx, q, connection, replacement); err != nil {
		return err
	}
	err = q.RotateStaffSession(ctx, repo.RotateStaffSessionParams{Now: conv.ToPGTimestamptz(now), ReplacedBySessionID: uuid.NullUUID{UUID: replacement.ID, Valid: true}, ID: row.SessionID})
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
