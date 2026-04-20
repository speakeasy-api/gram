package organizations_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/organizations"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
)

// Must match the const in impl.go. Kept local to avoid exporting the constant
// just for tests.
const testSpeakeasyTeamOrgID = "5a25158b-24dc-4d49-b03d-e85acfbea59c"

// adminCtx upserts the speakeasy-team org into the test DB and returns a
// context whose auth context is scoped to that org, so admin-gated methods
// pass the check.
func adminCtx(t *testing.T, ctx context.Context, conn *pgxpool.Pool) context.Context {
	t.Helper()

	_, err := orgrepo.New(conn).UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{
		ID:          testSpeakeasyTeamOrgID,
		Name:        "Speakeasy Team",
		Slug:        "speakeasy-team",
		WorkosID:    pgtype.Text{String: "workos_speakeasy_team", Valid: true},
		Whitelisted: pgtype.Bool{Bool: true, Valid: true},
	})
	require.NoError(t, err)

	existing, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	clone := *existing
	clone.ActiveOrganizationID = testSpeakeasyTeamOrgID
	return contextvalues.SetAuthContext(ctx, &clone)
}

func seedOrg(t *testing.T, ctx context.Context, conn *pgxpool.Pool, id, slug, name, tier string) {
	t.Helper()

	_, err := orgrepo.New(conn).UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{
		ID:          id,
		Name:        name,
		Slug:        slug,
		WorkosID:    pgtype.Text{String: "workos_" + id, Valid: true},
		Whitelisted: pgtype.Bool{Bool: true, Valid: true},
	})
	require.NoError(t, err)

	if tier != "" {
		_, err = orgrepo.New(conn).SetAccountType(ctx, orgrepo.SetAccountTypeParams{
			ID:              id,
			GramAccountType: tier,
		})
		require.NoError(t, err)
	}
}

func TestService_ListAll_HappyPath(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)
	ctx = adminCtx(t, ctx, ti.conn)

	seedOrg(t, ctx, ti.conn, "org-listall-a", "acme", "Acme", "enterprise")
	seedOrg(t, ctx, ti.conn, "org-listall-b", "globex", "Globex", "pro")

	res, err := ti.service.ListAll(ctx, &gen.ListAllPayload{})
	require.NoError(t, err)
	require.NotNil(t, res)
	require.Equal(t, 100, res.Limit, "default limit")
	require.Equal(t, 0, res.Offset, "default offset")
	require.GreaterOrEqual(t, res.Total, 3, "speakeasy-team + 2 seeded + default auth org")

	slugs := make(map[string]string, len(res.Organizations))
	for _, o := range res.Organizations {
		slugs[o.Slug] = o.GramAccountType
	}
	require.Contains(t, slugs, "speakeasy-team")
	require.Equal(t, "enterprise", slugs["acme"])
	require.Equal(t, "pro", slugs["globex"])
}

func TestService_ListAll_Unauthorized(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)
	// No adminCtx — the default auth context is in a mock-idp org, not speakeasy-team.

	res, err := ti.service.ListAll(ctx, &gen.ListAllPayload{})
	require.Error(t, err)
	require.Nil(t, res)

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeUnauthorized, oopsErr.Code)
}

func TestService_ListAll_Pagination(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)
	ctx = adminCtx(t, ctx, ti.conn)

	for i := 0; i < 5; i++ {
		seedOrg(t, ctx, ti.conn,
			fmt.Sprintf("org-page-%d", i),
			fmt.Sprintf("page-%d", i),
			fmt.Sprintf("Page %d", i),
			"free",
		)
	}

	limit := 2
	offset := 1
	res, err := ti.service.ListAll(ctx, &gen.ListAllPayload{Limit: &limit, Offset: &offset})
	require.NoError(t, err)
	require.NotNil(t, res)
	require.Equal(t, 2, res.Limit)
	require.Equal(t, 1, res.Offset)
	require.Len(t, res.Organizations, 2)
	require.GreaterOrEqual(t, res.Total, 6)
}

func TestService_SetAccountType_HappyPath(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)
	ctx = adminCtx(t, ctx, ti.conn)
	seedOrg(t, ctx, ti.conn, "org-settier", "settier", "SetTier", "free")

	err := ti.service.SetAccountType(ctx, &gen.SetAccountTypePayload{
		OrganizationID:  "org-settier",
		GramAccountType: "enterprise",
	})
	require.NoError(t, err)

	got, err := orgrepo.New(ti.conn).GetOrganizationMetadata(ctx, "org-settier")
	require.NoError(t, err)
	require.Equal(t, "enterprise", got.GramAccountType)
}

func TestService_SetAccountType_Unauthorized(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)
	// No adminCtx.

	err := ti.service.SetAccountType(ctx, &gen.SetAccountTypePayload{
		OrganizationID:  "anything",
		GramAccountType: "enterprise",
	})
	require.Error(t, err)

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeUnauthorized, oopsErr.Code)
}

func TestService_SetAccountType_NotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)
	ctx = adminCtx(t, ctx, ti.conn)

	err := ti.service.SetAccountType(ctx, &gen.SetAccountTypePayload{
		OrganizationID:  "does-not-exist",
		GramAccountType: "enterprise",
	})
	require.Error(t, err)

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeNotFound, oopsErr.Code)
}
