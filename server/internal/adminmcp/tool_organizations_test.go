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
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/admin"
)

// emptyReads answers every admin read with an empty result, so a fake only
// overrides the reads its test exercises.
type emptyReads struct{}

func (emptyReads) ListUsers(context.Context, *gen.ListUsersPayload) (*gen.AdminListUsersResult, error) {
	return nil, nil
}

func (emptyReads) ListUserOrganizations(context.Context, *gen.ListUserOrganizationsPayload) (*gen.AdminListUserOrganizationsResult, error) {
	return nil, nil
}

func (emptyReads) ListOrganizationProjects(context.Context, *gen.ListOrganizationProjectsPayload) (*gen.AdminListOrganizationProjectsResult, error) {
	return nil, nil
}

func (emptyReads) GetProject(context.Context, *gen.GetProjectPayload) (*gen.AdminProjectDetail, error) {
	return nil, nil
}

func (emptyReads) GetOrganizationFeaturesStrict(context.Context, string) (*gen.ProductFeatures, error) {
	return nil, nil
}

func (emptyReads) GetOrganizationChatAnalysisSettings(context.Context, *gen.GetOrganizationChatAnalysisSettingsPayload) (*gen.AdminChatAnalysisSettings, error) {
	return nil, nil
}

func (emptyReads) ListOrganizationActivity(context.Context, *gen.ListOrganizationActivityPayload) (*gen.AdminListOrganizationActivityResult, error) {
	return nil, nil
}

func (emptyReads) GetPaygBillingSummary(context.Context, *gen.GetPaygBillingSummaryPayload) (*gen.AdminPaygBillingSummary, error) {
	return nil, nil
}

func (emptyReads) GetSupportCoverage(context.Context, *gen.GetSupportCoveragePayload) (*gen.SupportCoverageResult, error) {
	return nil, nil
}

func (emptyReads) ListGlobalIssuers(context.Context, *gen.ListGlobalIssuersPayload) (*gen.ListGlobalRemoteSessionIssuersResult, error) {
	return nil, nil
}

func (emptyReads) GetGlobalIssuer(context.Context, *gen.GetGlobalIssuerPayload) (*gen.GlobalRemoteSessionIssuer, error) {
	return nil, nil
}

func (emptyReads) GetGlobalIssuerDuplicatePreflight(context.Context, *gen.GetGlobalIssuerDuplicatePreflightPayload) (*types.RemoteSessionIssuerDuplicatePreflight, error) {
	return nil, nil
}

func (emptyReads) GetGlobalIssuerMigratePreflight(context.Context, *gen.GetGlobalIssuerMigratePreflightPayload) (*gen.IssuerMigratePreflight, error) {
	return nil, nil
}

func (emptyReads) ListGlobalIssuerConvergenceCandidates(context.Context, *gen.ListGlobalIssuerConvergenceCandidatesPayload) (*gen.ListIssuerConvergenceCandidatesResult, error) {
	return nil, nil
}

func (emptyReads) GetSupportMatrix(context.Context, *gen.GetSupportMatrixPayload) (*gen.SupportMatrix, error) {
	return nil, nil
}

func (emptyReads) GetOrganizationOnboardingPlaybook(context.Context, *gen.GetOrganizationOnboardingPlaybookPayload) (*gen.AdminOrganizationOnboardingPlaybook, error) {
	return nil, nil
}

func (emptyReads) ListProjectMcpServers(context.Context, *gen.ListProjectMcpServersPayload) (*gen.AdminListProjectMcpServersResult, error) {
	return nil, nil
}

func (emptyReads) GetInferenceKeys(context.Context, *gen.GetInferenceKeysPayload) ([]*gen.AdminInferenceKey, error) {
	return nil, nil
}

func (emptyReads) GetInferenceSpendHistory(context.Context, *gen.GetInferenceSpendHistoryPayload) ([]*gen.AdminInferenceSpendMonth, error) {
	return nil, nil
}

func (emptyReads) GetMeterUsage(context.Context, *gen.GetMeterUsagePayload) (*gen.AdminMeterUsageResponse, error) {
	return nil, nil
}

func (emptyReads) GetSpendBreakdown(context.Context, *gen.GetSpendBreakdownPayload) (*gen.AdminSpendBreakdownResponse, error) {
	return nil, nil
}

func (emptyReads) GetOrganizationStats(context.Context, *gen.GetOrganizationStatsPayload) (*gen.AdminOrganizationStats, error) {
	return nil, nil
}

func (emptyReads) ListOrganizationMembersPage(context.Context, string, string, int) (*admin.MemberPage, error) {
	return nil, nil
}

func (emptyReads) GetStripeSubscription(context.Context, *gen.GetStripeSubscriptionPayload) (*gen.AdminStripeSubscription, error) {
	return nil, nil
}

func (emptyReads) ListRegistryEntries(context.Context, *gen.ListRegistryEntriesPayload) (*gen.AdminRegistryPage, error) {
	return nil, nil
}

func (emptyReads) GetRegistryEntry(context.Context, *gen.GetRegistryEntryPayload) (*gen.AdminRegistryEntry, error) {
	return nil, nil
}

func (emptyReads) DescribeMcpServerHealth(context.Context, *gen.DescribeMcpServerHealthPayload) (*gen.AdminMcpServerHealth, error) {
	return nil, nil
}

func (emptyReads) GetMcpServerToolCalls(context.Context, *gen.GetMcpServerToolCallsPayload) (*gen.AdminMcpServerToolCalls, error) {
	return nil, nil
}

type recordingOrganizationReader struct {
	emptyReads
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

func callStaffReadTool(t *testing.T, reads Reader, name, args string) (int, string, json.RawMessage) {
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
	require.Subset(t, message.Result.StructuredContent.Workflows, []string{"inspect staff admin context", "find organizations", "inspect organization account and trial"})
}
