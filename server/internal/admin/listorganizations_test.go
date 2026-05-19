package admin

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/conv"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
)

type orgFixture struct {
	id          string
	name        string
	slug        string
	accountType string
	workosID    *string
	whitelisted bool
	disabledAt  *time.Time
}

func seedOrg(t *testing.T, ctx context.Context, conn *pgxpool.Pool, f orgFixture) {
	t.Helper()

	if f.accountType == "" {
		f.accountType = "free"
	}

	params := orgrepo.CreateOrganizationMetadataFixtureParams{
		ID:                 f.id,
		Name:               f.name,
		Slug:               f.slug,
		GramAccountType:    f.accountType,
		WorkosID:           conv.PtrToPGText(f.workosID),
		Whitelisted:        f.whitelisted,
		FreeTrialStartedAt: conv.ToPGTimestamptz(time.Now().UTC()),
		FreeTrialEndsAt:    conv.ToPGTimestamptz(time.Now().UTC().Add(14 * 24 * time.Hour)),
		DisabledAt:         conv.PtrToPGTimestamptz(f.disabledAt),
	}

	err := orgrepo.New(conn).CreateOrganizationMetadataFixture(ctx, params)
	require.NoError(t, err)
}

func seedMembership(t *testing.T, ctx context.Context, conn *pgxpool.Pool, orgID string, userID string) {
	t.Helper()

	err := orgrepo.New(conn).CreateOrganizationUserRelationshipFixture(ctx, orgrepo.CreateOrganizationUserRelationshipFixtureParams{
		OrganizationID: orgID,
		UserID:         pgtype.Text{String: userID, Valid: true},
	})
	require.NoError(t, err)
}

func TestGetOrganization_ByID(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)

	seedOrg(t, ctx, conn, orgFixture{id: "org_get_id", name: "Get Co", slug: "get-co", accountType: "pro", whitelisted: true})
	seedMembership(t, ctx, conn, "org_get_id", "user_x")

	res, err := svc.GetOrganization(ctx, &gen.GetOrganizationPayload{IDOrSlug: "org_get_id"})
	require.NoError(t, err)
	require.NotNil(t, res)
	require.Equal(t, "org_get_id", res.ID)
	require.Equal(t, "Get Co", res.Name)
	require.Equal(t, "pro", res.AccountType)
	require.Equal(t, 1, res.MemberCount)
}

func TestGetOrganization_BySlug(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)

	seedOrg(t, ctx, conn, orgFixture{id: "org_get_slug", name: "Slug Co", slug: "slug-co", whitelisted: true})

	res, err := svc.GetOrganization(ctx, &gen.GetOrganizationPayload{IDOrSlug: "slug-co"})
	require.NoError(t, err)
	require.Equal(t, "org_get_slug", res.ID)
	require.Equal(t, "slug-co", res.Slug)
}

func TestUpdateOrganization_AccountTypeAndWhitelisted(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)

	seedOrg(t, ctx, conn, orgFixture{
		id:          "org_upd",
		name:        "Upd Co",
		slug:        "upd-co",
		accountType: "free",
		whitelisted: true,
	})

	newType := "pro"
	notWhitelisted := false
	res, err := svc.UpdateOrganization(ctx, &gen.UpdateOrganizationPayload{
		ID:          "org_upd",
		AccountType: &newType,
		Whitelisted: &notWhitelisted,
	})
	require.NoError(t, err)
	require.Equal(t, "pro", res.AccountType)
	require.False(t, res.Whitelisted)
}

func TestUpdateOrganization_AccountTypeOnly(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)

	seedOrg(t, ctx, conn, orgFixture{
		id:          "org_upd_partial",
		name:        "Upd Partial",
		slug:        "upd-partial",
		accountType: "free",
		whitelisted: true,
	})

	newType := "enterprise"
	res, err := svc.UpdateOrganization(ctx, &gen.UpdateOrganizationPayload{
		ID:          "org_upd_partial",
		AccountType: &newType,
	})
	require.NoError(t, err)
	require.Equal(t, "enterprise", res.AccountType)
	require.True(t, res.Whitelisted, "whitelisted should be untouched")
}

func TestUpdateOrganization_NoFieldsRejected(t *testing.T) {
	t.Parallel()

	ctx, svc, _ := newTestAdminService(t)
	_, err := svc.UpdateOrganization(ctx, &gen.UpdateOrganizationPayload{ID: "org_x"})
	require.Error(t, err)
}

func TestGetOrganization_NotFound(t *testing.T) {
	t.Parallel()

	ctx, svc, _ := newTestAdminService(t)

	_, err := svc.GetOrganization(ctx, &gen.GetOrganizationPayload{IDOrSlug: "does-not-exist"})
	require.Error(t, err)
}

func TestListOrganizations_Empty(t *testing.T) {
	t.Parallel()

	ctx, svc, _ := newTestAdminService(t)

	res, err := svc.ListOrganizations(ctx, &gen.ListOrganizationsPayload{})
	require.NoError(t, err)
	require.NotNil(t, res)
	require.Empty(t, res.Organizations)
	require.Nil(t, res.NextCursor)
}

func TestListOrganizations_DefaultExcludesDisabled(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)

	now := time.Now().UTC()
	seedOrg(t, ctx, conn, orgFixture{id: "org_active", name: "Active Co", slug: "active-co", whitelisted: true})
	seedOrg(t, ctx, conn, orgFixture{id: "org_disabled", name: "Disabled Co", slug: "disabled-co", whitelisted: true, disabledAt: &now})

	res, err := svc.ListOrganizations(ctx, &gen.ListOrganizationsPayload{})
	require.NoError(t, err)
	require.Len(t, res.Organizations, 1)
	require.Equal(t, "org_active", res.Organizations[0].ID)
	require.Nil(t, res.Organizations[0].DisabledAt)
}

func TestListOrganizations_IncludeDisabled(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)

	now := time.Now().UTC()
	seedOrg(t, ctx, conn, orgFixture{id: "org_a", name: "Alpha", slug: "alpha", whitelisted: true})
	seedOrg(t, ctx, conn, orgFixture{id: "org_b", name: "Bravo", slug: "bravo", whitelisted: true, disabledAt: &now})

	include := true
	res, err := svc.ListOrganizations(ctx, &gen.ListOrganizationsPayload{IncludeDisabled: &include})
	require.NoError(t, err)
	require.Len(t, res.Organizations, 2)

	byID := map[string]*gen.AdminOrganization{}
	for _, o := range res.Organizations {
		byID[o.ID] = o
	}
	require.NotNil(t, byID["org_b"].DisabledAt)
}

func TestListOrganizations_SearchByNameOrSlug(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)

	seedOrg(t, ctx, conn, orgFixture{id: "org_match_name", name: "Acme Industries", slug: "acme-ind", whitelisted: true})
	seedOrg(t, ctx, conn, orgFixture{id: "org_match_slug", name: "Globex", slug: "acme-rivals", whitelisted: true})
	seedOrg(t, ctx, conn, orgFixture{id: "org_nope", name: "Unrelated", slug: "no-match", whitelisted: true})

	q := "acme"
	res, err := svc.ListOrganizations(ctx, &gen.ListOrganizationsPayload{Q: &q})
	require.NoError(t, err)
	require.Len(t, res.Organizations, 2)

	ids := []string{res.Organizations[0].ID, res.Organizations[1].ID}
	require.Contains(t, ids, "org_match_name")
	require.Contains(t, ids, "org_match_slug")
}

func TestListOrganizations_FilterByAccountType(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)

	seedOrg(t, ctx, conn, orgFixture{id: "org_pro", name: "Pro Co", slug: "pro-co", accountType: "pro", whitelisted: true})
	seedOrg(t, ctx, conn, orgFixture{id: "org_free", name: "Free Co", slug: "free-co", accountType: "free", whitelisted: true})

	at := "pro"
	res, err := svc.ListOrganizations(ctx, &gen.ListOrganizationsPayload{AccountType: &at})
	require.NoError(t, err)
	require.Len(t, res.Organizations, 1)
	require.Equal(t, "org_pro", res.Organizations[0].ID)
	require.Equal(t, "pro", res.Organizations[0].AccountType)
}

func TestListOrganizations_MemberCount(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)

	seedOrg(t, ctx, conn, orgFixture{id: "org_members", name: "Members Co", slug: "members-co", whitelisted: true})
	seedOrg(t, ctx, conn, orgFixture{id: "org_solo", name: "Solo Co", slug: "solo-co", whitelisted: true})

	seedMembership(t, ctx, conn, "org_members", "user_1")
	seedMembership(t, ctx, conn, "org_members", "user_2")
	seedMembership(t, ctx, conn, "org_members", "user_3")

	res, err := svc.ListOrganizations(ctx, &gen.ListOrganizationsPayload{})
	require.NoError(t, err)
	require.Len(t, res.Organizations, 2)

	byID := map[string]*gen.AdminOrganization{}
	for _, o := range res.Organizations {
		byID[o.ID] = o
	}
	require.Equal(t, 3, byID["org_members"].MemberCount)
	require.Equal(t, 0, byID["org_solo"].MemberCount)
}

func TestListOrganizations_CursorPagination(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)

	for _, id := range []string{"org_a", "org_b", "org_c", "org_d"} {
		seedOrg(t, ctx, conn, orgFixture{id: id, name: "Org " + id, slug: id, whitelisted: true})
	}

	limit := 2

	page1, err := svc.ListOrganizations(ctx, &gen.ListOrganizationsPayload{Limit: &limit})
	require.NoError(t, err)
	require.Len(t, page1.Organizations, 2)
	require.NotNil(t, page1.NextCursor)
	require.Equal(t, "org_a", page1.Organizations[0].ID)
	require.Equal(t, "org_b", page1.Organizations[1].ID)
	require.Equal(t, "org_b", *page1.NextCursor)

	page2, err := svc.ListOrganizations(ctx, &gen.ListOrganizationsPayload{Limit: &limit, Cursor: page1.NextCursor})
	require.NoError(t, err)
	require.Len(t, page2.Organizations, 2)
	require.Equal(t, "org_c", page2.Organizations[0].ID)
	require.Equal(t, "org_d", page2.Organizations[1].ID)
	// Final page is full but exhausts, so next call returns empty + nil cursor.
	require.NotNil(t, page2.NextCursor)
	require.Equal(t, "org_d", *page2.NextCursor)

	page3, err := svc.ListOrganizations(ctx, &gen.ListOrganizationsPayload{Limit: &limit, Cursor: page2.NextCursor})
	require.NoError(t, err)
	require.Empty(t, page3.Organizations)
	require.Nil(t, page3.NextCursor)
}
