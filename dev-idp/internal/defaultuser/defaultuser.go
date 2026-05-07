// Package defaultuser implements the dev-idp's "default user" bootstrap.
// When an identity-resolving endpoint runs before any operator/test has
// called /rpc/devIdp.setCurrentUser, the dev-idp falls back to a user
// derived from the local git committer config.
//
// Local modes (local-speakeasy / oauth2-1 / oauth2) synthesize a row in
// the dev-idp's users table with the committer email + name and place it
// in a "Speakeasy" organization. Each mode's currentUser is then upserted
// to point at that user, so the bootstrap fires at most once per mode
// per dev-idp database.
//
// The workos mode does its own bootstrap path (it can't synthesize -- its
// identity universe is the live WorkOS account) and only borrows the
// GitCommitter helper from this package.
package defaultuser

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/dev-idp/internal/database/repo"
)

const (
	DefaultOrgName = "Speakeasy"
	DefaultOrgSlug = "speakeasy"
)

// userIDNamespace is a fixed UUID v5 namespace used to derive deterministic
// user IDs from email addresses. This ensures the same email always maps to
// the same UUID, surviving dev-idp SQLite resets without colliding with the
// Gram server's users_email_key unique constraint.
var userIDNamespace = uuid.MustParse("a1b2c3d4-e5f6-4a7b-8c9d-0e1f2a3b4c5d")

// DeterministicUserID returns a stable UUID v5 derived from the given email.
func DeterministicUserID(email string) uuid.UUID {
	return uuid.NewSHA1(userIDNamespace, []byte(email))
}

// Committer holds the values read from `git config user.{email,name}`.
type Committer struct {
	Email string
	Name  string
}

// GitCommitter shells out to `git config --get user.email` and
// `git config --get user.name`.
func GitCommitter(ctx context.Context) (Committer, error) {
	emailOut, err := exec.CommandContext(ctx, "git", "config", "--get", "user.email").Output()
	if err != nil {
		return Committer{}, fmt.Errorf("the default user relies on committer email from `git config user.email`: %w", err)
	}
	email := strings.TrimSpace(string(emailOut))
	if email == "" {
		return Committer{}, errors.New("the default user relies on committer email from `git config user.email`: returned empty")
	}

	nameOut, err := exec.CommandContext(ctx, "git", "config", "--get", "user.name").Output()
	if err != nil {
		return Committer{}, fmt.Errorf("the default user relies on committer name from `git config user.name`: %w", err)
	}
	name := strings.TrimSpace(string(nameOut))
	if name == "" {
		return Committer{}, errors.New("the default user relies on committer name from `git config user.name`: returned empty")
	}

	return Committer{Email: email, Name: name}, nil
}

// BootstrapLocalUser is the local-mode bootstrap path. Idempotent --
// repeated calls converge on the same user/org/membership rows and the
// same currentUser entry. Returns the bootstrapped user's id.
func BootstrapLocalUser(ctx context.Context, db *sql.DB, mode string) (uuid.UUID, error) {
	committer, err := GitCommitter(ctx)
	if err != nil {
		return uuid.Nil, err
	}

	queries := repo.New(db)
	now := time.Now()

	user, err := queries.UpsertUserByEmail(ctx, repo.UpsertUserByEmailParams{
		ID:          DeterministicUserID(committer.Email),
		Email:       committer.Email,
		DisplayName: committer.Name,
	})
	if err != nil {
		return uuid.Nil, fmt.Errorf("upsert default user: %w", err)
	}

	org, err := queries.UpsertOrganizationBySlug(ctx, repo.UpsertOrganizationBySlugParams{
		ID:   uuid.New(),
		Name: DefaultOrgName,
		Slug: DefaultOrgSlug,
	})
	if err != nil {
		return uuid.Nil, fmt.Errorf("upsert default organization: %w", err)
	}

	// Seed the two default roles WorkOS provisions on every org so the
	// local-speakeasy WorkOS-emulation endpoints have something to return
	// from /authorization/organizations/{id}/roles even before any test
	// calls CreateRole.
	for _, r := range []struct {
		Slug, Name string
	}{
		{Slug: "admin", Name: "Admin"},
		{Slug: "member", Name: "Member"},
	} {
		if _, err := queries.UpsertOrganizationRole(ctx, repo.UpsertOrganizationRoleParams{
			ID:             uuid.New(),
			OrganizationID: org.ID,
			Slug:           r.Slug,
			Name:           r.Name,
			Description:    sql.NullString{},
		}); err != nil {
			return uuid.Nil, fmt.Errorf("seed default org role %q: %w", r.Slug, err)
		}
	}

	if _, err := queries.CreateMembership(ctx, repo.CreateMembershipParams{
		ID:             uuid.New(),
		UserID:         user.ID,
		OrganizationID: org.ID,
		Role:           sql.NullString{},
	}); err != nil {
		return uuid.Nil, fmt.Errorf("upsert default membership: %w", err)
	}

	if _, err := queries.UpsertCurrentUser(ctx, repo.UpsertCurrentUserParams{
		Mode:       mode,
		SubjectRef: user.ID.String(),
		Ts:         now,
	}); err != nil {
		return uuid.Nil, fmt.Errorf("upsert default currentUser for %s: %w", mode, err)
	}

	return user.ID, nil
}
