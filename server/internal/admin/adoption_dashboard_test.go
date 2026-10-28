package admin

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestSetAdoptionDashboardURL_SetsAndReadsBack(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)
	seedOrg(t, ctx, conn, orgFixture{id: "org_adopt", name: "Adopt Co", slug: "adopt-co", whitelisted: true})

	link := "https://claude.ai/artifact/example"
	res, err := svc.SetAdoptionDashboardURL(ctx, &gen.SetAdoptionDashboardURLPayload{
		OrganizationID:       "org_adopt",
		AdoptionDashboardURL: conv.PtrEmpty("  " + link + "  "),
	})
	require.NoError(t, err)
	require.NotNil(t, res.AdoptionDashboardURL)
	require.Equal(t, link, *res.AdoptionDashboardURL, "the stored link must be trimmed")

	got, err := svc.GetOrganization(ctx, &gen.GetOrganizationPayload{IDOrSlug: "org_adopt"})
	require.NoError(t, err)
	require.NotNil(t, got.AdoptionDashboardURL)
	require.Equal(t, link, *got.AdoptionDashboardURL)
}

func TestSetAdoptionDashboardURL_EmptyClears(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)
	seedOrg(t, ctx, conn, orgFixture{id: "org_adopt_clear", name: "Clear Co", slug: "clear-co", whitelisted: true})

	_, err := svc.SetAdoptionDashboardURL(ctx, &gen.SetAdoptionDashboardURLPayload{
		OrganizationID:       "org_adopt_clear",
		AdoptionDashboardURL: conv.PtrEmpty("https://claude.ai/artifact/example"),
	})
	require.NoError(t, err)

	for _, cleared := range []*string{nil, conv.PtrEmpty("   ")} {
		res, err := svc.SetAdoptionDashboardURL(ctx, &gen.SetAdoptionDashboardURLPayload{
			OrganizationID:       "org_adopt_clear",
			AdoptionDashboardURL: cleared,
		})
		require.NoError(t, err)
		require.Nil(t, res.AdoptionDashboardURL)
	}
}

func TestSetAdoptionDashboardURL_RejectsNonHTTPS(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)
	seedOrg(t, ctx, conn, orgFixture{id: "org_adopt_bad", name: "Bad Co", slug: "bad-co", whitelisted: true})

	for _, bad := range []string{
		"http://claude.ai/artifact/example",
		"javascript:alert(1)",
		"claude.ai/artifact/example",
		"https://user:pass@claude.ai/artifact/example",
		"https://" + strings.Repeat("a", maxAdoptionDashboardURLLength),
	} {
		_, err := svc.SetAdoptionDashboardURL(ctx, &gen.SetAdoptionDashboardURLPayload{
			OrganizationID:       "org_adopt_bad",
			AdoptionDashboardURL: conv.PtrEmpty(bad),
		})
		requireOopsCode(t, err, oops.CodeBadRequest)
	}
}

func TestSetAdoptionDashboardURL_UnknownOrganization(t *testing.T) {
	t.Parallel()

	ctx, svc, _ := newTestAdminService(t)

	_, err := svc.SetAdoptionDashboardURL(ctx, &gen.SetAdoptionDashboardURLPayload{
		OrganizationID:       "org_missing",
		AdoptionDashboardURL: conv.PtrEmpty("https://claude.ai/artifact/example"),
	})
	requireOopsCode(t, err, oops.CodeNotFound)
}
