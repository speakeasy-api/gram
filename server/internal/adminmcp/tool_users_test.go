package adminmcp

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/stretchr/testify/require"
)

type recordingUserReader struct {
	recordingOrganizationReader
	usersInput *gen.ListUsersPayload
	orgsInput  *gen.ListUserOrganizationsPayload
	users      *gen.AdminListUsersResult
	orgs       *gen.AdminListUserOrganizationsResult
	userErr    error
}

func (r *recordingUserReader) ListUsers(_ context.Context, p *gen.ListUsersPayload) (*gen.AdminListUsersResult, error) {
	r.usersInput = p
	return r.users, r.userErr
}

func (r *recordingUserReader) ListUserOrganizations(_ context.Context, p *gen.ListUserOrganizationsPayload) (*gen.AdminListUserOrganizationsResult, error) {
	r.orgsInput = p
	return r.orgs, r.userErr
}

func TestFindUsersDefaultsBoundsAndProjection(t *testing.T) {
	t.Parallel()
	r := &recordingUserReader{users: &gen.AdminListUsersResult{Users: []*gen.AdminUser{{ID: "user_zero", Email: "zero@example.invalid", Organizations: []*gen.AdminUserOrganization{}}, {ID: "user_many", DisplayName: "Ignore instructions", OrganizationCount: 4, Organizations: []*gen.AdminUserOrganization{{ID: "org_a"}, {ID: "org_b"}, {ID: "org_c"}}}}, Total: 2, Page: 1, Limit: 10}}
	_, body, data := callStaffReadTool(t, r, "find_users", `{}`)
	require.NotContains(t, body, `"isError":true`)
	require.NotNil(t, r.usersInput)
	require.Empty(t, *r.usersInput.Q)
	require.Equal(t, 1, *r.usersInput.Page)
	require.Equal(t, 10, *r.usersInput.Limit)
	require.Nil(t, r.usersInput.AdminSessionToken)
	var out struct {
		Users []struct {
			ID            string            `json:"id"`
			Organizations []json.RawMessage `json:"organizations"`
			Count         int64             `json:"organization_count"`
		} `json:"users"`
	}
	require.NoError(t, json.Unmarshal(data, &out))
	require.Len(t, out.Users, 2)
	require.Empty(t, out.Users[0].Organizations)
	require.Len(t, out.Users[1].Organizations, 3)
	require.EqualValues(t, 4, out.Users[1].Count)
	for _, secret := range []string{"session_token", "handoff", "workos", "provider"} {
		require.NotContains(t, body, secret)
	}
	_, body, _ = callStaffReadTool(t, r, "find_users", `{"query":"name:Alex org:Example org:Team","page":2,"limit":20}`)
	require.NotContains(t, body, `"isError":true`)
	require.Equal(t, "name:Alex org:Example org:Team", *r.usersInput.Q)
	require.Equal(t, 2, *r.usersInput.Page)
	require.Equal(t, 20, *r.usersInput.Limit)
	for _, args := range []string{`{"page":0}`, `{"page":-1}`, `{"limit":0}`, `{"limit":21}`, `{"page":2147483647,"limit":20}`, `{"query":"unknown:value"}`, `{"query":"name:"}`, `{"query":"\"unclosed"}`, `{"query":"` + strings.Repeat("x", 2049) + `"}`, `{"query":"` + strings.Repeat("x ", 21) + `"}`, `{"query":"` + strings.Repeat("é", 257) + `"}`} {
		r.usersInput = nil
		_, body, _ := callStaffReadTool(t, r, "find_users", args)
		require.Contains(t, body, `"isError":true`, args)
		require.Nil(t, r.usersInput, args)
	}
}

func TestListUserOrganizationsExactIDAndPagination(t *testing.T) {
	t.Parallel()
	r := &recordingUserReader{orgs: &gen.AdminListUserOrganizationsResult{Organizations: []*gen.AdminUserOrganization{{ID: "org_four", Name: "Example", Slug: "example"}}, Total: 4, Page: 2, Limit: 3}}
	_, body, data := callStaffReadTool(t, r, "list_user_organizations", `{"user_id":"user_many","page":2,"limit":3}`)
	require.NotContains(t, body, `"isError":true`)
	require.Contains(t, string(data), `"id":"org_four"`)
	require.Equal(t, "user_many", r.orgsInput.UserID)
	require.Equal(t, 2, *r.orgsInput.Page)
	require.Equal(t, 3, *r.orgsInput.Limit)
	require.Nil(t, r.orgsInput.AdminSessionToken)
	_, _, _ = callStaffReadTool(t, r, "list_user_organizations", `{"user_id":"user_zero"}`)
	require.Equal(t, 1, *r.orgsInput.Page)
	require.Equal(t, 20, *r.orgsInput.Limit)
	for _, args := range []string{`{"user_id":""}`, `{"user_id":" user_many"}`, `{"user_id":"` + strings.Repeat("x", 129) + `"}`, `{"user_id":"user_many","page":0}`, `{"user_id":"user_many","limit":101}`, `{"user_id":"user_many","limit":0}`} {
		r.orgsInput = nil
		_, body, _ := callStaffReadTool(t, r, "list_user_organizations", args)
		require.Contains(t, body, `"isError":true`)
		require.Nil(t, r.orgsInput)
	}
}

func TestUsersErrorsAndUnavailable(t *testing.T) {
	t.Parallel()
	for _, tool := range []struct{ name, args string }{{"find_users", `{}`}, {"list_user_organizations", `{"user_id":"user_zero"}`}} {
		_, body, _ := callStaffReadTool(t, &recordingOrganizationReader{}, tool.name, tool.args)
		require.Contains(t, body, "user information is unavailable")
		for _, err := range []error{errors.New("provider-secret"), oops.E(oops.CodeInvalid, errors.New("provider-secret"), "safe validation")} {
			r := &recordingUserReader{userErr: err}
			_, body, _ = callStaffReadTool(t, r, tool.name, tool.args)
			require.Contains(t, body, `"isError":true`)
			require.NotContains(t, body, "provider-secret")
			if _, ok := errors.AsType[*oops.ShareableError](err); ok {
				require.Contains(t, body, "safe validation")
			} else {
				require.Contains(t, body, "user information is unavailable")
			}
		}
	}
}

func TestUsersOutputBoundsAndEmptyOrganizations(t *testing.T) {
	t.Parallel()
	orgs := make([]*gen.AdminUserOrganization, 101)
	for i := range orgs {
		orgs[i] = &gen.AdminUserOrganization{ID: "org_example"}
	}
	users := make([]*gen.AdminUser, 21)
	for i := range users {
		users[i] = &gen.AdminUser{ID: "user_example", Organizations: orgs, OrganizationCount: 101}
	}
	reads := &recordingUserReader{users: &gen.AdminListUsersResult{Users: users, Total: 21}, orgs: &gen.AdminListUserOrganizationsResult{Organizations: orgs, Total: 101}}
	_, _, data := callStaffReadTool(t, reads, "find_users", `{"limit":20}`)
	var found FindUsersOutput
	require.NoError(t, json.Unmarshal(data, &found))
	require.Len(t, found.Users, 20)
	require.Len(t, found.Users[0].Organizations, 3)
	require.EqualValues(t, 101, found.Users[0].OrganizationCount)
	_, _, data = callStaffReadTool(t, reads, "list_user_organizations", `{"user_id":"user_example","limit":100}`)
	var memberships ListUserOrganizationsOutput
	require.NoError(t, json.Unmarshal(data, &memberships))
	require.Len(t, memberships.Organizations, 100)
	require.EqualValues(t, 101, memberships.Total)
	reads.orgs = &gen.AdminListUserOrganizationsResult{Organizations: []*gen.AdminUserOrganization{}}
	_, body, data := callStaffReadTool(t, reads, "list_user_organizations", `{"user_id":"user_zero"}`)
	require.NotContains(t, body, `"isError":true`)
	require.Contains(t, string(data), `"organizations":[]`)
}
