package adminmcp

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
)

func TestOrganizationSearchForwardsDashboardFilters(t *testing.T) {
	t.Parallel()
	reads := &recordingOrganizationReader{list: &gen.AdminListOrganizationsResult{Total: 21}}
	_, body, data := callStaffReadTool(t, reads, "find_organizations", `{"account_types":["payg"],"trial_states":["ending_soon"],"disabled_status":"active","min_members":"9007199254740993","max_members":"9223372036854775807","created_from":"2026-01-01","created_to":"2026-02-28","sort":"member_count","direction":"desc","page":2,"limit":5}`)
	require.NotContains(t, body, `"isError":true`)
	require.Nil(t, reads.listInput.Q)
	require.Equal(t, []string{"payg"}, reads.listInput.AccountTypes)
	require.Equal(t, []string{"ending_soon"}, reads.listInput.TrialStates)
	require.Equal(t, "active", *reads.listInput.DisabledStatus)
	require.EqualValues(t, 9007199254740993, *reads.listInput.MinMembers)
	require.EqualValues(t, 9223372036854775807, *reads.listInput.MaxMembers)
	require.Equal(t, "2026-01-01", *reads.listInput.CreatedFrom)
	require.Equal(t, "2026-02-28", *reads.listInput.CreatedTo)
	require.Equal(t, "member_count", *reads.listInput.Sort)
	require.Equal(t, "desc", *reads.listInput.Direction)
	require.Equal(t, 2, *reads.listInput.Page)
	require.Nil(t, reads.listInput.Cursor)
	var out FindOrganizationsOutput
	require.NoError(t, json.Unmarshal(data, &out))
	require.Equal(t, 2, out.Page)
	require.Equal(t, 3, *out.NextPage)
	require.Nil(t, out.NextCursor)
}

func TestOrganizationSearchSortDefaultsToFirstPage(t *testing.T) {
	t.Parallel()
	payload, err := organizationSearchPayload(FindOrganizationsInput{Query: "example", Sort: "name"})
	require.NoError(t, err)
	require.Equal(t, 1, *payload.Page)
	require.Equal(t, defaultOrganizationSearchLimit, *payload.Limit)
	require.Nil(t, payload.Direction)
}

func TestOrganizationSearchForwardsUnicodeQueries(t *testing.T) {
	t.Parallel()
	for _, query := range []string{"你好界", strings.Repeat("界", 128)} {
		t.Run(query, func(t *testing.T) {
			t.Parallel()
			reads := &recordingOrganizationReader{list: &gen.AdminListOrganizationsResult{}}
			_, body, _ := callStaffReadTool(t, reads, "find_organizations", `{"query":"`+query+`"}`)
			require.NotContains(t, body, `"isError":true`)
			require.Equal(t, query, *reads.listInput.Q)
			require.Nil(t, reads.listInput.DisabledStatus)
		})
	}
}

func TestOrganizationSearchRejectsInvalidFilters(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		args string
	}{
		{"no search or filter", `{}`},
		{"short search", `{"query":"ab"}`},
		{"single multibyte character", `{"query":"界"}`},
		{"two multibyte characters", `{"query":"你好"}`},
		{"oversized multibyte search", `{"query":"` + strings.Repeat("界", 129) + `"}`},
		{"empty access state", `{"query":"example","disabled_status":""}`},
		{"empty access state with filter", `{"account_types":["payg"],"disabled_status":""}`},
		{"account type", `{"account_types":["unknown"]}`},
		{"trial state", `{"trial_states":["unknown"]}`},
		{"access state", `{"disabled_status":"unknown"}`},
		{"count precision", `{"min_members":"9223372036854775808"}`},
		{"negative count", `{"min_members":"-1"}`},
		{"count whitespace", `{"min_members":" 1"}`},
		{"count range", `{"min_members":"5","max_members":"4"}`},
		{"invalid date", `{"created_from":"2026-02-30"}`},
		{"date range", `{"created_from":"2026-02-01","created_to":"2026-01-01"}`},
		{"sort column", `{"query":"example","sort":"unknown"}`},
		{"direction without sort", `{"query":"example","direction":"desc"}`},
		{"direction", `{"query":"example","sort":"name","direction":"unknown"}`},
		{"cursor and sort", `{"query":"example","cursor":"org-a","sort":"name"}`},
		{"cursor and page", `{"query":"example","cursor":"org-a","page":2}`},
		{"deep offset", `{"query":"example","page":1001}`},
		{"negative page", `{"query":"example","page":-1}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			reads := &recordingOrganizationReader{}
			_, body, _ := callStaffReadTool(t, reads, "find_organizations", tc.args)
			require.Contains(t, body, `"isError":true`)
			require.Nil(t, reads.listInput)
		})
	}
}
