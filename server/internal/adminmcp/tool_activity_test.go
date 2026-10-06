package adminmcp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
)

type recordingActivityReader struct {
	*recordingOrganizationReader
	input  *gen.ListOrganizationActivityPayload
	result *gen.AdminListOrganizationActivityResult
	err    error
}

func (r *recordingActivityReader) ListOrganizationActivity(_ context.Context, input *gen.ListOrganizationActivityPayload) (*gen.AdminListOrganizationActivityResult, error) {
	r.input = input
	return r.result, r.err
}

func testActivityReads() *recordingActivityReader {
	return &recordingActivityReader{
		recordingOrganizationReader: &recordingOrganizationReader{org: &gen.AdminOrganization{ID: "org-a"}},
		result:                      &gen.AdminListOrganizationActivityResult{},
	}
}

func TestOrganizationActivityExactTargetAndSafeProjection(t *testing.T) {
	t.Parallel()
	reads := testActivityReads()
	name, secret, cursor := "Staff member", "private audit payload", "next-page"
	reads.result.Logs = []*gen.AuditLog{{
		ID: "event-1", Action: "organization:updated", ActorType: "user", ActorDisplayName: &name,
		ActorID: "private-actor", SubjectID: "private-subject", SubjectType: "organization",
		ActingSurface: "dashboard", ActingClientID: &secret, BeforeSnapshot: json.RawMessage(`{"credential":"private audit payload"}`),
		AfterSnapshot: json.RawMessage(`{"credential":"private audit payload"}`), Metadata: map[string]any{"credential": secret},
		CreatedAt: "2026-01-01T00:00:00Z",
	}}
	reads.result.NextCursor = &cursor
	status, body, data := callStaffReadTool(t, reads, "list_organization_activity", `{"organization_id":"org-a","cursor":"previous-page"}`)
	require.Equal(t, http.StatusOK, status)
	require.Equal(t, "org-a", reads.getInput.IDOrSlug)
	require.Equal(t, "org-a", reads.input.OrganizationID)
	require.Equal(t, "previous-page", *reads.input.Cursor)
	require.Nil(t, reads.input.AdminSessionToken)
	var output ListOrganizationActivityOutput
	require.NoError(t, json.Unmarshal(data, &output))
	require.Equal(t, ListOrganizationActivityOutput{
		OrganizationID: "org-a", NextCursor: &cursor,
		Activity: []OrganizationActivity{{
			ID: "event-1", Action: "organization:updated", ActorType: "user", ActorDisplayName: &name,
			SubjectType: "organization", ActingSurface: "dashboard", CreatedAt: "2026-01-01T00:00:00Z",
		}},
	}, output)
	for _, excluded := range []string{secret, "private-actor", "private-subject", "before_snapshot", "after_snapshot", "metadata", "admin_session_token"} {
		require.NotContains(t, body, excluded)
	}

	reads = testActivityReads()
	_, _, _ = callStaffReadTool(t, reads, "list_organization_activity", `{"organization_id":"org-a"}`)
	require.Nil(t, reads.input.Cursor)
}

func TestOrganizationActivityFailsClosed(t *testing.T) {
	t.Parallel()
	args := `{"organization_id":"org-a"}`
	for _, invalid := range []string{`{"organization_id":" org-a"}`, `{"organization_id":""}`, `{"organization_id":"org-a","cursor":"` + strings.Repeat("x", 129) + `"}`} {
		reads := testActivityReads()
		_, body, _ := callStaffReadTool(t, reads, "list_organization_activity", invalid)
		require.Contains(t, body, `"isError":true`)
		require.Nil(t, reads.input)
	}
	reads := testActivityReads()
	reads.org.ID = "org-b"
	_, body, _ := callStaffReadTool(t, reads, "list_organization_activity", args)
	require.Contains(t, body, `"isError":true`)
	require.Nil(t, reads.input)

	reads = testActivityReads()
	reads.err = errors.New("private database failure")
	_, body, _ = callStaffReadTool(t, reads, "list_organization_activity", args)
	require.Contains(t, body, `"isError":true`)
	require.NotContains(t, body, "private database failure")

	_, body, _ = callStaffReadTool(t, &recordingOrganizationReader{org: &gen.AdminOrganization{ID: "org-a"}}, "list_organization_activity", args)
	require.Contains(t, body, `"isError":true`)

	for _, logs := range [][]*gen.AuditLog{make([]*gen.AuditLog, maxOrganizationActivityPage+1), {nil}} {
		reads = testActivityReads()
		reads.result.Logs = logs
		_, body, _ = callStaffReadTool(t, reads, "list_organization_activity", args)
		require.Contains(t, body, `"isError":true`)
	}

	reads = testActivityReads()
	reads.result = nil
	_, body, _ = callStaffReadTool(t, reads, "list_organization_activity", args)
	require.Contains(t, body, `"isError":true`)

	reads = testActivityReads()
	longCursor := strings.Repeat("x", 129)
	reads.result.NextCursor = &longCursor
	_, body, _ = callStaffReadTool(t, reads, "list_organization_activity", args)
	require.Contains(t, body, `"isError":true`)
	require.NotContains(t, body, longCursor)
}

func TestOrganizationActivityPageAndContextAvailability(t *testing.T) {
	t.Parallel()
	reads := testActivityReads()
	reads.result.Logs = make([]*gen.AuditLog, maxOrganizationActivityPage)
	for i := range reads.result.Logs {
		reads.result.Logs[i] = &gen.AuditLog{ID: "event"}
	}
	status, _, data := callStaffReadTool(t, reads, "list_organization_activity", `{"organization_id":"org-a"}`)
	require.Equal(t, http.StatusOK, status)
	var output ListOrganizationActivityOutput
	require.NoError(t, json.Unmarshal(data, &output))
	require.Len(t, output.Activity, maxOrganizationActivityPage)

	_, body, _ := callStaffReadTool(t, reads, "get_admin_context", `{}`)
	require.Contains(t, body, "inspect paginated organization activity (without snapshots or metadata)")
	_, body, _ = callStaffReadTool(t, &recordingOrganizationReader{}, "get_admin_context", `{}`)
	require.NotContains(t, body, "inspect paginated organization activity")
}
