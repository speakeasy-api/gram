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

// OrganizationOpts configures CreateOrganization.
type OrganizationOpts struct {
	// Name overrides the org's display name. Defaults to "Test Org".
	Name string

	// Slug overrides the org's slug. When empty, a unique
	// "test-org-<uuid8>" slug is used so parallel tests don't collide on
	// the organizations_slug_key unique index.
	Slug string

	// AccountType overrides the row's account_type column. Defaults to
	// the schema default ("enterprise").
	AccountType string

	// WorkOSID, when non-empty, is stored on the row.
	WorkOSID string
}

// OrganizationResult holds the rows created by CreateOrganization.
type OrganizationResult struct {
	Organization repo.Organization
}

// CreateOrganization inserts a row into the dev-idp organizations table.
// The underlying CreateOrganization query is find-or-create on slug — if
// the supplied (or generated) slug already exists, the existing row is
// returned.
func CreateOrganization(t *testing.T, ctx context.Context, q *repo.Queries, opts OrganizationOpts) OrganizationResult {
	t.Helper()

	name := opts.Name
	if name == "" {
		name = "Test Org"
	}
	slug := opts.Slug
	if slug == "" {
		slug = fmt.Sprintf("test-org-%s", uuid.New().String()[:8])
	}

	var accountType any
	if opts.AccountType != "" {
		accountType = opts.AccountType
	}

	org, err := q.CreateOrganization(ctx, repo.CreateOrganizationParams{
		ID:          uuid.New(),
		Name:        name,
		Slug:        slug,
		AccountType: accountType,
		WorkosID:    conv.StringOrNull(opts.WorkOSID),
	})
	require.NoError(t, err, "create dev-idp organization")
	return OrganizationResult{Organization: org}
}
