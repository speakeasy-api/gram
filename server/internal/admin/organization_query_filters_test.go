package admin

import (
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/speakeasy-api/gram/server/internal/admin/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/stretchr/testify/require"
)

// Exercise the internal contract directly; handlers do not expose these yet.
func TestOrganizationQueryFilters(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	conn, err := infra.CloneTestDatabase(t, "adminqueryfilters")
	require.NoError(t, err)
	queries := repo.New(conn)
	start := time.Date(2025, 3, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	fixtures := []struct {
		id       string
		members  int
		created  time.Time
		disabled bool
		account  string
	}{
		{"org_filter_a", 0, start.Add(-time.Microsecond), false, "free"},
		{"org_filter_b", 1, start, false, "payg"},
		{"org_filter_c", 2, start.Add(time.Microsecond), false, "payg"},
		{"org_filter_d", 0, end.Add(-time.Microsecond), true, "free"},
		{"org_filter_e", 2, end, true, "payg"},
		{"org_filter_f", 3, end.Add(time.Microsecond), false, "free"},
	}
	for _, f := range fixtures {
		var disabledAt *time.Time
		if f.disabled {
			disabledAt = &start
		}
		seedOrg(t, ctx, conn, orgFixture{id: f.id, name: f.id, slug: f.id, accountType: f.account, createdAt: &f.created, disabledAt: disabledAt, workosID: new("workos_" + f.id)})
		for i := 0; i < f.members; i++ {
			seedMembership(t, ctx, conn, f.id, fmt.Sprintf("user_%d", i))
		}
		// Deleted relationships must neither display nor satisfy a member bound.
		_, err := conn.Exec(ctx, `INSERT INTO organization_user_relationships (organization_id, user_id, deleted_at) VALUES ($1, 'deleted_user', now())`, f.id)
		require.NoError(t, err)
	}
	seedTrial(t, ctx, conn, trialFixture{orgID: "org_filter_c", endsAt: time.Now().Add(30 * 24 * time.Hour)})
	n := func(v int64) pgtype.Int8 { return pgtype.Int8{Int64: v, Valid: true} }
	b := func(v bool) pgtype.Bool { return pgtype.Bool{Bool: v, Valid: true} }
	stamp := conv.ToPGTimestamptz
	type testCase struct {
		name   string
		params repo.AdminListOrganizationsParams
		want   []string
	}
	cases := []testCase{
		{name: "omitted", want: []string{"a", "b", "c", "d", "e", "f"}},
		{name: "min zero", params: repo.AdminListOrganizationsParams{MinMembers: n(0)}, want: []string{"a", "b", "c", "d", "e", "f"}},
		{name: "max zero", params: repo.AdminListOrganizationsParams{MaxMembers: n(0)}, want: []string{"a", "d"}},
		{name: "equal zero", params: repo.AdminListOrganizationsParams{MinMembers: n(0), MaxMembers: n(0)}, want: []string{"a", "d"}},
		{name: "min inclusive", params: repo.AdminListOrganizationsParams{MinMembers: n(2)}, want: []string{"c", "e", "f"}},
		{name: "max inclusive", params: repo.AdminListOrganizationsParams{MaxMembers: n(2)}, want: []string{"a", "b", "c", "d", "e"}},
		{name: "equal positive", params: repo.AdminListOrganizationsParams{MinMembers: n(2), MaxMembers: n(2)}, want: []string{"c", "e"}},
		{name: "empty", params: repo.AdminListOrganizationsParams{MinMembers: n(4)}},
		{name: "inverted members", params: repo.AdminListOrganizationsParams{MinMembers: n(3), MaxMembers: n(1)}},
		{name: "lower inclusive", params: repo.AdminListOrganizationsParams{CreatedAtGte: stamp(start)}, want: []string{"b", "c", "d", "e", "f"}},
		{name: "upper exclusive", params: repo.AdminListOrganizationsParams{CreatedAtLt: stamp(end)}, want: []string{"a", "b", "c", "d"}},
		{name: "day window", params: repo.AdminListOrganizationsParams{CreatedAtGte: stamp(start), CreatedAtLt: stamp(end)}, want: []string{"b", "c", "d"}},
		{name: "equal timestamps", params: repo.AdminListOrganizationsParams{CreatedAtGte: stamp(start), CreatedAtLt: stamp(start)}},
		{name: "timezone equivalent", params: repo.AdminListOrganizationsParams{CreatedAtGte: stamp(start.In(time.FixedZone("west", -7*3600))), CreatedAtLt: stamp(end.In(time.FixedZone("east", 5*3600)))}, want: []string{"b", "c", "d"}},
		{name: "disabled false", params: repo.AdminListOrganizationsParams{DisabledOnly: b(false)}, want: []string{"a", "b", "c", "d", "e", "f"}},
		{name: "disabled true", params: repo.AdminListOrganizationsParams{DisabledOnly: b(true)}, want: []string{"d", "e"}},
		{name: "legacy active", params: repo.AdminListOrganizationsParams{DisabledStates: []string{"active"}}, want: []string{"a", "b", "c", "f"}},
		{name: "legacy id bypass", params: repo.AdminListOrganizationsParams{Q: conv.ToPGText("org_filter_d"), DisabledStates: []string{"active"}}, want: []string{"d"}},
		{name: "false id bypass", params: repo.AdminListOrganizationsParams{Q: conv.ToPGText("org_filter_d"), DisabledStates: []string{"active"}, DisabledOnly: b(false)}, want: []string{"d"}},
		{name: "true disabled id bypass", params: repo.AdminListOrganizationsParams{Q: conv.ToPGText("org_filter_d"), DisabledStates: []string{"active"}, DisabledOnly: b(true)}, want: []string{"d"}},
		{name: "strict active id", params: repo.AdminListOrganizationsParams{Q: conv.ToPGText("org_filter_b"), DisabledStates: []string{"disabled"}, DisabledOnly: b(true)}},
		{name: "strict active workos id", params: repo.AdminListOrganizationsParams{Q: conv.ToPGText("workos_org_filter_b"), DisabledOnly: b(true)}},
		{name: "combined", params: repo.AdminListOrganizationsParams{Q: conv.ToPGText("filter"), AccountTypes: []string{"payg"}, TrialStates: []string{"running"}, MinMembers: n(2), MaxMembers: n(2), CreatedAtGte: stamp(start), CreatedAtLt: stamp(end)}, want: []string{"c"}},
		{name: "combined disabled", params: repo.AdminListOrganizationsParams{Q: conv.ToPGText("filter"), AccountTypes: []string{"free"}, TrialStates: []string{"none"}, MaxMembers: n(0), CreatedAtGte: stamp(start), CreatedAtLt: stamp(end), DisabledOnly: b(true)}, want: []string{"d"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := tc.params
			if p.DisabledStates == nil {
				p.DisabledStates = []string{"active", "disabled"}
			}
			p.PageLimit = 100
			checkCount := func() {
				count, err := queries.AdminCountOrganizations(ctx, repo.AdminCountOrganizationsParams{Q: p.Q, AccountTypes: p.AccountTypes, TrialStates: p.TrialStates, DisabledStates: p.DisabledStates, MinMembers: p.MinMembers, MaxMembers: p.MaxMembers, CreatedAtGte: p.CreatedAtGte, CreatedAtLt: p.CreatedAtLt, DisabledOnly: p.DisabledOnly})
				require.NoError(t, err)
				require.Equal(t, int64(len(tc.want)), count)
			}
			want := make([]string, 0, len(tc.want))
			for _, suffix := range tc.want {
				want = append(want, "org_filter_"+suffix)
			}
			rows, err := queries.AdminListOrganizations(ctx, p)
			require.NoError(t, err)
			ids := make([]string, 0, len(rows))
			for _, row := range rows {
				ids = append(ids, row.ID)
				for _, f := range fixtures {
					if row.ID == f.id {
						require.Equal(t, int64(f.members), row.MemberCount)
					}
				}
			}
			require.Equal(t, want, ids)
			checkCount()
			// Walk every filtered set through both paging modes and past the end.
			p.PageLimit = 1
			p.SortBy = "name"
			p.SortDir = "asc"
			for i := 0; i <= len(want); i++ {
				p.PageOffset = int64(i)
				rows, err := queries.AdminListOrganizations(ctx, p)
				require.NoError(t, err)
				if i == len(want) {
					require.Empty(t, rows)
				} else {
					require.Len(t, rows, 1)
					require.Equal(t, want[i], rows[0].ID)
				}
				checkCount()
			}
			p.PageOffset = 0
			p.SortBy = "created_at"
			p.SortDir = "desc"
			p.PageLimit = 100
			ordered, err := queries.AdminListOrganizations(ctx, p)
			require.NoError(t, err)
			p.PageLimit = 1
			for _, expected := range ordered {
				rows, err := queries.AdminListOrganizations(ctx, p)
				require.NoError(t, err)
				require.Len(t, rows, 1)
				require.Equal(t, expected.ID, rows[0].ID)
				p.AfterID = conv.ToPGText(rows[0].ID)
				checkCount()
			}
			rows, err = queries.AdminListOrganizations(ctx, p)
			require.NoError(t, err)
			require.Empty(t, rows)
			checkCount()
		})
	}
}

func TestOrganizationQueryFilters_LegacyCursor(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	conn, err := infra.CloneTestDatabase(t, "adminquerycursor")
	require.NoError(t, err)
	queries := repo.New(conn)
	// Equal creation times exercise the existing ID tiebreaker without changing
	// the legacy no-sort contract. Excluded rows straddle both page boundaries.
	for _, id := range []string{"org_a", "org_b", "org_c", "org_d", "org_e"} {
		seedOrg(t, ctx, conn, orgFixture{id: id, name: id, slug: id})
		if id == "org_b" || id == "org_d" {
			seedMembership(t, ctx, conn, id, "user_example")
		}
	}
	min := pgtype.Int8{Int64: 1, Valid: true}
	p := repo.AdminListOrganizationsParams{DisabledStates: []string{"active"}, MinMembers: min, PageLimit: 1}
	for _, id := range []string{"org_b", "org_d", ""} {
		rows, err := queries.AdminListOrganizations(ctx, p)
		require.NoError(t, err)
		if id == "" {
			require.Empty(t, rows)
		} else {
			require.Len(t, rows, 1)
			require.Equal(t, id, rows[0].ID)
			p.AfterID = conv.ToPGText(id)
		}
		count, err := queries.AdminCountOrganizations(ctx, repo.AdminCountOrganizationsParams{DisabledStates: p.DisabledStates, MinMembers: min})
		require.NoError(t, err)
		require.Equal(t, int64(2), count)
	}
	// An anchor need not satisfy the filter, but an unknown anchor still exhausts.
	p.AfterID = conv.ToPGText("org_c")
	rows, err := queries.AdminListOrganizations(ctx, p)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "org_d", rows[0].ID)
	p.AfterID = conv.ToPGText("org_missing")
	rows, err = queries.AdminListOrganizations(ctx, p)
	require.NoError(t, err)
	require.Empty(t, rows)
}
