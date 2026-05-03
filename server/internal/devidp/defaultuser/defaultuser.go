// Package defaultuser implements the dev-idp's "default user" bootstrap
// (idp-design.md §3) — when an identity-resolving endpoint runs before
// any operator/test has called /rpc/devIdp.setCurrentUser, the dev-idp
// falls back to a user derived from the local git committer config.
//
// Local modes (mock-speakeasy / oauth2-1 / oauth2) synthesize a row in
// the dev-idp's users table with the committer email + name and place it
// in a "Speakeasy" organization. Each mode's currentUser is then upserted
// to point at that user, so the bootstrap fires at most once per mode
// per dev-idp database.
//
// The workos mode does its own bootstrap path (it can't synthesize — its
// identity universe is the live WorkOS account) and only borrows the
// GitCommitter helper from this package.
package defaultuser

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/devidp/database/repo"
)

// DefaultOrgName is the organization the bootstrap places the default
// user in. The slug is the lowercased name.
const (
	DefaultOrgName = "Speakeasy"
	DefaultOrgSlug = "speakeasy"
)

// Committer holds the values read from `git config user.{email,name}`.
type Committer struct {
	Email string
	Name  string
}

// GitCommitter shells out to `git config --get user.email` and
// `git config --get user.name`. Returns a wrapped error whose message
// names committer email/name as the default-user source so callers can
// surface it directly.
func GitCommitter(ctx context.Context) (Committer, error) {
	email, err := gitConfig(ctx, "user.email")
	if err != nil {
		return Committer{}, fmt.Errorf("the default user relies on committer email from `git config user.email`: %w", err)
	}
	name, err := gitConfig(ctx, "user.name")
	if err != nil {
		return Committer{}, fmt.Errorf("the default user relies on committer name from `git config user.name`: %w", err)
	}
	return Committer{Email: email, Name: name}, nil
}

func gitConfig(ctx context.Context, key string) (string, error) {
	// `key` is constrained to the two hardcoded callers above. Surfaced as
	// a parameter only to keep the wiring readable, so gosec's G204 is a
	// false positive here.
	out, err := exec.CommandContext(ctx, "git", "config", "--get", key).Output() //nolint:gosec // hardcoded keys; dev-idp is local-only test infra
	if err != nil {
		return "", fmt.Errorf("run git config --get %s: %w", key, err)
	}
	v := strings.TrimSpace(string(out))
	if v == "" {
		return "", errors.New("git config returned an empty value")
	}
	return v, nil
}

// BootstrapLocalUser is the local-mode bootstrap path. Idempotent —
// repeated calls converge on the same user/org/membership rows and the
// same currentUser entry. Returns the bootstrapped user's id.
func BootstrapLocalUser(ctx context.Context, db *pgxpool.Pool, mode string) (uuid.UUID, error) {
	committer, err := GitCommitter(ctx)
	if err != nil {
		return uuid.Nil, err
	}

	queries := repo.New(db)

	user, err := queries.UpsertUserByEmail(ctx, repo.UpsertUserByEmailParams{
		Email:       committer.Email,
		DisplayName: committer.Name,
	})
	if err != nil {
		return uuid.Nil, fmt.Errorf("upsert default user: %w", err)
	}

	org, err := queries.UpsertOrganizationBySlug(ctx, repo.UpsertOrganizationBySlugParams{
		Name: DefaultOrgName,
		Slug: DefaultOrgSlug,
	})
	if err != nil {
		return uuid.Nil, fmt.Errorf("upsert default organization: %w", err)
	}

	if _, err := queries.CreateMembership(ctx, repo.CreateMembershipParams{
		UserID:         user.ID,
		OrganizationID: org.ID,
		Role:           pgtype.Text{String: "", Valid: false},
	}); err != nil {
		return uuid.Nil, fmt.Errorf("upsert default membership: %w", err)
	}

	if _, err := queries.UpsertCurrentUser(ctx, repo.UpsertCurrentUserParams{
		Mode:       mode,
		SubjectRef: user.ID.String(),
	}); err != nil {
		return uuid.Nil, fmt.Errorf("upsert default currentUser for %s: %w", mode, err)
	}

	return user.ID, nil
}
