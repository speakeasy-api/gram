package admin

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func resolvedIDs(orgs []*gen.AdminResolvedOrganization) []string {
	ids := make([]string, 0, len(orgs))
	for _, org := range orgs {
		ids = append(ids, org.ID)
	}
	return ids
}

func TestResolveOrganizations_MatchesIDsAndSlugsAndReportsMisses(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)
	disabledAt := time.Date(2026, time.March, 1, 0, 0, 0, 0, time.UTC)
	seedOrg(t, ctx, conn, orgFixture{id: "org_r1", name: "One", slug: "one", accountType: "pro", whitelisted: true})
	seedOrg(t, ctx, conn, orgFixture{id: "org_r2", name: "Two", slug: "two", accountType: "free", whitelisted: false, disabledAt: &disabledAt})
	seedOrg(t, ctx, conn, orgFixture{id: "org_r3", name: "Three", slug: "three", whitelisted: true})

	res, err := svc.ResolveOrganizations(ctx, &gen.ResolveOrganizationsPayload{
		// org_r1 is named twice and once more by slug; it must be listed once.
		Ids:   []string{"org_r2", "org_r1", "org_nope", "org_r1"},
		Slugs: []string{"one", "three", "ghost"},
	})
	require.NoError(t, err)

	require.Equal(t, []string{"org_r2", "org_r1", "org_r3"}, resolvedIDs(res.Organizations), "ids first, then slugs, each organization once")
	require.Equal(t, []string{"org_nope"}, res.MissingIds)
	require.Equal(t, []string{"ghost"}, res.MissingSlugs)

	two := res.Organizations[0]
	require.Equal(t, "Two", two.Name)
	require.Equal(t, "two", two.Slug)
	require.Equal(t, "free", two.AccountType)
	require.False(t, two.Whitelisted)
	require.NotNil(t, two.DisabledAt, "an already-disabled organization must say so in the review")
	require.Nil(t, res.Organizations[1].DisabledAt)
}

func TestResolveOrganizations_IDDoesNotMatchAnotherOrganizationsSlug(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)
	// This organization's slug is spelled like an id. Asking for that id must
	// not find it.
	seedOrg(t, ctx, conn, orgFixture{id: "org_cross", name: "Cross", slug: "org_lookalike", whitelisted: true})

	res, err := svc.ResolveOrganizations(ctx, &gen.ResolveOrganizationsPayload{Ids: []string{"org_lookalike"}, Slugs: nil})
	require.NoError(t, err)
	require.Empty(t, res.Organizations)
	require.Equal(t, []string{"org_lookalike"}, res.MissingIds)
}

func TestResolveOrganizations_RejectsBadInput(t *testing.T) {
	t.Parallel()

	ctx, svc, _ := newTestAdminService(t)

	_, err := svc.ResolveOrganizations(ctx, &gen.ResolveOrganizationsPayload{Ids: nil, Slugs: nil})
	requireOopsCode(t, err, oops.CodeBadRequest)

	_, err = svc.ResolveOrganizations(ctx, &gen.ResolveOrganizationsPayload{Ids: []string{""}, Slugs: nil})
	requireOopsCode(t, err, oops.CodeBadRequest)

	half := constants.MaxResolveOrganizationsBatch/2 + 1
	ids := make([]string, half)
	slugs := make([]string, half)
	for i := range half {
		ids[i] = fmt.Sprintf("org_%d", i)
		slugs[i] = fmt.Sprintf("slug_%d", i)
	}
	_, err = svc.ResolveOrganizations(ctx, &gen.ResolveOrganizationsPayload{Ids: ids, Slugs: slugs})
	requireOopsCode(t, err, oops.CodeBadRequest)
}
