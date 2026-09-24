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

const testProjectID = "f744be5c-02ed-4da3-924b-6867c785a116"

type recordingProjectReader struct {
	*recordingOrganizationReader
	listInput *gen.ListOrganizationProjectsPayload
	getInput  *gen.GetProjectPayload
	list      *gen.AdminListOrganizationProjectsResult
	project   *gen.AdminProjectDetail
	err       error
}

func (r *recordingProjectReader) ListOrganizationProjects(_ context.Context, input *gen.ListOrganizationProjectsPayload) (*gen.AdminListOrganizationProjectsResult, error) {
	r.listInput = input
	return r.list, r.err
}

func (r *recordingProjectReader) GetProject(_ context.Context, input *gen.GetProjectPayload) (*gen.AdminProjectDetail, error) {
	r.getInput = input
	return r.project, r.err
}

func testProjectReads() *recordingProjectReader {
	return &recordingProjectReader{
		recordingOrganizationReader: &recordingOrganizationReader{org: &gen.AdminOrganization{ID: "org-a"}},
		list:                        &gen.AdminListOrganizationProjectsResult{},
	}
}

func TestListOrganizationProjectsBoundsAndSafeProjection(t *testing.T) {
	t.Parallel()
	reads := testProjectReads()
	reads.list.Projects = []*gen.AdminProject{{ID: testProjectID, Name: "Example", Slug: "example", McpServerCount: 2, CreatedAt: "2026-01-01T00:00:00Z"}}
	status, body, data := callStaffReadTool(t, reads, "list_organization_projects", `{"organization_id":"org-a"}`)
	require.Equal(t, http.StatusOK, status)
	require.Equal(t, "org-a", reads.recordingOrganizationReader.getInput.IDOrSlug)
	require.Equal(t, "org-a", reads.listInput.OrganizationID)
	var output ListOrganizationProjectsOutput
	require.NoError(t, json.Unmarshal(data, &output))
	require.Equal(t, "org-a", output.OrganizationID)
	require.Equal(t, []ProjectListing{{ID: testProjectID, Name: "Example", Slug: "example", MCPServerCount: 2, CreatedAt: "2026-01-01T00:00:00Z"}}, output.Projects)
	require.False(t, output.PossiblyIncomplete)
	require.NotContains(t, body, "private-provider-id")

	reads = testProjectReads()
	reads.list.Projects = make([]*gen.AdminProject, maxOrganizationProjects)
	for i := range reads.list.Projects {
		reads.list.Projects[i] = &gen.AdminProject{ID: testProjectID}
	}
	_, _, data = callStaffReadTool(t, reads, "list_organization_projects", `{"organization_id":"org-a"}`)
	require.NoError(t, json.Unmarshal(data, &output))
	require.Len(t, output.Projects, maxOrganizationProjects)
	require.True(t, output.PossiblyIncomplete)

	reads = testProjectReads()
	reads.list.Projects = append(reads.list.Projects, make([]*gen.AdminProject, maxOrganizationProjects+1)...)
	_, body, _ = callStaffReadTool(t, reads, "list_organization_projects", `{"organization_id":"org-a"}`)
	require.Contains(t, body, `"isError":true`)
}

func TestProjectSummaryRequiresExactOrganizationAndProject(t *testing.T) {
	t.Parallel()
	reads := testProjectReads()
	reads.project = &gen.AdminProjectDetail{
		ID: testProjectID, OrganizationID: "org-a", Name: "Example", Slug: "example",
		ToolsetCount: 2, DeploymentCount: 3, HTTPToolCount: 4,
		EnvironmentCount: 5, APIKeyCount: 6, AssistantCount: 7,
		LogoAssetID: new("private-provider-id"), FunctionsRunnerVersion: new("private-version"),
	}
	args := `{"organization_id":"org-a","project_id":"` + testProjectID + `"}`
	status, body, data := callStaffReadTool(t, reads, "get_project_summary", args)
	require.Equal(t, http.StatusOK, status)
	require.Equal(t, testProjectID, reads.getInput.IDOrSlug)
	require.Equal(t, "org-a", *reads.getInput.OrganizationIDOrSlug)
	var summary ProjectSummary
	require.NoError(t, json.Unmarshal(data, &summary))
	require.Equal(t, "org-a", summary.OrganizationID)
	require.Equal(t, 6, summary.APIKeyCount)
	require.NotContains(t, body, "private-provider-id")
	require.NotContains(t, body, "private-version")

	for _, invalid := range []string{
		`{"organization_id":"org-a","project_id":"example"}`,
		`{"organization_id":"org-a","project_id":" "}`,
		`{"organization_id":"org-a","project_id":"F744BE5C-02ED-4DA3-924B-6867C785A116"}`,
	} {
		other := testProjectReads()
		_, body, _ := callStaffReadTool(t, other, "get_project_summary", invalid)
		require.Contains(t, body, `"isError":true`)
		require.Nil(t, other.getInput)
	}

	for _, name := range []string{"list_organization_projects", "get_project_summary"} {
		other := testProjectReads()
		other.org.ID = "org-b"
		_, body, _ := callStaffReadTool(t, other, name, args)
		require.Contains(t, body, `"isError":true`)
		require.Nil(t, other.listInput)
		require.Nil(t, other.getInput)
	}

	reads.project.OrganizationID = "org-b"
	_, body, _ = callStaffReadTool(t, reads, "get_project_summary", args)
	require.Contains(t, body, `"isError":true`)
	require.NotContains(t, body, "Example")

	reads.project.OrganizationID = "org-a"
	reads.project.ID = "another-project"
	_, body, _ = callStaffReadTool(t, reads, "get_project_summary", args)
	require.Contains(t, body, `"isError":true`)
	require.NotContains(t, body, "Example")
}

func TestProjectReadsFailClosedAndContextReflectsAvailability(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, args string }{
		{"list_organization_projects", `{"organization_id":"org-a"}`},
		{"get_project_summary", `{"organization_id":"org-a","project_id":"` + testProjectID + `"}`},
	} {
		reads := testProjectReads()
		reads.err = errors.New("private database failure")
		_, body, _ := callStaffReadTool(t, reads, tc.name, tc.args)
		require.Contains(t, body, `"isError":true`)
		require.NotContains(t, body, "private database failure")

		_, body, _ = callStaffReadTool(t, &recordingOrganizationReader{org: &gen.AdminOrganization{ID: "org-a"}}, tc.name, tc.args)
		require.Contains(t, body, `"isError":true`)
	}

	auth := &testAuthenticator{principal: staffPrincipal()}
	request := httptest.NewRequest(http.MethodPost, Path, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"get_admin_context","arguments":{}}}`))
	request.Header.Set("Authorization", "Bearer test-token")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json, text/event-stream")
	response := httptest.NewRecorder()
	NewRuntime(auth, "", testProjectReads()).Handler().ServeHTTP(response, request)
	require.Equal(t, http.StatusOK, response.Code)
	require.Contains(t, response.Body.String(), "inspect organization projects and project setup")
}
