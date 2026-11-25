package adminmcp

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/adminmcp/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
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
	q := repo.New(tx)
	clientID, err := q.LockOAuthClientForConsent(ctx, input.ClientID)
	if errors.Is(err, pgx.ErrNoRows) {
		return errStaffGrant
	}
	if err != nil {
		return fmt.Errorf("lock staff MCP client: %w", err)
	}
	connectionID, err := q.UpsertStaffConnection(ctx, repo.UpsertStaffConnectionParams{
		SubjectUrn:             input.Subject,
		OauthClientID:          clientID,
		AdminSessionIDEnc:      input.SessionEnc,
		Scopes:                 input.Scopes,
		ResourceUri:            input.ResourceURI,
		AuthorizationExpiresAt: conv.ToPGTimestamptz(input.ExpiresAt),
	})
	if err != nil {
		return fmt.Errorf("authorize staff MCP connection: %w", err)
	}
	err = q.InsertAuthorizationGrant(ctx, repo.InsertAuthorizationGrantParams{
		AuthorizationCodeHash: input.CodeHash,
		OauthClientID:         clientID,
		RedirectUri:           input.RedirectURI,
		CodeChallenge:         input.CodeChallenge,
		Scopes:                input.Scopes,
		ResourceUri:           input.ResourceURI,
		ExpiresAt:             conv.ToPGTimestamptz(input.GrantExpires),
		ConnectionID:          connectionID,
	})
	if err != nil {
		return fmt.Errorf("create staff MCP authorization grant: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit staff MCP authorization: %w", err)
	}
	return nil
}
