package devidptest

import (
	"context"
	"database/sql"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/dev-idp/internal/database/repo"
)

// MembershipOpts configures CreateMembership. UserID and OrganizationID are
// required.
type MembershipOpts struct {
	// UserID is the user being added to the org. Required.
	UserID uuid.UUID

	// OrganizationID is the org the user is being added to. Required.
	OrganizationID uuid.UUID

	// Role overrides the membership role. Defaults to the schema default
	// ("admin"). Use "member" to seed a non-admin.
	Role string
}

// MembershipResult holds the rows created by CreateMembership.
type MembershipResult struct {
	Membership repo.Membership
}

// CreateMembership inserts a row into the dev-idp memberships table. The
// underlying CreateMembership query is find-or-create on
// (user_id, organization_id) — if a membership already exists, the
// existing row is returned and the supplied Role is ignored.
func CreateMembership(t *testing.T, ctx context.Context, q *repo.Queries, opts MembershipOpts) MembershipResult {
	t.Helper()

	require.NotEqual(t, uuid.Nil, opts.UserID, "MembershipOpts.UserID is required")
	require.NotEqual(t, uuid.Nil, opts.OrganizationID, "MembershipOpts.OrganizationID is required")

	role := sql.NullString{}
	if opts.Role != "" {
		role = sql.NullString{String: opts.Role, Valid: true}
	}

	m, err := q.CreateMembership(ctx, repo.CreateMembershipParams{
		ID:             uuid.New(),
		UserID:         opts.UserID,
		OrganizationID: opts.OrganizationID,
		Role:           role,
	})
	require.NoError(t, err, "create dev-idp membership")
	return MembershipResult{Membership: m}
}
