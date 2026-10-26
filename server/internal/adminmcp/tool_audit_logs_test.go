package adminmcp

import (
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/audit"
)

func TestAuditLogEntriesFullFidelity(t *testing.T) {
	t.Parallel()
	reads := testActivityReads()
	log := &gen.AuditLog{
		ID: "event-1", ProjectID: new("project-id"), ProjectSlug: new("project-slug"),
		ActorID: "staff-id", ActorType: "user", ActorDisplayName: new("Staff member"), ActorSlug: new("staff-slug"),
		Action: "assistant:run", ActingSurface: "admin_mcp", ActingClientID: new("staff-client"),
		SubjectID: "assistant-id", SubjectType: "assistant", SubjectDisplayName: new("Assistant name"), SubjectSlug: new("assistant-slug"),
		BeforeSnapshot: json.RawMessage(`{"nested":[1,true,null,"<untrusted>"]}`), AfterSnapshot: json.RawMessage(`["after",{"enabled":true}]`),
		Metadata: map[string]any{"nested": map[string]any{"email": "example@example.test", "count": float64(2)}}, CreatedAt: "2026-01-01T00:00:00Z",
	}
	reads.result.Logs = []*gen.AuditLog{log}
	cursor := audit.EncodeCursor(100, "event-previous")
	next := audit.EncodeCursor(99, log.ID)
	reads.result.NextCursor = &next
	status, body, data := callStaffReadTool(t, reads, "list_audit_log_entries", `{"organization_id":"org-a","cursor":"`+cursor+`"}`)
	require.Equal(t, http.StatusOK, status)
	require.NotContains(t, body, `"isError":true`)
	require.Equal(t, "org-a", reads.getInput.IDOrSlug)
	require.Nil(t, reads.getInput.AdminSessionToken)
	require.Equal(t, &gen.ListOrganizationActivityPayload{OrganizationID: "org-a", Cursor: &cursor}, reads.input)
	var output ListAuditLogEntriesOutput
	require.NoError(t, json.Unmarshal(data, &output))
	require.Equal(t, "org-a", output.OrganizationID)
	require.Equal(t, &next, output.NextCursor)
	require.Len(t, output.Entries, 1)
	entry := output.Entries[0]
	require.JSONEq(t, string(log.BeforeSnapshot), string(entry.BeforeSnapshot))
	require.JSONEq(t, string(log.AfterSnapshot), string(entry.AfterSnapshot))
	roundtrip := gen.AuditLog{
		ID: entry.ID, ProjectID: entry.ProjectID, ProjectSlug: entry.ProjectSlug,
		ActorID: entry.ActorID, ActorType: entry.ActorType, ActorDisplayName: entry.ActorDisplayName, ActorSlug: entry.ActorSlug,
		Action: entry.Action, ActingSurface: entry.ActingSurface, ActingClientID: entry.ActingClientID,
		SubjectID: entry.SubjectID, SubjectType: entry.SubjectType, SubjectDisplayName: entry.SubjectDisplayName, SubjectSlug: entry.SubjectSlug,
		BeforeSnapshot: log.BeforeSnapshot, AfterSnapshot: log.AfterSnapshot, Metadata: entry.Metadata, CreatedAt: entry.CreatedAt,
	}
	require.Equal(t, *log, roundtrip)
	require.NotContains(t, body, "test-staff-session")
	require.NotContains(t, body, "admin_session_token")
}

func TestAuditLogEntriesEmptyAndMaximumPages(t *testing.T) {
	t.Parallel()
	for _, count := range []int{0, maxOrganizationActivityPage} {
		reads := testActivityReads()
		for range count {
			reads.result.Logs = append(reads.result.Logs, &gen.AuditLog{ID: "event"})
		}
		_, body, data := callStaffReadTool(t, reads, "list_audit_log_entries", `{"organization_id":"org-a"}`)
		require.NotContains(t, body, `"isError":true`)
		require.Nil(t, reads.input.Cursor)
		var output ListAuditLogEntriesOutput
		require.NoError(t, json.Unmarshal(data, &output))
		require.NotNil(t, output.Entries)
		require.Len(t, output.Entries, count)
		require.Nil(t, output.NextCursor)
	}
}

func TestAuditLogEntriesRejectInvalidTargetsAndCursors(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, args string }{
		{"empty", `{"organization_id":""}`},
		{"padded", `{"organization_id":" org-a"}`},
		{"long target", `{"organization_id":"` + strings.Repeat("x", 129) + `"}`},
		{"slug", `{"organization_id":"example-slug"}`},
		{"long cursor", `{"organization_id":"org-a","cursor":"` + strings.Repeat("x", 129) + `"}`},
		{"bad encoding", `{"organization_id":"org-a","cursor":"!!!"}`},
		{"bad shape", `{"organization_id":"org-a","cursor":"YWJj"}`},
		{"bad sequence", `{"organization_id":"org-a","cursor":"` + audit.EncodeCursor(1, "id")[1:] + `"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			reads := testActivityReads()
			_, body, data := callStaffReadTool(t, reads, "list_audit_log_entries", tc.args)
			require.Contains(t, body, `"isError":true`)
			require.Empty(t, data)
			require.Nil(t, reads.input)
		})
	}
}

func TestAuditLogEntriesBackendFailuresDoNotLeakPartialDetails(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		page *gen.AdminListOrganizationActivityResult
		err  error
	}{
		{name: "backend error", err: errors.New("private database failure")},
		{name: "nil page"},
		{name: "51 rows", page: &gen.AdminListOrganizationActivityResult{Logs: make([]*gen.AuditLog, 51)}},
		{name: "nil row after details", page: &gen.AdminListOrganizationActivityResult{Logs: []*gen.AuditLog{{ID: "private-event"}, nil}}},
		{name: "long next cursor", page: &gen.AdminListOrganizationActivityResult{NextCursor: new(strings.Repeat("x", 129))}},
		{name: "invalid before", page: &gen.AdminListOrganizationActivityResult{Logs: []*gen.AuditLog{{ID: "private-event", BeforeSnapshot: json.RawMessage(`{"private":`)}}}},
		{name: "invalid after", page: &gen.AdminListOrganizationActivityResult{Logs: []*gen.AuditLog{{ID: "private-event", AfterSnapshot: json.RawMessage(`{"private":`)}}}},
		{name: "invalid metadata", page: &gen.AdminListOrganizationActivityResult{Logs: []*gen.AuditLog{{ID: "private-event", Metadata: map[string]any{"private": math.NaN()}}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			reads := testActivityReads()
			reads.result, reads.err = tc.page, tc.err
			_, body, data := callStaffReadTool(t, reads, "list_audit_log_entries", `{"organization_id":"org-a"}`)
			require.Contains(t, body, `"isError":true`)
			require.Contains(t, body, errAuditLogsUnavailable.Error())
			require.NotContains(t, body, "private")
			require.NotContains(t, body, "next_cursor")
			require.Empty(t, data)
		})
	}
}

func TestAuditLogEntriesSerializedByteLimit(t *testing.T) {
	t.Parallel()
	for _, extra := range []int{0, 1} {
		reads := testActivityReads()
		log := &gen.AuditLog{ID: "private-event", BeforeSnapshot: json.RawMessage(`""`)}
		reads.result.Logs = []*gen.AuditLog{log}
		_, _, baseline := callStaffReadTool(t, reads, "list_audit_log_entries", `{"organization_id":"org-a"}`)
		require.NotEmpty(t, baseline)
		log.BeforeSnapshot = json.RawMessage(`"` + strings.Repeat("x", maxAuditOutputBytes-len(baseline)+extra) + `"`)
		_, body, data := callStaffReadTool(t, reads, "list_audit_log_entries", `{"organization_id":"org-a"}`)
		if extra == 0 {
			require.NotContains(t, body, `"isError":true`)
			require.Len(t, data, maxAuditOutputBytes)
		} else {
			require.Contains(t, body, `"isError":true`)
			require.Contains(t, body, "use narrower filters or the staff UI")
			require.NotContains(t, body, "private-event")
			require.Empty(t, data)
		}
	}
}

func TestAuditLogEntriesAvailability(t *testing.T) {
	t.Parallel()
	for _, reads := range []OrganizationReader{nil, &recordingOrganizationReader{org: &gen.AdminOrganization{ID: "org-a"}}} {
		_, body, data := callStaffReadTool(t, reads, "list_audit_log_entries", `{"organization_id":"org-a"}`)
		require.Contains(t, body, errAuditLogsUnavailable.Error())
		require.Contains(t, body, `"isError":true`)
		require.Empty(t, data)
		_, body, _ = callStaffReadTool(t, reads, "get_admin_context", `{}`)
		require.NotContains(t, body, "investigate full audit log entries")
	}
	_, body, _ := callStaffReadTool(t, testActivityReads(), "get_admin_context", `{}`)
	require.Contains(t, body, "investigate full audit log entries")
}

func TestAuditLogEntriesRequireVerifiedStaffContext(t *testing.T) {
	t.Parallel()
	reads := testActivityReads()
	auth := &testAuthenticator{principal: staffPrincipal()}
	request := httptest.NewRequest(http.MethodPost, Path, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"list_audit_log_entries","arguments":{"organization_id":"org-a"}}}`))
	request.Header.Set("Authorization", "Bearer test-token")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json, text/event-stream")
	response := httptest.NewRecorder()
	NewRuntime(auth, "", reads).Handler().ServeHTTP(response, request)
	require.Equal(t, http.StatusOK, response.Code)
	require.Contains(t, response.Body.String(), `"isError":true`)
	require.Contains(t, response.Body.String(), errAuditLogsUnavailable.Error())
	require.Nil(t, reads.getInput)
	require.Nil(t, reads.input)
}

func TestAuditLogEntriesSnapshotJSONValues(t *testing.T) {
	t.Parallel()
	for _, value := range []string{`{}`, `[]`, `"text"`, `42`, `true`, `null`} {
		t.Run(value, func(t *testing.T) {
			t.Parallel()
			reads := testActivityReads()
			reads.result.Logs = []*gen.AuditLog{{BeforeSnapshot: json.RawMessage(value), AfterSnapshot: json.RawMessage(value)}}
			_, body, data := callStaffReadTool(t, reads, "list_audit_log_entries", `{"organization_id":"org-a"}`)
			require.NotContains(t, body, `"isError":true`)
			var output ListAuditLogEntriesOutput
			require.NoError(t, json.Unmarshal(data, &output))
			require.Len(t, output.Entries, 1)
			require.JSONEq(t, value, string(output.Entries[0].BeforeSnapshot))
			require.JSONEq(t, value, string(output.Entries[0].AfterSnapshot))
		})
	}
}
