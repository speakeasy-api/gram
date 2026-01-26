package chatsessions

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/chatsessions/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/encryption"
)

// CredentialStore handles storage and retrieval of OAuth credentials for chat sessions.
type CredentialStore struct {
	db  *pgxpool.Pool
	enc *encryption.Client
}

// NewCredentialStore creates a new credential store.
func NewCredentialStore(db *pgxpool.Pool, enc *encryption.Client) *CredentialStore {
	return &CredentialStore{
		db:  db,
		enc: enc,
	}
}

// Credential represents a decrypted OAuth credential.
type Credential struct {
	AccessToken  string
	RefreshToken string
	TokenType    string
	Scope        string
	ExpiresAt    *time.Time
}

// StoreCredentialParams contains the parameters for storing a credential.
type StoreCredentialParams struct {
	SessionID    uuid.UUID
	ProjectID    uuid.UUID
	ToolsetID    uuid.UUID
	AccessToken  string
	RefreshToken string
	TokenType    string
	Scope        string
	ExpiresAt    *time.Time
}

// StoreCredential stores an OAuth credential for a chat session and toolset.
// The access token and refresh token are encrypted before storage.
func (s *CredentialStore) StoreCredential(ctx context.Context, params StoreCredentialParams) error {
	// Encrypt the access token
	encryptedAccessToken, err := s.enc.Encrypt([]byte(params.AccessToken))
	if err != nil {
		return fmt.Errorf("encrypt access token: %w", err)
	}

	// Encrypt the refresh token if provided
	var encryptedRefreshToken []byte
	if params.RefreshToken != "" {
		encrypted, err := s.enc.Encrypt([]byte(params.RefreshToken))
		if err != nil {
			return fmt.Errorf("encrypt refresh token: %w", err)
		}
		encryptedRefreshToken = []byte(encrypted)
	}

	// Set token type default
	tokenType := params.TokenType
	if tokenType == "" {
		tokenType = "Bearer"
	}

	// Convert expires_at to pgtype
	var expiresAt pgtype.Timestamptz
	if params.ExpiresAt != nil {
		expiresAt = pgtype.Timestamptz{Time: *params.ExpiresAt, Valid: true, InfinityModifier: 0}
	}

	queries := repo.New(s.db)
	_, err = queries.UpsertCredential(ctx, repo.UpsertCredentialParams{
		ChatSessionID:         params.SessionID,
		ProjectID:             params.ProjectID,
		ToolsetID:             params.ToolsetID,
		AccessTokenEncrypted:  []byte(encryptedAccessToken),
		RefreshTokenEncrypted: encryptedRefreshToken,
		TokenType:             tokenType,
		Scope:                 conv.ToPGTextEmpty(params.Scope),
		ExpiresAt:             expiresAt,
	})
	if err != nil {
		return fmt.Errorf("upsert credential: %w", err)
	}

	return nil
}

// ErrCredentialNotFound is returned when a credential is not found.
var ErrCredentialNotFound = errors.New("credential not found")

// ErrCredentialExpired is returned when a credential has expired.
var ErrCredentialExpired = errors.New("credential expired")

// GetCredential retrieves and decrypts an OAuth credential for a chat session and toolset.
func (s *CredentialStore) GetCredential(ctx context.Context, sessionID, toolsetID uuid.UUID) (*Credential, error) {
	queries := repo.New(s.db)
	cred, err := queries.GetCredentialByToolset(ctx, repo.GetCredentialByToolsetParams{
		ChatSessionID: sessionID,
		ToolsetID:     toolsetID,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrCredentialNotFound
		}
		return nil, fmt.Errorf("get credential: %w", err)
	}

	// Check if credential has expired
	if cred.ExpiresAt.Valid && cred.ExpiresAt.Time.Before(time.Now()) {
		return nil, ErrCredentialExpired
	}

	// Decrypt the access token
	accessToken, err := s.enc.Decrypt(string(cred.AccessTokenEncrypted))
	if err != nil {
		return nil, fmt.Errorf("decrypt access token: %w", err)
	}

	// Decrypt the refresh token if present
	var refreshToken string
	if len(cred.RefreshTokenEncrypted) > 0 {
		refreshToken, err = s.enc.Decrypt(string(cred.RefreshTokenEncrypted))
		if err != nil {
			return nil, fmt.Errorf("decrypt refresh token: %w", err)
		}
	}

	var expiresAt *time.Time
	if cred.ExpiresAt.Valid {
		expiresAt = &cred.ExpiresAt.Time
	}

	return &Credential{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		TokenType:    cred.TokenType,
		Scope:        conv.PtrValOr(conv.FromPGText[string](cred.Scope), ""),
		ExpiresAt:    expiresAt,
	}, nil
}

// DeleteCredential removes a credential for a chat session and toolset.
func (s *CredentialStore) DeleteCredential(ctx context.Context, sessionID, toolsetID uuid.UUID) error {
	queries := repo.New(s.db)
	err := queries.DeleteCredential(ctx, repo.DeleteCredentialParams{
		ChatSessionID: sessionID,
		ToolsetID:     toolsetID,
	})
	if err != nil {
		return fmt.Errorf("delete credential: %w", err)
	}
	return nil
}
