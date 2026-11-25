package adminmcp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/admin"
)

type recordingOrganizationDetail struct {
	recordingOrganizationReader
	stats       *gen.AdminOrganizationStats
	page        *admin.MemberPage
	memberOrg   string
	memberAfter string
	memberLimit int
	calls       int
	readErr     error
}

func (r *recordingOrganizationDetail) GetOrganizationStats(context.Context, *gen.GetOrganizationStatsPayload) (*gen.AdminOrganizationStats, error) {
	r.calls++
	return r.stats, r.readErr
}

func (r *recordingOrganizationDetail) ListOrganizationMembersPage(_ context.Context, org, after string, limit int) (*admin.MemberPage, error) {
	r.calls++
	r.memberOrg, r.memberAfter, r.memberLimit = org, after, limit
	return r.page, r.readErr
}

func TestOrganizationStatisticsAreGlobalAndRequireStaff(t *testing.T) {
	t.Parallel()
	reads := &recordingOrganizationDetail{stats: &gen.AdminOrganizationStats{Total: 17, CreatedLast7Days: 2, Customers: 8, CustomersCreatedLast7Days: 1, TrialsEndingSoon: 3, Disabled: 4, DisabledLast7Days: 1}}
	body, data := issuerToolCall(t, reads, "get_organization_statistics", `{}`, true)
	require.NotContains(t, body, `"isError":true`)
	require.Nil(t, reads.getInput)
	require.Nil(t, reads.listInput)
	requireJSONKeys(t, data, "total", "created_last_7_days", "customers", "customers_created_last_7_days", "trials_ending_soon", "disabled", "disabled_last_7_days")
	var out OrganizationStatistics
	require.NoError(t, json.Unmarshal(data, &out))
	require.EqualValues(t, 17, out.Total)
	reads.calls = 0
	body, _ = issuerToolCall(t, reads, "get_organization_statistics", `{}`, false)
	require.Contains(t, body, `"isError":true`)
	require.Zero(t, reads.calls)
}

func TestOrganizationMemberLookupScopesItsCursor(t *testing.T) {
	t.Parallel()
	reads := &recordingOrganizationDetail{
		recordingOrganizationReader: recordingOrganizationReader{org: &gen.AdminOrganization{ID: "org-a"}},
		page:                        &admin.MemberPage{OrganizationID: "org-a", Members: []*gen.AdminOrganizationMember{{ID: "user-a", Email: "member@example.test", DisplayName: "Example Member", CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-02T00:00:00Z"}}, NextUserID: new("user-a")},
	}
	body, data := issuerToolCall(t, reads, "list_organization_members", `{"organization_id":"org-a","limit":1}`, true)
	require.NotContains(t, body, `"isError":true`)
	var out OrganizationMembersOutput
	require.NoError(t, json.Unmarshal(data, &out))
	require.Equal(t, "org-a", reads.memberOrg)
	require.Equal(t, 1, reads.memberLimit)
	require.Empty(t, reads.memberAfter)
	require.Equal(t, "member@example.test", out.Members[0].Email)
	require.NotNil(t, out.NextCursor)
	decoded, err := base64.RawURLEncoding.DecodeString(*out.NextCursor)
	require.NoError(t, err)
	require.NotContains(t, string(decoded), "member@example.test")
	args, err := json.Marshal(OrganizationMembersInput{OrganizationID: "org-a", Cursor: *out.NextCursor})
	require.NoError(t, err)
	body, _ = issuerToolCall(t, reads, "list_organization_members", string(args), true)
	require.NotContains(t, body, `"isError":true`)
	require.Equal(t, "user-a", reads.memberAfter)
	require.Equal(t, defaultMemberLookupLimit, reads.memberLimit)

	reads.org = &gen.AdminOrganization{ID: "org-b"}
	reads.calls = 0
	args, err = json.Marshal(OrganizationMembersInput{OrganizationID: "org-b", Cursor: *out.NextCursor})
	require.NoError(t, err)
	body, _ = issuerToolCall(t, reads, "list_organization_members", string(args), true)
	require.Contains(t, body, `"isError":true`)
	require.Zero(t, reads.calls)
	require.NotContains(t, body, "member@example.test")
}

func TestOrganizationMemberLookupRejectsWrongTargetAndPrivateErrors(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		args string
		page *admin.MemberPage
		err  error
	}{
		{"returned target", `{"organization_id":"org-a"}`, &admin.MemberPage{OrganizationID: "org-b"}, nil},
		{"private failure", `{"organization_id":"org-a"}`, nil, errors.New("private database failure")},
		{"cursor", `{"organization_id":"org-a","cursor":"bad-cursor"}`, nil, nil},
		{"limit", `{"organization_id":"org-a","limit":51}`, nil, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			reads := &recordingOrganizationDetail{recordingOrganizationReader: recordingOrganizationReader{org: &gen.AdminOrganization{ID: "org-a"}}, page: tc.page, readErr: tc.err}
			body, _ := issuerToolCall(t, reads, "list_organization_members", tc.args, true)
			require.Contains(t, body, `"isError":true`)
			require.NotContains(t, body, "private database failure")
		})
	}
}

func TestOrganizationDetailsAppearInContext(t *testing.T) {
	t.Parallel()
	reads := &recordingOrganizationDetail{}
	body, data := issuerToolCall(t, reads, "get_admin_context", `{}`, true)
	require.NotContains(t, body, `"isError":true`)
	var out AdminContext
	require.NoError(t, json.Unmarshal(data, &out))
	require.Contains(t, out.Workflows, "inspect unfiltered global organization statistics")
	require.Contains(t, out.Workflows, "inspect bounded organization member contact and login information")
}
