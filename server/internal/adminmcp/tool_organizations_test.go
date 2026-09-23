package adminmcp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
)

type recordingOrganizationReader struct {
	listInput *gen.ListOrganizationsPayload
	getInput  *gen.GetOrganizationPayload
	list      *gen.AdminListOrganizationsResult
	org       *gen.AdminOrganization
	err       error
}

func (r *recordingOrganizationReader) ListOrganizations(_ context.Context, input *gen.ListOrganizationsPayload) (*gen.AdminListOrganizationsResult, error) {
	r.listInput = input
	return r.list, r.err
}

func (r *recordingOrganizationReader) GetOrganization(_ context.Context, input *gen.GetOrganizationPayload) (*gen.AdminOrganization, error) {
	r.getInput = input
	return r.org, r.err
}

func callStaffReadTool(t *testing.T, reads OrganizationReader, name, args string) (int, string, json.RawMessage) {
	t.Helper()
	auth := &testAuthenticator{principal: staffPrincipal()}
	handler := NewRuntime(auth, "", reads).Handler()
	request := httptest.NewRequest(http.MethodPost, Path, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"`+name+`","arguments":`+args+`}}`))
	request.Header.Set("Authorization", "Bearer test-token")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json, text/event-stream")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	require.Equal(t, 1, auth.calls)
	var message struct {
		Result struct {
			StructuredContent json.RawMessage `json:"structuredContent"`
			IsError           bool            `json:"isError"`
		} `json:"result"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &message))
	return response.Code, response.Body.String(), message.Result.StructuredContent
}

func TestFindOrganizationsBoundsAndSafeProjection(t *testing.T) {
	t.Parallel()
	secret := "private-provider-id"
	state := "ending_soon"
	cursor := "org-b"
	reads := &recordingOrganizationReader{list: &gen.AdminListOrganizationsResult{
		Organizations: []*gen.AdminOrganization{
			{ID: "org-a", Name: "Example One", Slug: "example-one", AccountType: "payg", TrialState: &state, WorkosID: &secret, StripeCustomerID: &secret},
			{ID: "org-b", Name: "Example Two", Slug: "example-two", AccountType: "free"},
		},
		NextCursor: &cursor,
		Total:      3,
	}}
	status, body, data := callStaffReadTool(t, reads, "find_organizations", `{"query":"  example  ","limit":5,"cursor":"org-previous"}`)
	require.Equal(t, http.StatusOK, status)
	require.Equal(t, "example", *reads.listInput.Q)
	require.Equal(t, 5, *reads.listInput.Limit)
	require.Equal(t, "org-previous", *reads.listInput.Cursor)
	require.NotContains(t, body, secret)
	var output FindOrganizationsOutput
	require.NoError(t, json.Unmarshal(data, &output))
	require.Equal(t, []OrganizationMatch{
		{ID: "org-a", Name: "Example One", Slug: "example-one", AccountType: "payg", TrialState: state},
		{ID: "org-b", Name: "Example Two", Slug: "example-two", AccountType: "free", TrialState: "none"},
	}, output.Organizations)
	require.Equal(t, &cursor, output.NextCursor)
	require.EqualValues(t, 3, output.Total)

	defaultReads := &recordingOrganizationReader{list: &gen.AdminListOrganizationsResult{}}
	status, _, _ = callStaffReadTool(t, defaultReads, "find_organizations", `{"query":"example"}`)
	require.Equal(t, http.StatusOK, status)
	require.Equal(t, 10, *defaultReads.listInput.Limit)

	for _, args := range []string{`{"query":"ab"}`, `{"query":"example","limit":21}`, `{"query":"example","limit":-1}`} {
		other := &recordingOrganizationReader{}
		_, body, _ := callStaffReadTool(t, other, "find_organizations", args)
		require.Contains(t, body, `"isError":true`)
		require.Nil(t, other.listInput)
	}
}

func TestOrganizationSummaryAndTrialRequireExactID(t *testing.T) {
	t.Parallel()
	secret := "private-provider-id"
	state := "running"
	tier := "enterprise"
	endsAt := "2026-01-01T00:00:00Z"
	org := &gen.AdminOrganization{
		ID: "org-a", Name: "Example One", Slug: "example-one", AccountType: "payg", MemberCount: 3,
		TrialState: &state, TrialTier: &tier, TrialEndsAt: &endsAt,
		WorkosID: &secret, StripeCustomerID: &secret,
	}
	for _, tc := range []struct {
		name string
		key  string
	}{
		{name: "get_organization_summary", key: "member_count"},
		{name: "get_organization_trial", key: "ends_at"},
	} {
		reads := &recordingOrganizationReader{org: org}
		status, body, data := callStaffReadTool(t, reads, tc.name, `{"organization_id":"org-a"}`)
		require.Equal(t, http.StatusOK, status)
		require.Equal(t, "org-a", reads.getInput.IDOrSlug)
		require.Contains(t, string(data), tc.key)
		require.NotContains(t, body, secret)
	}

	reads := &recordingOrganizationReader{org: org}
	_, body, _ := callStaffReadTool(t, reads, "get_organization_summary", `{"organization_id":"example-one"}`)
	require.Contains(t, body, `"isError":true`)
	require.NotContains(t, body, "Example One")

	reads = &recordingOrganizationReader{err: errors.New("private database failure")}
	_, body, _ = callStaffReadTool(t, reads, "get_organization_trial", `{"organization_id":"org-b"}`)
	require.NotContains(t, body, "private database failure")
}

func TestConfiguredOrganizationReadsAppearInContext(t *testing.T) {
	t.Parallel()
	auth := &testAuthenticator{principal: staffPrincipal()}
	handler := NewRuntime(auth, "", &recordingOrganizationReader{}).Handler()
	request := httptest.NewRequest(http.MethodPost, Path, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"get_admin_context","arguments":{}}}`))
	request.Header.Set("Authorization", "Bearer test-token")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json, text/event-stream")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	require.Equal(t, http.StatusOK, response.Code)
	var message struct {
		Result struct {
			StructuredContent AdminContext `json:"structuredContent"`
		} `json:"result"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &message))
	require.Equal(t, []string{"inspect staff admin context", "find organizations", "inspect organization account and trial"}, message.Result.StructuredContent.Workflows)
}

func TestOrganizationReadsUnavailableWithoutService(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, args string }{
		{"find_organizations", `{"query":"example"}`},
		{"get_organization_summary", `{"organization_id":"org-a"}`},
		{"get_organization_trial", `{"organization_id":"org-a"}`},
	} {
		status, body, _ := callStaffReadTool(t, nil, tc.name, tc.args)
		require.Equal(t, http.StatusOK, status)
		require.Contains(t, body, `"isError":true`)
	}
}
