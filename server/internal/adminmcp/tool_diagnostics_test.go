package adminmcp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
)

type recordingDiagnosticsReader struct {
	*recordingProjectReader
	onboardingInput *gen.GetOrganizationOnboardingPlaybookPayload
	onboarding      *gen.AdminOrganizationOnboardingPlaybook
	serversInput    *gen.ListProjectMcpServersPayload
	servers         *gen.AdminListProjectMcpServersResult
	err             error
}

func (r *recordingDiagnosticsReader) GetOrganizationOnboardingPlaybook(_ context.Context, input *gen.GetOrganizationOnboardingPlaybookPayload) (*gen.AdminOrganizationOnboardingPlaybook, error) {
	r.onboardingInput = input
	return r.onboarding, r.err
}

func (r *recordingDiagnosticsReader) ListProjectMcpServers(_ context.Context, input *gen.ListProjectMcpServersPayload) (*gen.AdminListProjectMcpServersResult, error) {
	r.serversInput = input
	return r.servers, r.err
}

func callDiagnosticReadTool(t *testing.T, reads *recordingDiagnosticsReader, name, args string) (int, string, json.RawMessage) {
	t.Helper()
	return callDiagnosticReadToolAs(t, reads, nil, name, args)
}

// callDiagnosticReadToolAs lets a test alter the authenticated principal before the call.
func callDiagnosticReadToolAs(t *testing.T, reads *recordingDiagnosticsReader, alter func(*Principal), name, args string) (int, string, json.RawMessage) {
	t.Helper()
	principal := staffPrincipal()
	principal.staff = &contextvalues.AdminAuthContext{SessionID: "browser-session", OIDCSubject: "staff-subject", Email: principal.Email}
	if alter != nil {
		alter(&principal)
	}
	auth := &testAuthenticator{principal: principal}
	handler := NewRuntime(auth, "", reads).Handler()
	request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"`+name+`","arguments":`+args+`}}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json, text/event-stream")
	request.Header.Set("Authorization", "Bearer test-token")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	var message struct {
		Result struct {
			StructuredContent json.RawMessage `json:"structuredContent"`
			IsError           bool            `json:"isError"`
		} `json:"result"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &message))
	return response.Code, response.Body.String(), message.Result.StructuredContent
}

func testDiagnosticsReads() *recordingDiagnosticsReader {
	projectReads := testProjectReads()
	projectReads.project = &gen.AdminProjectDetail{ID: testProjectID, OrganizationID: "org-a"}
	return &recordingDiagnosticsReader{
		recordingProjectReader: projectReads,
		onboarding:             &gen.AdminOrganizationOnboardingPlaybook{OrganizationID: "org-a"},
		servers:                &gen.AdminListProjectMcpServersResult{},
	}
}

func TestOrganizationOnboardingExactTargetAndRedactedProjection(t *testing.T) {
	t.Parallel()
	reads := testDiagnosticsReads()
	// The id and the slug differ so the projection is seen to pick the slug.
	useCaseID, useCase, useCaseName := "use-case-uuid-a", "distribution", "Distribution"
	reads.onboarding.Playbook = &gen.AdminOnboardingPlaybook{
		ID: "playbook-a", UseCaseID: &useCaseID, UseCaseSlug: &useCase, UseCaseName: &useCaseName,
		Name: "staff-authored playbook name", Description: "private playbook description", IsDefault: true,
		Steps: []*gen.AdminOnboardingPlaybookStep{{Slug: "platform-mcp", Title: "customer-facing step title"}},
	}
	reads.onboarding.Applicability = []*gen.AdminOnboardingStepApplicability{{Slug: "platform-mcp", Title: "customer-facing step title", Applies: false, Reason: "private stack reason"}}
	status, body, data := callDiagnosticReadTool(t, reads, "get_organization_onboarding", `{"organization_id":"org-a"}`)
	require.Equal(t, http.StatusOK, status)
	require.Equal(t, "org-a", reads.recordingOrganizationReader.getInput.IDOrSlug)
	require.Equal(t, "org-a", reads.onboardingInput.OrganizationID)
	require.Nil(t, reads.onboardingInput.AdminSessionToken)
	var output OrganizationOnboardingOutput
	require.NoError(t, json.Unmarshal(data, &output))
	require.Equal(t, OrganizationOnboardingOutput{
		OrganizationID: "org-a",
		Playbook:       &OnboardingPlaybook{ID: "playbook-a", UseCase: &useCase, Default: true, Custom: false},
		Steps:          []OnboardingStep{{Slug: "platform-mcp", Applies: false}},
	}, output)
	require.NotContains(t, body, "staff-authored playbook name")
	require.NotContains(t, body, "private playbook description")
	require.NotContains(t, body, "customer-facing step title")
	require.NotContains(t, body, "private stack reason")
	require.NotContains(t, body, useCaseName)
	require.NotContains(t, body, "admin_session_token")

	// An organization's own playbook is reported as custom, and one that
	// belongs to another organization is never reported at all.
	reads = testDiagnosticsReads()
	other := "org-b"
	reads.onboarding.Playbook = &gen.AdminOnboardingPlaybook{ID: "playbook-b", OrganizationID: &other}
	_, body, _ = callDiagnosticReadTool(t, reads, "get_organization_onboarding", `{"organization_id":"org-a"}`)
	require.Contains(t, body, `"isError":true`)
	own := "org-a"
	reads.onboarding.Playbook = &gen.AdminOnboardingPlaybook{ID: "playbook-b", OrganizationID: &own}
	_, _, data = callDiagnosticReadTool(t, reads, "get_organization_onboarding", `{"organization_id":"org-a"}`)
	var ownOutput OrganizationOnboardingOutput
	require.NoError(t, json.Unmarshal(data, &ownOutput))
	require.Equal(t, &OnboardingPlaybook{ID: "playbook-b", UseCase: nil, Default: false, Custom: true}, ownOutput.Playbook)
	require.Empty(t, ownOutput.Steps)
}

func TestListProjectMCPServersExactTargetAndRedactedProjection(t *testing.T) {
	t.Parallel()
	reads := testDiagnosticsReads()
	privateURL := "https://customer.example/private-route"
	reads.servers.McpServers = []*gen.AdminMcpServer{{
		ID: "server-a", Name: "Docs", URL: &privateURL, Visibility: "public", Source: "toolset_only", CreatedAt: "2026-01-01T00:00:00Z",
	}}
	args := `{"organization_id":"org-a","project_id":"` + testProjectID + `"}`
	status, body, data := callDiagnosticReadTool(t, reads, "list_project_mcp_servers", args)
	require.Equal(t, http.StatusOK, status)
	require.Equal(t, "org-a", reads.recordingOrganizationReader.getInput.IDOrSlug)
	require.Equal(t, testProjectID, reads.getInput.IDOrSlug)
	require.Equal(t, "org-a", *reads.getInput.OrganizationIDOrSlug)
	require.Equal(t, "org-a", reads.serversInput.OrganizationID)
	require.Equal(t, testProjectID, reads.serversInput.ProjectID)
	require.Nil(t, reads.serversInput.AdminSessionToken)
	var output ListProjectMCPServersOutput
	require.NoError(t, json.Unmarshal(data, &output))
	require.Equal(t, ListProjectMCPServersOutput{
		OrganizationID: "org-a", ProjectID: testProjectID,
		Servers: []ProjectMCPServer{{ID: "server-a", Name: "Docs", Visibility: "public", Source: "toolset_only", CreatedAt: "2026-01-01T00:00:00Z"}},
	}, output)
	require.NotContains(t, body, privateURL)
	require.NotContains(t, body, `"url"`)
}

func TestDiagnosticsRequireVerifiedStaff(t *testing.T) {
	t.Parallel()
	for name, alter := range map[string]func(*Principal){
		"missing staff context":  func(p *Principal) { p.staff = nil },
		"mismatched staff email": func(p *Principal) { p.staff.Email = "someone-else@example.test" },
	} {
		for _, call := range []struct{ tool, args string }{
			{"get_organization_onboarding", `{"organization_id":"org-a"}`},
			{"list_project_mcp_servers", `{"organization_id":"org-a","project_id":"` + testProjectID + `"}`},
		} {
			reads := testDiagnosticsReads()
			_, body, _ := callDiagnosticReadToolAs(t, reads, alter, call.tool, call.args)
			require.Contains(t, body, `"isError":true`, name+": "+call.tool)
			require.Nil(t, reads.onboardingInput, name+": "+call.tool)
			require.Nil(t, reads.serversInput, name+": "+call.tool)
		}
	}
}

func TestDiagnosticsRejectInexactTargetsAndFailClosed(t *testing.T) {
	t.Parallel()
	for _, args := range []string{`{"organization_id":" org-a"}`, `{"organization_id":"org-a "}`, `{"organization_id":""}`} {
		reads := testDiagnosticsReads()
		_, body, _ := callDiagnosticReadTool(t, reads, "get_organization_onboarding", args)
		require.Contains(t, body, `"isError":true`)
		require.Nil(t, reads.onboardingInput)
	}
	reads := testDiagnosticsReads()
	reads.onboarding.OrganizationID = "org-b"
	_, body, _ := callDiagnosticReadTool(t, reads, "get_organization_onboarding", `{"organization_id":"org-a"}`)
	require.Contains(t, body, `"isError":true`)
	require.NotNil(t, reads.onboardingInput)

	for _, args := range []string{
		`{"organization_id":"org-a","project_id":"` + testProjectID + ` "}`,
		`{"organization_id":"org-a","project_id":"project-slug"}`,
		`{"organization_id":"org-a","project_id":"` + testProjectID + `"}`,
	} {
		reads := testDiagnosticsReads()
		if args == `{"organization_id":"org-a","project_id":"`+testProjectID+`"}` {
			reads.project.OrganizationID = "org-b"
		}
		_, body, _ := callDiagnosticReadTool(t, reads, "list_project_mcp_servers", args)
		require.Contains(t, body, `"isError":true`)
		require.Nil(t, reads.serversInput)
	}

	reads = testDiagnosticsReads()
	reads.err = errors.New("private query details")
	_, body, _ = callDiagnosticReadTool(t, reads, "get_organization_onboarding", `{"organization_id":"org-a"}`)
	require.Contains(t, body, `"isError":true`)
	require.NotContains(t, body, "private query details")

	reads = testDiagnosticsReads()
	for range maxProjectMCPServers + 1 {
		reads.servers.McpServers = append(reads.servers.McpServers, &gen.AdminMcpServer{ID: "synthetic-server"})
	}
	_, body, data := callDiagnosticReadTool(t, reads, "list_project_mcp_servers", `{"organization_id":"org-a","project_id":"`+testProjectID+`"}`)
	require.NotContains(t, body, `"isError":true`)
	var page ListProjectMCPServersOutput
	require.NoError(t, json.Unmarshal(data, &page))
	require.Len(t, page.Servers, maxProjectMCPServers)
	require.True(t, page.PossiblyIncomplete)
}
