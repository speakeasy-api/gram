package adminmcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

const testRegistryID = "a3fe1d35-4855-4dd8-a863-a0f45806e817"

type recordingRegistryReader struct {
	listInput *gen.ListRegistryEntriesPayload
	getInput  *gen.GetRegistryEntryPayload
	page      *gen.AdminRegistryPage
	entry     *gen.AdminRegistryEntry
	listErr   error
	getErr    error
}

func (r *recordingRegistryReader) ListRegistryEntries(_ context.Context, input *gen.ListRegistryEntriesPayload) (*gen.AdminRegistryPage, error) {
	r.listInput = input
	return r.page, r.listErr
}

func (r *recordingRegistryReader) GetRegistryEntry(_ context.Context, input *gen.GetRegistryEntryPayload) (*gen.AdminRegistryEntry, error) {
	r.getInput = input
	return r.entry, r.getErr
}

func registryToolCall(t *testing.T, reader RegistryReader, name, args string, verified bool) (string, json.RawMessage) {
	t.Helper()
	principal := staffPrincipal()
	if verified {
		principal.staff = &contextvalues.AdminAuthContext{SessionID: "browser-session", OIDCSubject: "staff-subject", Email: principal.Email}
	}
	runtime := NewRuntime(&testAuthenticator{principal: principal}, "", &recordingOrganizationReader{})
	registerRegistryTools(runtime.server, reader)
	request := httptest.NewRequest(http.MethodPost, Path, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"`+name+`","arguments":`+args+`}}`))
	request.Header.Set("Authorization", "Bearer test-token")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json, text/event-stream")
	response := httptest.NewRecorder()
	runtime.Handler().ServeHTTP(response, request)
	require.Equal(t, http.StatusOK, response.Code)
	var message struct {
		Result struct {
			StructuredContent json.RawMessage `json:"structuredContent"`
			IsError           bool            `json:"isError"`
		} `json:"result"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &message))
	return response.Body.String(), message.Result.StructuredContent
}

func TestRegistryListRequiresVerifiedStaffAndBoundsInputs(t *testing.T) {
	t.Parallel()
	published := true
	cursor := "next-page"
	reader := &recordingRegistryReader{page: &gen.AdminRegistryPage{
		Entries:    []*gen.AdminRegistrySummary{{ID: testRegistryID, Name: "example/server", Published: true, UpdatedAt: "2026-01-02T03:04:05Z", Issues: []*gen.AdminRegistryIssue{{Path: "/server/name", Message: "does not match required pattern"}}}},
		NextCursor: &cursor,
	}}
	body, data := registryToolCall(t, reader, "list_registry_entries", `{"query":"server","published":true,"cursor":"previous","limit":5}`, true)
	require.NotContains(t, body, `"isError":true`)
	require.Equal(t, "server", *reader.listInput.Query)
	require.Equal(t, &published, reader.listInput.Published)
	require.Equal(t, "previous", *reader.listInput.Cursor)
	require.Equal(t, int32(5), *reader.listInput.Limit)
	var page RegistryEntryPage
	require.NoError(t, json.Unmarshal(data, &page))
	require.Equal(t, "next-page", *page.NextCursor)
	require.Equal(t, "invalid_pattern", page.Entries[0].Issues[0].Category)

	reader.listInput = nil
	body, _ = registryToolCall(t, reader, "list_registry_entries", `{}`, false)
	require.Contains(t, body, `"isError":true`)
	require.Nil(t, reader.listInput)
	for _, args := range []string{`{"limit":51}`, `{"limit":-1}`, `{"query":"` + strings.Repeat("q", maxRegistryQuery+1) + `"}`, `{"cursor":"` + strings.Repeat("c", maxRegistryCursor+1) + `"}`} {
		body, _ = registryToolCall(t, reader, "list_registry_entries", args, true)
		require.Contains(t, body, `"isError":true`)
		require.Nil(t, reader.listInput)
	}
}

func TestRegistryDetailProjectsReviewedFieldsAndSafeIssues(t *testing.T) {
	t.Parallel()
	secret := "do-not-return-secret-value"
	description := "untrusted description"
	reader := &recordingRegistryReader{entry: &gen.AdminRegistryEntry{
		ID: testRegistryID, Published: true, CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-03T00:00:00Z",
		DataJSON: `{"server":{"name":"example/server","description":"` + description + `","version":"1.2.3","packages":[{"registryType":"npm","identifier":"` + secret + `","transport":{"type":"stdio"},"runtimeArguments":[{"value":"` + secret + `"}],"environmentVariables":[{"name":"TOKEN","value":"` + secret + `"}]}],"remotes":[{"type":"streamable-http","url":"https://` + secret + `"}]},"_meta":{"io.modelcontextprotocol.registry/official":{"publishedAt":"2026-01-01T00:00:00Z","updatedAt":"2026-01-02T00:00:00Z"},"com.speakeasy.ai/registry":{"publishedAt":"2026-01-02T00:00:00Z"},"example/extra":"arbitrary vendor value"},"extension":"` + secret + `"}`,
		Issues:   []*gen.AdminRegistryIssue{{Path: "/server/" + secret, Message: "value " + secret + " rejected"}},
	}}
	body, data := registryToolCall(t, reader, "get_registry_entry", `{"id":"`+testRegistryID+`"}`, true)
	require.NotContains(t, body, `"isError":true`)
	require.NotContains(t, body, secret)

	require.NotContains(t, body, "https://")
	require.Equal(t, testRegistryID, reader.getInput.ID)
	var detail RegistryEntryDetail
	require.NoError(t, json.Unmarshal(data, &detail))
	require.True(t, detail.Found)
	require.Equal(t, "example/server", detail.Name)
	require.Equal(t, description, detail.Description)
	require.True(t, detail.ProjectionAvailable)
	require.Equal(t, "2026-01-03T00:00:00Z", detail.UpdatedAt)
	require.Equal(t, "2026-01-02T00:00:00Z", detail.PublishedAt)
	require.Equal(t, []RegistryTransportSummary{{Type: "stdio", Count: 1}, {Type: "streamable-http", Count: 1}}, detail.Transports)
	require.Equal(t, "record", detail.Issues[0].Path)
	require.Equal(t, "schema_constraint", detail.Issues[0].Category)
}

func TestRegistryDetailExactUUIDAndNotFoundSemantics(t *testing.T) {
	t.Parallel()
	reader := &recordingRegistryReader{}
	body, _ := registryToolCall(t, reader, "get_registry_entry", `{"id":"slug"}`, true)
	require.Contains(t, body, `"isError":true`)
	require.Nil(t, reader.getInput)

	for _, missing := range []error{gen.MakeNotFound(errors.New("not present")), oops.C(oops.CodeNotFound), fmt.Errorf("registry lookup: %w", oops.C(oops.CodeNotFound))} {
		reader.getErr = missing
		body, data := registryToolCall(t, reader, "get_registry_entry", `{"id":"`+testRegistryID+`"}`, true)
		require.NotContains(t, body, `"isError":true`)
		var detail RegistryEntryDetail
		require.NoError(t, json.Unmarshal(data, &detail))
		require.False(t, detail.Found)
	}

	reader.getErr = errors.New("private database failure")
	body, _ = registryToolCall(t, reader, "get_registry_entry", `{"id":"`+testRegistryID+`"}`, true)
	require.Contains(t, body, `"isError":true`)
	require.NotContains(t, body, "private database failure")
}

func TestRegistryDetailRetainsSafeSummaryForInvalidStoredRecords(t *testing.T) {
	t.Parallel()
	for _, record := range []string{
		`{}`,
		`{"server":null}`,
		`{"server":{"description":"synthetic record","version":"1"}}`,
		`{"server":{"name":"example/server","version":"1"}}`,
		`{"server":{"name":"example/server","description":"synthetic record"}}`,
		`{"server":{"name":" ","description":"synthetic record","version":"1"}}`,
		`{"server":{"name":"example/server","description":"synthetic record","version":"1","packages":[{"transport":{}}]}}`,
		`{"server":{"name":"example/server","description":"synthetic record","version":"1","remotes":[{"type":"unsupported"}]}}`,
		`{"server":{"name":42}}`,
	} {
		reader := &recordingRegistryReader{entry: &gen.AdminRegistryEntry{
			ID: testRegistryID, DataJSON: record, UpdatedAt: "2026-01-03T00:00:00Z",
			Issues: []*gen.AdminRegistryIssue{{Path: "/server/name", Message: "unsafe stored value"}},
		}}
		body, data := registryToolCall(t, reader, "get_registry_entry", `{"id":"`+testRegistryID+`"}`, true)
		require.NotContains(t, body, `"isError":true`)
		require.NotContains(t, body, "unsafe stored value")
		var detail RegistryEntryDetail
		require.NoError(t, json.Unmarshal(data, &detail))
		require.True(t, detail.Found)
		require.False(t, detail.ProjectionAvailable)
		require.Equal(t, testRegistryID, detail.ID)
		require.Equal(t, "2026-01-03T00:00:00Z", detail.UpdatedAt)
		require.Equal(t, []RegistryValidationIssue{{Path: "server.name", Category: "schema_constraint"}}, detail.Issues)
		require.Empty(t, detail.Transports)
	}
}

func TestRegistryDetailRetainsSafeSummaryForOversizedStoredRecords(t *testing.T) {
	t.Parallel()
	reader := &recordingRegistryReader{entry: &gen.AdminRegistryEntry{
		ID: testRegistryID, Published: false, CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-03T00:00:00Z",
		DataJSON: `{"server":{"name":"example/server","description":"synthetic record","version":"1"},"extension":"` + strings.Repeat("x", maxRegistryDataJSON) + `"}`,
		Issues:   []*gen.AdminRegistryIssue{{Message: "record exceeds byte limit"}},
	}}
	body, data := registryToolCall(t, reader, "get_registry_entry", `{"id":"`+testRegistryID+`"}`, true)
	require.NotContains(t, body, `"isError":true`)
	var detail RegistryEntryDetail
	require.NoError(t, json.Unmarshal(data, &detail))
	require.True(t, detail.Found)
	require.False(t, detail.ProjectionAvailable)
	require.Equal(t, testRegistryID, detail.ID)
	require.Equal(t, "2026-01-01T00:00:00Z", detail.CreatedAt)
	require.Equal(t, "2026-01-03T00:00:00Z", detail.UpdatedAt)
	require.Equal(t, []RegistryValidationIssue{{Path: "record", Category: "record_too_large"}}, detail.Issues)
	require.Empty(t, detail.Name)
	require.Empty(t, detail.Description)
	require.Empty(t, detail.Version)
	require.Empty(t, detail.Transports)
	require.Less(t, len(body), MaxBodyBytes)
}

func TestRegistryProjectionBoundsAndSafeCategories(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("x", maxRegistryText+10)
	projection, ok := projectRegistryRecord([]byte(`{"server":{"name":"` + long + `","description":"line\nbreak","version":"v","remotes":[{"type":"sse"}]}}`))
	require.True(t, ok)
	require.LessOrEqual(t, len(projection.Name), maxRegistryText)
	require.Equal(t, "line\nbreak", projection.Description)
	require.Equal(t, "missing_required_property", safeRegistryIssueCategory("required property missing"))
	require.Equal(t, "schema_constraint", safeRegistryIssueCategory("unsafe error includes attacker value"))
	require.Equal(t, "line\nbreak", boundedText("line\nbreak continues", 10))
	require.Equal(t, `path\name`, boundedText(`path\name continues`, 9))
	require.Equal(t, "éé", boundedText("ééé", 5))
}
