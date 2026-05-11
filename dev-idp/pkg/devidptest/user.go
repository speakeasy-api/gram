package devidptest

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/dev-idp/internal/conv"
	"github.com/speakeasy-api/gram/dev-idp/internal/database/repo"
)

// UserOpts configures CreateUser. The zero value is valid: a unique email
// and a default display name are generated.
type UserOpts struct {
	// Email overrides the user's email. When empty, a unique
	// "user-<uuid8>@devidptest.local" address is used so parallel tests
	// don't collide on the users_email_key unique index.
	Email string

	// DisplayName overrides the user's display name. Defaults to
	// "Test User".
	DisplayName string

	// PhotoURL, when non-empty, is stored on the row.
	PhotoURL string

	// GithubHandle, when non-empty, is stored on the row.
	GithubHandle string

	// Admin marks the user as a dev-idp admin. Defaults to false.
	Admin bool
}

// UserResult holds the rows created by CreateUser.
type UserResult struct {
	User repo.User
}

// CreateUser inserts a row into the dev-idp users table. The underlying
// CreateUser query is find-or-create on email — if the supplied (or
// generated) email already exists, the existing row is returned.
func CreateUser(t *testing.T, ctx context.Context, q *repo.Queries, opts UserOpts) UserResult {
	t.Helper()

	email := opts.Email
	if email == "" {
		email = fmt.Sprintf("user-%s@devidptest.local", uuid.New().String()[:8])
	}
	displayName := opts.DisplayName
	if displayName == "" {
		displayName = defaultUserDisplayName
	}

	user, err := q.CreateUser(ctx, repo.CreateUserParams{
		ID:           uuid.New(),
		Email:        email,
		DisplayName:  displayName,
		PhotoUrl:     conv.StringOrNull(opts.PhotoURL),
		GithubHandle: conv.StringOrNull(opts.GithubHandle),
		Admin:        opts.Admin,
		Whitelisted:  true,
	})
	require.NoError(t, err, "create dev-idp user")
	return UserResult{User: user}
}
