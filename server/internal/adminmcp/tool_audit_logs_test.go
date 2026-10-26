package adminmcp

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/admin"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/conv"
)

type recordingFilteredActivityReader struct {
	*recordingActivityReader
	filterInput *admin.OrganizationActivityFilter
}

func (r *recordingFilteredActivityReader) ListOrganizationActivityFiltered(_ context.Context, filter admin.OrganizationActivityFilter) (*gen.AdminListOrganizationActivityResult, error) {
	r.filterInput = &filter
	return r.result, r.err
}

func testAuditReads() *recordingFilteredActivityReader {
	return &recordingFilteredActivityReader{recordingActivityReader: testActivityReads()}
}

func TestAuditLogEntriesFullFidelity(t *testing.T) {
	t.Parallel()
	reads := testAuditReads()
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
	require.Equal(t, &admin.OrganizationActivityFilter{OrganizationID: "org-a", Cursor: &cursor}, reads.filterInput)
	require.Nil(t, reads.input)
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
		reads := testAuditReads()
		for range count {
			reads.result.Logs = append(reads.result.Logs, &gen.AuditLog{ID: "event"})
		}
		_, body, data := callStaffReadTool(t, reads, "list_audit_log_entries", `{"organization_id":"org-a"}`)
		require.NotContains(t, body, `"isError":true`)
		require.Nil(t, reads.filterInput.Cursor)
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
		{"padded cursor", `{"organization_id":"org-a","cursor":"` + audit.EncodeCursor(1, "id") + `\n"}`},
		{"bad sequence", `{"organization_id":"org-a","cursor":"` + audit.EncodeCursor(1, "id")[1:] + `"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			reads := testAuditReads()
			_, body, data := callStaffReadTool(t, reads, "list_audit_log_entries", tc.args)
			require.Contains(t, body, `"isError":true`)
			require.Empty(t, data)
			require.Nil(t, reads.filterInput)
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
			reads := testAuditReads()
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
		reads := testAuditReads()
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
	for _, reads := range []OrganizationReader{nil, &recordingOrganizationReader{org: &gen.AdminOrganization{ID: "org-a"}}, testActivityReads()} {
		_, body, data := callStaffReadTool(t, reads, "list_audit_log_entries", `{"organization_id":"org-a"}`)
		require.Contains(t, body, errAuditLogsUnavailable.Error())
		require.Contains(t, body, `"isError":true`)
		require.Empty(t, data)
		_, body, _ = callStaffReadTool(t, reads, "get_admin_context", `{}`)
		require.NotContains(t, body, "investigate full audit log entries")
	}
	_, body, _ := callStaffReadTool(t, testAuditReads(), "get_admin_context", `{}`)
	require.Contains(t, body, "investigate full audit log entries")
}

func TestAuditLogEntriesRequireVerifiedStaffContext(t *testing.T) {
	t.Parallel()
	reads := testAuditReads()
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
	require.Nil(t, reads.filterInput)
}

func TestAuditLogEntriesSnapshotJSONValues(t *testing.T) {
	t.Parallel()
	for _, value := range []string{`{}`, `[]`, `"text"`, `42`, `true`, `null`} {
		t.Run(value, func(t *testing.T) {
			t.Parallel()
			reads := testAuditReads()
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

func TestAuditLogEntriesForwardsTypedFilters(t *testing.T) {
	t.Parallel()
	reads := testAuditReads()
	projectID := uuid.New()
	cursor := audit.EncodeCursor(100, "previous-event")
	input := ListAuditLogEntriesInput{
		OrganizationID: "org-a", Cursor: cursor, ProjectID: projectID.String(),
		ActorID: "actor-id", Action: "future:action", SubjectType: "future_subject",
		SubjectID: "subject-id", ActingSurface: "future_surface",
		From: "2026-01-01T02:00:00+02:00", To: "2026-01-02T02:00:00+02:00",
	}
	args, err := json.Marshal(input)
	require.NoError(t, err)
	_, body, _ := callStaffReadTool(t, reads, "list_audit_log_entries", string(args))
	require.NotContains(t, body, `"isError":true`)
	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	to := from.Add(24 * time.Hour)
	require.Equal(t, &admin.OrganizationActivityFilter{
		OrganizationID: "org-a", Cursor: &cursor, ProjectID: uuid.NullUUID{UUID: projectID, Valid: true},
		ActorID: &input.ActorID, Action: &input.Action, SubjectType: &input.SubjectType,
		SubjectID: &input.SubjectID, ActingSurface: &input.ActingSurface, From: &from, To: &to,
	}, reads.filterInput)
	require.Nil(t, reads.input, "the summary reader must not be used as a fallback")
}

func TestAuditLogEntriesRejectMalformedFiltersBeforeReads(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, field, value string }{
		{"project slug", "project_id", "project-slug"},
		{"compact project", "project_id", "123456781234123412341234567890ab"},
		{"uppercase project", "project_id", "12345678-1234-1234-1234-1234567890AB"},
		{"padded project", "project_id", " 12345678-1234-1234-1234-1234567890ab"},
		{"malformed from", "from", "yesterday"},
		{"malformed to", "to", "tomorrow"},
	}
	for _, field := range []string{"actor_id", "action", "subject_type", "subject_id", "acting_surface", "from", "to"} {
		limit := maxAuditFilterBytes
		if field == "from" || field == "to" {
			limit = maxAuditTimestampBytes
		}
		cases = append(cases,
			struct{ name, field, value string }{field + " padded", field, " value"},
			struct{ name, field, value string }{field + " whitespace", field, " "},
			struct{ name, field, value string }{field + " long", field, strings.Repeat("x", limit+1)},
			struct{ name, field, value string }{field + " multibyte", field, strings.Repeat("\u00e9", limit/2+1)},
		)
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			reads := testAuditReads()
			args, err := json.Marshal(map[string]string{"organization_id": "org-a", tc.field: tc.value})
			require.NoError(t, err)
			_, body, data := callStaffReadTool(t, reads, "list_audit_log_entries", string(args))
			require.Contains(t, body, `"isError":true`)
			require.Empty(t, data)
			require.Nil(t, reads.filterInput)
			require.Nil(t, reads.getInput)
		})
	}
}

func TestAuditLogEntriesTimestampGuards(t *testing.T) {
	t.Parallel()
	for _, field := range []string{"from", "to"} {
		for _, tc := range []struct{ name, value string }{
			{"long valid timestamp", "2026-01-01T00:00:00." + strings.Repeat("1", maxAuditTimestampBytes) + "Z"},
			{"leading padding", " 2026-01-01T00:00:00Z"},
			{"trailing padding", "2026-01-01T00:00:00Z "},
		} {
			t.Run(field+"/"+tc.name, func(t *testing.T) {
				t.Parallel()
				_, _, err := conv.ParseOptionalTimeWindow(&tc.value, nil)
				require.NoError(t, err, "the time parser alone must accept this value")
				reads := testAuditReads()
				args, err := json.Marshal(map[string]string{"organization_id": "org-a", field: tc.value})
				require.NoError(t, err)
				_, body, data := callStaffReadTool(t, reads, "list_audit_log_entries", string(args))
				require.Contains(t, body, `"isError":true`)
				require.Contains(t, body, "provide an unpadded "+field+" up to 64 bytes")
				require.Empty(t, data)
				require.Nil(t, reads.getInput)
				require.Nil(t, reads.filterInput)
			})
		}
	}
}

func TestAuditLogEntriesTimeWindows(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, from, to string
		valid          bool
	}{
		{"empty", "", "", true},
		{"from only", "2026-01-01T00:00:00Z", "", true},
		{"to only", "", "2026-01-01T00:00:00Z", true},
		{"equal", "2026-01-01T00:00:00Z", "2026-01-01T00:00:00Z", false},
		{"inverted", "2026-01-02T00:00:00Z", "2026-01-01T00:00:00Z", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			reads := testAuditReads()
			args, err := json.Marshal(ListAuditLogEntriesInput{OrganizationID: "org-a", From: tc.from, To: tc.to})
			require.NoError(t, err)
			_, body, _ := callStaffReadTool(t, reads, "list_audit_log_entries", string(args))
			if !tc.valid {
				require.Contains(t, body, `"isError":true`)
				require.Nil(t, reads.filterInput)
				require.Nil(t, reads.getInput)
				return
			}
			require.NotContains(t, body, `"isError":true`)
			require.Equal(t, tc.from != "", reads.filterInput.From != nil)
			require.Equal(t, tc.to != "", reads.filterInput.To != nil)
		})
	}
}

func TestAuditLogEntriesEmptyAndBoundaryFilters(t *testing.T) {
	t.Parallel()
	for _, value := range []string{"", strings.Repeat("x", maxAuditFilterBytes)} {
		reads := testAuditReads()
		args, err := json.Marshal(ListAuditLogEntriesInput{
			OrganizationID: "org-a", ActorID: value, Action: value, SubjectType: value, SubjectID: value, ActingSurface: value,
		})
		require.NoError(t, err)
		_, body, _ := callStaffReadTool(t, reads, "list_audit_log_entries", string(args))
		require.NotContains(t, body, `"isError":true`)
		for _, filter := range []*string{reads.filterInput.ActorID, reads.filterInput.Action, reads.filterInput.SubjectType, reads.filterInput.SubjectID, reads.filterInput.ActingSurface} {
			if value == "" {
				require.Nil(t, filter)
			} else {
				require.Equal(t, value, *filter)
			}
		}
	}
}

func TestAuditLogEntriesNoUnfilteredFallback(t *testing.T) {
	t.Parallel()
	reads := testActivityReads()
	_, body, _ := callStaffReadTool(t, reads, "list_audit_log_entries", `{"organization_id":"org-a","action":"project:update"}`)
	require.Contains(t, body, errAuditLogsUnavailable.Error())
	require.Nil(t, reads.input)
	require.Nil(t, reads.getInput)
	_, body, _ = callStaffReadTool(t, reads, "get_admin_context", `{}`)
	require.Contains(t, body, "inspect paginated organization activity")
	require.NotContains(t, body, "investigate full audit log entries")
}

func TestAuditLogEntriesAvailableWithoutSummaryReader(t *testing.T) {
	t.Parallel()
	reads := testAuditReads()
	filteredOnly := struct {
		OrganizationReader
		FilteredActivityReader
	}{OrganizationReader: reads, FilteredActivityReader: reads}
	_, body, _ := callStaffReadTool(t, filteredOnly, "get_admin_context", `{}`)
	require.Contains(t, body, "investigate full audit log entries")
	require.NotContains(t, body, "inspect paginated organization activity")
	_, body, _ = callStaffReadTool(t, filteredOnly, "list_audit_log_entries", `{"organization_id":"org-a"}`)
	require.NotContains(t, body, `"isError":true`)
	require.NotNil(t, reads.filterInput)
	require.Nil(t, reads.input)
}
