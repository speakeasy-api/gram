package devidptest

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/dev-idp/internal/database/repo"
)

// DefaultClientID is the client_id stamped onto fixture refresh tokens and
// recommended as the default client_id for test OAuth flows against
// dev-idp. The dev-idp's refresh path does not validate the form's
// client_id against the token's stored client_id, but using a single
// constant keeps test code consistent and grep-friendly.
const DefaultClientID = "test-client"

const defaultRefreshTTL = 30 * 24 * time.Hour

// RefreshTokenOpts configures CreateRefreshToken. Token, Mode, and UserID
// are required.
type RefreshTokenOpts struct {
	// Token is the opaque refresh-token string to insert. Whoever later
	// presents this to /<mode>/token with grant_type=refresh_token will
	// receive a fresh access+refresh token pair (oauth2-1) or a fresh
	// access token reusing the same refresh (oauth2). Required.
	Token string

	// Mode picks which mode owns the row. Use oauth21.Mode ("oauth2-1")
	// or oauth2.Mode ("oauth2"); the mode handler scopes its lookups to
	// its own mode, so a token stamped with the wrong mode is invisible.
	// Required.
	Mode string

	// UserID is the user the token is bound to. Required — the tokens
	// row has a NOT NULL FOREIGN KEY into users(id), so a real user must
	// already exist. Pass Instance.DefaultUser.ID for the auto-seeded
	// default user, or a row created via CreateUser.
	UserID uuid.UUID

	// ClientID is recorded for inspection only — the dev-idp's refresh
	// path doesn't validate the form's client_id against the token's
	// stored client_id. Defaults to "test-client".
	ClientID string

	// Scope, when non-empty, is stored alongside the token. The dev-idp
	// preserves it across refresh.
	Scope string

	// ExpiresAt overrides the token's expiry. Defaults to 30 days from
	// now (matches the dev-idp's refresh-token lifetime).
	ExpiresAt time.Time
}

// RefreshTokenResult holds the rows created by CreateRefreshToken.
type RefreshTokenResult struct {
	Token repo.Token
}

// CreateRefreshToken inserts a refresh token directly into the dev-idp's
// tokens table, bypassing DCR + the auth-code flow. Useful when a Gram-side
// test needs to wrap a known upstream refresh-token string in a Gram-issued
// token without driving the full authorization dance.
//
// The dev-idp's /token refresh handler (oauth2-1 and oauth2) validates the
// presented refresh token against the tokens table by (token, mode); it
// does NOT validate client_id, so any value works there.
func CreateRefreshToken(t *testing.T, ctx context.Context, q *repo.Queries, opts RefreshTokenOpts) RefreshTokenResult {
	t.Helper()

	require.NotEmpty(t, opts.Token, "RefreshTokenOpts.Token is required")
	require.NotEmpty(t, opts.Mode, "RefreshTokenOpts.Mode is required")
	require.NotEqual(t, uuid.Nil, opts.UserID, "RefreshTokenOpts.UserID is required")

	clientID := opts.ClientID
	if clientID == "" {
		clientID = DefaultClientID
	}

	expiresAt := opts.ExpiresAt
	if expiresAt.IsZero() {
		expiresAt = time.Now().Add(defaultRefreshTTL)
	}

	scope := sql.NullString{}
	if opts.Scope != "" {
		scope = sql.NullString{String: opts.Scope, Valid: true}
	}

	tok, err := q.CreateToken(ctx, repo.CreateTokenParams{
		Token:     opts.Token,
		Mode:      opts.Mode,
		UserID:    opts.UserID,
		ClientID:  clientID,
		Kind:      "refresh_token",
		Scope:     scope,
		ExpiresAt: expiresAt,
	})
	require.NoError(t, err, "create dev-idp refresh token")
	return RefreshTokenResult{Token: tok}
}
