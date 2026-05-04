// Package defaultuser implements the dev-idp's "default user" bootstrap
// (idp-design.md §3) — when an identity-resolving endpoint runs before
// any operator/test has called /rpc/devIdp.setCurrentUser, the dev-idp
// falls back to a user derived from the local git committer config.
//
// Local modes (local-speakeasy / oauth2-1 / oauth2) synthesize a row in
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

	// Seed the two default roles WorkOS provisions on every org so the
	// local-speakeasy WorkOS-emulation endpoints have something to return
	// from /authorization/organizations/{id}/roles even before any test
	// calls CreateRole.
	for _, r := range []repo.UpsertOrganizationRoleParams{
		{OrganizationID: org.ID, Slug: "admin", Name: "Admin", Description: pgtype.Text{String: "", Valid: false}},
		{OrganizationID: org.ID, Slug: "member", Name: "Member", Description: pgtype.Text{String: "", Valid: false}},
	} {
		if _, err := queries.UpsertOrganizationRole(ctx, r); err != nil {
			return uuid.Nil, fmt.Errorf("seed default org role %q: %w", r.Slug, err)
		}
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
