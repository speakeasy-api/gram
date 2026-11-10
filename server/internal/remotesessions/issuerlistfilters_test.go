package remotesessions_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/remote_session_issuers"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func listProjectIssuerIDs(t *testing.T, ctx context.Context, ti *testInstance, search, upstreamHost *string) map[string]bool {
	t.Helper()
	return listProjectIssuerIDsInTier(t, ctx, ti, search, upstreamHost, nil)
}

func listProjectIssuerIDsInTier(t *testing.T, ctx context.Context, ti *testInstance, search, upstreamHost, tier *string) map[string]bool {
	t.Helper()
	limit := 100
	result, err := ti.service.ListRemoteSessionIssuers(ctx, &gen.ListRemoteSessionIssuersPayload{
		Cursor:           nil,
		Limit:            &limit,
		Search:           search,
		UpstreamHost:     upstreamHost,
		Tier:             tier,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	ids := make(map[string]bool, len(result.Items))
	for _, item := range result.Items {
		ids[item.ID] = true
	}
	return ids
}

func testProjectID(t *testing.T, ctx context.Context) uuid.NullUUID {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	return uuid.NullUUID{UUID: *authCtx.ProjectID, Valid: true}
}

func TestListRemoteSessionIssuers_SearchMatchesNameSlugAndIssuer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	project := testProjectID(t, ctx)
	noOrg := pgtype.Text{String: "", Valid: false}

	bySlug := seedRemoteIssuerWithURL(t, ctx, ti.conn, project, noOrg, "zephyr-idp", "https://one.example.test")
	byIssuer := seedRemoteIssuerWithURL(t, ctx, ti.conn, project, noOrg, "second-idp", "https://login.zephyr.test")
	platform := seedRemoteIssuerWithURL(t, ctx, ti.conn, uuid.NullUUID{}, noOrg, "platform-zephyr", "https://platform.example.test")
	other := seedRemoteIssuerWithURL(t, ctx, ti.conn, project, noOrg, "unrelated-idp", "https://two.example.test")
	named := newIssuerPayloadForURL("named-idp", "https://three.example.test")
	named.Name = new("The Zephyr Directory")
	byName, err := ti.service.CreateRemoteSessionIssuer(ctx, named)
	require.NoError(t, err)

	ids := listProjectIssuerIDs(t, ctx, ti, new("ZEPHYR"), nil)
	require.True(t, ids[bySlug.String()], "slug match")
	require.True(t, ids[byIssuer.String()], "issuer URL match")
	require.True(t, ids[byName.ID], "name match")
	require.True(t, ids[platform.String()], "search spans inherited tiers")
	require.False(t, ids[other.String()], "non-matching issuer is filtered out")
}

func TestListRemoteSessionIssuers_SearchTreatsWildcardsLiterally(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	project := testProjectID(t, ctx)
	noOrg := pgtype.Text{String: "", Valid: false}

	underscore := seedRemoteIssuerWithURL(t, ctx, ti.conn, project, noOrg, "wild_card", "https://wild.example.test")
	plain := seedRemoteIssuerWithURL(t, ctx, ti.conn, project, noOrg, "wildxcard", "https://wildx.example.test")

	ids := listProjectIssuerIDs(t, ctx, ti, new("wild_card"), nil)
	require.True(t, ids[underscore.String()])
	require.False(t, ids[plain.String()], "_ must not act as a single-character wildcard")

	require.Empty(t, listProjectIssuerIDs(t, ctx, ti, new("%"), nil), "% must not match everything")
}

func TestListRemoteSessionIssuers_UpstreamHostMatchesHostAndParentDomains(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	project := testProjectID(t, ctx)
	noOrg := pgtype.Text{String: "", Valid: false}

	parent := seedRemoteIssuerWithURL(t, ctx, ti.conn, project, noOrg, "parent", "https://Linear.example.test")
	exact := seedRemoteIssuerWithURL(t, ctx, ti.conn, uuid.NullUUID{}, noOrg, "exact", "https://mcp.linear.example.test:443/oauth")
	sibling := seedRemoteIssuerWithURL(t, ctx, ti.conn, project, noOrg, "sibling", "https://login.linear.example.test")
	lookalike := seedRemoteIssuerWithURL(t, ctx, ti.conn, project, noOrg, "lookalike", "https://notlinear.example.test")
	otherPort := seedRemoteIssuerWithURL(t, ctx, ti.conn, project, noOrg, "other-port", "https://linear.example.test:8443")

	ids := listProjectIssuerIDs(t, ctx, ti, nil, new("mcp.linear.example.test"))
	require.True(t, ids[parent.String()], "parent domain matches, ignoring case")
	require.True(t, ids[exact.String()], "same host matches, with default port and path ignored, across tiers")
	require.False(t, ids[sibling.String()], "a sibling subdomain is not a parent")
	require.False(t, ids[lookalike.String()], "a suffix that is not a whole label does not match")
	require.False(t, ids[otherPort.String()], "a non-default port is a different host")
}

func TestListRemoteSessionIssuers_RejectsUpstreamHostWithScheme(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	_, err := ti.service.ListRemoteSessionIssuers(ctx, &gen.ListRemoteSessionIssuersPayload{
		Cursor:           nil,
		Limit:            nil,
		Search:           nil,
		UpstreamHost:     new("https://mcp.linear.app"),
		Tier:             nil,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestListRemoteSessionIssuers_TierListsOneTierOnly(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	project := testProjectID(t, ctx)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	noOrg := pgtype.Text{String: "", Valid: false}

	own := seedRemoteIssuerWithURL(t, ctx, ti.conn, project, noOrg, "tier-project", "https://project.example.test")
	org := seedRemoteIssuerWithURL(t, ctx, ti.conn, uuid.NullUUID{}, pgtype.Text{String: authCtx.ActiveOrganizationID, Valid: true}, "tier-org", "https://org.example.test")
	platform := seedRemoteIssuerWithURL(t, ctx, ti.conn, uuid.NullUUID{}, noOrg, "tier-platform", "https://platform.example.test")

	for tier, want := range map[string]uuid.UUID{"project": own, "organization": org, "platform": platform} {
		ids := listProjectIssuerIDsInTier(t, ctx, ti, nil, nil, new(tier))
		require.True(t, ids[want.String()], "%s tier lists its own issuer", tier)
		for _, other := range []uuid.UUID{own, org, platform} {
			if other != want {
				require.False(t, ids[other.String()], "%s tier excludes other tiers", tier)
			}
		}
	}

	all := listProjectIssuerIDs(t, ctx, ti, nil, nil)
	require.True(t, all[own.String()] && all[org.String()] && all[platform.String()], "no tier lists all three")
}
