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
	onboardingInput *gen.GetOrganizationOnboardingPayload
	onboarding      *gen.AdminOnboardingConfiguration
	serversInput    *gen.ListProjectMcpServersPayload
	servers         *gen.AdminListProjectMcpServersResult
	err             error
}

func (r *recordingDiagnosticsReader) GetOrganizationOnboarding(_ context.Context, input *gen.GetOrganizationOnboardingPayload) (*gen.AdminOnboardingConfiguration, error) {
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
		onboarding:             &gen.AdminOnboardingConfiguration{OrganizationID: "org-a"},
		servers:                &gen.AdminListProjectMcpServersResult{},
	}
}

func TestOrganizationOnboardingExactTargetAndRedactedProjection(t *testing.T) {
	t.Parallel()
	reads := testDiagnosticsReads()
	preset := "guided"
	reads.onboarding.Preset = &preset
	reads.onboarding.Tasks = []*gen.AdminOnboardingTask{{Key: "connect", Title: "customer-authored private title", Description: "private description", Hidden: true}}
	reads.onboarding.Presets = []*gen.AdminOnboardingPreset{{Key: "guided", VisibleTaskKeys: []string{"connect"}}}
	status, body, data := callDiagnosticReadTool(t, reads, "get_organization_onboarding", `{"organization_id":"org-a"}`)
	require.Equal(t, http.StatusOK, status)
	require.Equal(t, "org-a", reads.recordingOrganizationReader.getInput.IDOrSlug)
	require.Equal(t, "org-a", reads.onboardingInput.OrganizationID)
	require.Nil(t, reads.onboardingInput.AdminSessionToken)
	var output OrganizationOnboardingOutput
	require.NoError(t, json.Unmarshal(data, &output))
	require.Equal(t, OrganizationOnboardingOutput{
		OrganizationID: "org-a", Preset: &preset,
		Tasks:   []OnboardingTask{{Key: "connect", Hidden: true}},
		Presets: []OnboardingPreset{{Key: "guided", VisibleTaskKeys: []string{"connect"}}},
	}, output)
	require.NotContains(t, body, "customer-authored private title")
	require.NotContains(t, body, "private description")
	require.NotContains(t, body, "admin_session_token")
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
