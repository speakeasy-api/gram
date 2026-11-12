package adminmcp

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
)

type recordingAssistantReader struct {
	*recordingOrganizationReader
	listInput *gen.ListOrganizationAssistantsPayload
	list      *gen.AdminOrganizationAssistants
}

func (r *recordingAssistantReader) ListOrganizationAssistants(_ context.Context, input *gen.ListOrganizationAssistantsPayload) (*gen.AdminOrganizationAssistants, error) {
	r.listInput = input
	return r.list, nil
}

func testAssistantReads(listedOrg string, assistants ...*gen.AdminOrganizationAssistant) *recordingAssistantReader {
	return &recordingAssistantReader{
		recordingOrganizationReader: &recordingOrganizationReader{org: &gen.AdminOrganization{ID: "org-a"}},
		list:                        &gen.AdminOrganizationAssistants{OrganizationID: listedOrg, Assistants: assistants},
	}
}

func testAdminAssistant() *gen.AdminOrganizationAssistant {
	return &gen.AdminOrganizationAssistant{
		ID: "assistant-a", Name: "Assistant", ProjectID: "project-a", ProjectSlug: "project", ProjectName: "Project",
		Status: "active", IdentityState: "NEVER_CONFIGURED", RuntimeVersion: 2,
		CreatedByUserID: new("user-a"), CreatedByUserEmail: new("creator@example.invalid"), CreatorActive: false,
		CreatedAt: "2026-01-01T00:00:00Z",
	}
}

func TestListOrganizationAssistantsProjectsCreator(t *testing.T) {
	t.Parallel()
	reads := testAssistantReads("org-a", testAdminAssistant())

	status, _, data := callStaffReadTool(t, reads, "list_organization_assistants", `{"organization_id":"org-a"}`)
	require.Equal(t, http.StatusOK, status)
	require.Equal(t, "org-a", reads.listInput.OrganizationID)
	var output ListOrganizationAssistantsOutput
	require.NoError(t, json.Unmarshal(data, &output))
	require.False(t, output.PossiblyIncomplete)
	require.Equal(t, []AssistantListing{{
		ID: "assistant-a", Name: "Assistant", ProjectID: "project-a", ProjectSlug: "project", Status: "active",
		IdentityState: "NEVER_CONFIGURED", RuntimeVersion: 2, CreatorUserID: "user-a", CreatorEmail: "creator@example.invalid",
		CreatorActive: false, CreatedAt: "2026-01-01T00:00:00Z",
	}}, output.Assistants)
}

func TestListOrganizationAssistantsFailsClosed(t *testing.T) {
	t.Parallel()
	other := testAssistantReads("org-a", testAdminAssistant())
	other.org.ID = "org-b"
	_, body, _ := callStaffReadTool(t, other, "list_organization_assistants", `{"organization_id":"org-a"}`)
	require.Contains(t, body, `"isError":true`)
	require.Nil(t, other.listInput, "a slug or another organization never reaches the list")

	mismatched := testAssistantReads("org-b", testAdminAssistant())
	_, body, _ = callStaffReadTool(t, mismatched, "list_organization_assistants", `{"organization_id":"org-a"}`)
	require.Contains(t, body, `"isError":true`)
	require.NotContains(t, body, "assistant-a")

	missing := testAssistantReads("org-a", nil)
	_, body, _ = callStaffReadTool(t, missing, "list_organization_assistants", `{"organization_id":"org-a"}`)
	require.Contains(t, body, `"isError":true`)
}

func TestListOrganizationAssistantsCapsResults(t *testing.T) {
	t.Parallel()
	assistants := make([]*gen.AdminOrganizationAssistant, maxOrganizationAssistants+1)
	for i := range assistants {
		assistants[i] = testAdminAssistant()
	}
	reads := testAssistantReads("org-a", assistants...)

	_, _, data := callStaffReadTool(t, reads, "list_organization_assistants", `{"organization_id":"org-a"}`)
	var output ListOrganizationAssistantsOutput
	require.NoError(t, json.Unmarshal(data, &output))
	require.Len(t, output.Assistants, maxOrganizationAssistants)
	require.True(t, output.PossiblyIncomplete)
}
