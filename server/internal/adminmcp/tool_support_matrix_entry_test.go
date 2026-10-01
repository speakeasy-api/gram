package adminmcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/supportmatrix"
)

// supportMatrixEntryFixture is a matrix the projection tests can mutate; the
// tool itself reads the embedded file.
const supportMatrixEntryFixture = `
capabilities:
  - { id: session, name: Session tracking, group: Observe }
  - { id: cost, name: Cost, group: Observe }
platforms:
  - { id: cli, name: CLI, vendor: Vendor, family: Agent, surface: CLI }
  - { id: web, name: Web, vendor: Vendor, family: Agent, surface: Web }
methods:
  - id: hooks
    name: Hooks
    vendor: Vendor
    plans: Team plans
    claims:
      session: { status: supported, note: via hooks, verify: false }
      cost: { status: partial, note: tokens only, verify: true }
    platforms:
      - platform: cli
        applicability: applicable
        accounts: { personal: unsupported, team: supported, enterprise: unknown }
        os: { mac: supported, linux: verify }
        note: cost only
        cells:
          session: { status: partial, note: sessions only, verify: true }
          cost: { status: supported, note: "", verify: false }
      - platform: web
        applicability: na
        accounts: { personal: unsupported, team: unsupported, enterprise: unsupported }
`

func parseSupportMatrixEntryFixture(t *testing.T) *supportmatrix.Matrix {
	t.Helper()
	matrix, err := supportmatrix.Parse([]byte(supportMatrixEntryFixture))
	require.NoError(t, err)
	return matrix
}

func verifiedStaffContext() *contextvalues.AdminAuthContext {
	return &contextvalues.AdminAuthContext{SessionID: "browser-session", OIDCSubject: "staff-subject", Email: staffPrincipal().Email}
}

// callSupportMatrixEntryTool calls the tool over HTTP; nil staff models a
// principal without verified staff.
func callSupportMatrixEntryTool(t *testing.T, available bool, input GetSupportMatrixEntryInput, staff *contextvalues.AdminAuthContext) *mcp.CallToolResult {
	t.Helper()
	server := mcp.NewServer(&mcp.Implementation{Name: "admin-mcp-test", Version: "0.1.0"}, nil)
	registerSupportMatrixEntryTool(server, available)
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true})
	arguments, err := json.Marshal(input)
	require.NoError(t, err)
	request := httptest.NewRequest(http.MethodPost, "/admin-mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"get_support_matrix_entry","arguments":`+string(arguments)+`}}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json, text/event-stream")
	principal := staffPrincipal()
	principal.staff = staff
	ctx := context.WithValue(request.Context(), principalKey{}, principal)
	if staff != nil {
		ctx = contextvalues.SetAdminAuthContext(ctx, staff)
	}
	request = request.WithContext(ctx)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var message struct {
		Result *mcp.CallToolResult `json:"result"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &message))
	require.NotNil(t, message.Result, response.Body.String())
	return message.Result
}

func requireSupportMatrixEntryError(t *testing.T, result *mcp.CallToolResult, message string) {
	t.Helper()
	require.True(t, result.IsError)
	require.Nil(t, result.StructuredContent)
	require.Len(t, result.Content, 1)
	text, ok := result.Content[0].(*mcp.TextContent)
	require.True(t, ok)
	require.Equal(t, message, text.Text)
}

func decodeSupportMatrixEntry(t *testing.T, result *mcp.CallToolResult) SupportMatrixEntryOutput {
	t.Helper()
	require.False(t, result.IsError)
	data, err := json.Marshal(result.StructuredContent)
	require.NoError(t, err)
	var output SupportMatrixEntryOutput
	require.NoError(t, json.Unmarshal(data, &output))
	return output
}

// firstApplicableEntry finds an entry of the embedded matrix that lists every cell.
func firstApplicableEntry(t *testing.T, matrix *supportmatrix.Matrix) (GetSupportMatrixEntryInput, *supportmatrix.PlatformSupport) {
	t.Helper()
	for i := range matrix.Methods {
		method := &matrix.Methods[i]
		for j := range method.Platforms {
			support := &method.Platforms[j]
			if support.Applicability == supportmatrix.Applicable {
				return GetSupportMatrixEntryInput{MethodID: method.ID, PlatformID: support.Platform}, support
			}
		}
	}
	t.Fatal("the embedded matrix has no applicable entry")
	return GetSupportMatrixEntryInput{}, nil
}

func TestGetSupportMatrixEntryReadsTheEmbeddedFile(t *testing.T) {
	t.Parallel()
	matrix, err := supportmatrix.Current()
	require.NoError(t, err)
	input, support := firstApplicableEntry(t, matrix)

	output := decodeSupportMatrixEntry(t, callSupportMatrixEntryTool(t, true, input, verifiedStaffContext()))
	require.Equal(t, input.MethodID, output.Method.ID)
	require.Equal(t, input.PlatformID, output.Platform.ID)
	require.Equal(t, "applicable", output.Applicability)
	require.Equal(t, support.Note, output.Note)
	require.Equal(t, matrix.Revision, output.Revision)
	require.Len(t, output.Cells, len(matrix.Capabilities))
	require.True(t, slices.IsSortedFunc(output.Cells, func(a, b SupportMatrixEntryFact) int { return strings.Compare(a.CapabilityID, b.CapabilityID) }))
	for _, cell := range output.Cells {
		fact := support.Cells[cell.CapabilityID]
		require.Equal(t, SupportMatrixEntryFact{CapabilityID: cell.CapabilityID, Status: string(fact.Status), Note: fact.Note, Verify: fact.Verify}, cell)
	}
	want, err := projectSupportMatrixEntry(matrix, input)
	require.NoError(t, err)
	require.Equal(t, want, output)
}

func TestProjectSupportMatrixEntryReportsEveryField(t *testing.T) {
	t.Parallel()
	matrix := parseSupportMatrixEntryFixture(t)

	output, err := projectSupportMatrixEntry(matrix, GetSupportMatrixEntryInput{MethodID: "hooks", PlatformID: "cli"})
	require.NoError(t, err)
	require.Equal(t, SupportMatrixEntryOutput{
		Method:        SupportMatrixEntryAxis{ID: "hooks", Name: "Hooks", Vendor: "Vendor"},
		Platform:      SupportMatrixEntryAxis{ID: "cli", Name: "CLI", Vendor: "Vendor"},
		Applicability: "applicable",
		Note:          "cost only",
		Accounts:      SupportMatrixAccounts{Personal: "unsupported", Team: "supported", Enterprise: "unknown"},
		Os:            &SupportMatrixEntryOS{Mac: "supported", Windows: "", Linux: "verify"},
		Cells: []SupportMatrixEntryFact{
			{CapabilityID: "cost", Status: "supported", Note: "", Verify: false},
			{CapabilityID: "session", Status: "partial", Note: "sessions only", Verify: true},
		},
		ReferenceFacts: []SupportMatrixEntryFact{
			{CapabilityID: "cost", Status: "partial", Note: "tokens only", Verify: true},
			{CapabilityID: "session", Status: "supported", Note: "via hooks", Verify: false},
		},
		Revision: matrix.Revision,
	}, output)

	// A method that does not apply lists no cells and says nothing per OS.
	output, err = projectSupportMatrixEntry(matrix, GetSupportMatrixEntryInput{MethodID: "hooks", PlatformID: "web"})
	require.NoError(t, err)
	require.Equal(t, "na", output.Applicability)
	require.Empty(t, output.Note)
	require.Nil(t, output.Os)
	require.Empty(t, output.Cells)
	require.Len(t, output.ReferenceFacts, 2)
}

func TestGetSupportMatrixEntryFailsClosed(t *testing.T) {
	t.Parallel()
	matrix, err := supportmatrix.Current()
	require.NoError(t, err)
	input, _ := firstApplicableEntry(t, matrix)
	for name, tc := range map[string]struct {
		available bool
		staff     *contextvalues.AdminAuthContext
	}{
		"missing staff context":  {available: true, staff: nil},
		"mismatched staff email": {available: true, staff: &contextvalues.AdminAuthContext{SessionID: "browser-session", OIDCSubject: "staff-subject", Email: "someone-else@example.test"}},
		"matrix not served":      {available: false, staff: verifiedStaffContext()},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			result := callSupportMatrixEntryTool(t, tc.available, input, tc.staff)
			requireSupportMatrixEntryError(t, result, errSupportMatrixUnavailable.Error())
		})
	}
}

func TestGetSupportMatrixEntryMapsTargetErrors(t *testing.T) {
	t.Parallel()
	matrix, err := supportmatrix.Current()
	require.NoError(t, err)
	input, _ := firstApplicableEntry(t, matrix)
	for name, tc := range map[string]struct {
		input   GetSupportMatrixEntryInput
		message string
	}{
		"missing method":    {GetSupportMatrixEntryInput{MethodID: "", PlatformID: input.PlatformID}, "provide an exact method ID"},
		"oversized method":  {GetSupportMatrixEntryInput{MethodID: strings.Repeat("m", maxSupportMatrixText+1), PlatformID: input.PlatformID}, "provide an exact method ID"},
		"missing platform":  {GetSupportMatrixEntryInput{MethodID: input.MethodID, PlatformID: ""}, "provide an exact platform ID"},
		"unknown method":    {GetSupportMatrixEntryInput{MethodID: "no-such-method", PlatformID: input.PlatformID}, errSupportMatrixEntryNotFound.Error()},
		"unknown platform":  {GetSupportMatrixEntryInput{MethodID: input.MethodID, PlatformID: "no-such-platform"}, errSupportMatrixEntryNotFound.Error()},
		"swapped arguments": {GetSupportMatrixEntryInput{MethodID: input.PlatformID, PlatformID: input.MethodID}, errSupportMatrixEntryNotFound.Error()},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			result := callSupportMatrixEntryTool(t, true, tc.input, verifiedStaffContext())
			requireSupportMatrixEntryError(t, result, tc.message)
		})
	}
}

func TestProjectSupportMatrixEntryFailsClosed(t *testing.T) {
	t.Parallel()
	input := GetSupportMatrixEntryInput{MethodID: "hooks", PlatformID: "cli"}
	_, err := projectSupportMatrixEntry(nil, input)
	require.ErrorIs(t, err, errSupportMatrixUnavailable)

	for name, mutate := range map[string]func(*supportmatrix.Matrix){
		"oversized revision": func(m *supportmatrix.Matrix) { m.Revision = strings.Repeat("r", maxSupportMatrixText+1) },
		"oversized entry note": func(m *supportmatrix.Matrix) {
			method, _ := m.Method("hooks")
			support, _ := method.Support("cli")
			support.Note = strings.Repeat("n", supportmatrix.MaxNoteLength+1)
		},
		"invalid applicability": func(m *supportmatrix.Matrix) {
			method, _ := m.Method("hooks")
			support, _ := method.Support("cli")
			support.Applicability = "sometimes"
		},
		"invalid eligibility": func(m *supportmatrix.Matrix) {
			method, _ := m.Method("hooks")
			support, _ := method.Support("cli")
			support.Accounts.Team = "maybe"
		},
		"invalid operating system": func(m *supportmatrix.Matrix) {
			method, _ := m.Method("hooks")
			support, _ := method.Support("cli")
			support.OS.Mac = "maybe"
		},
		"invalid cell status": func(m *supportmatrix.Matrix) {
			method, _ := m.Method("hooks")
			support, _ := method.Support("cli")
			support.Cells["session"] = supportmatrix.Fact{Status: "invented", Note: "", Verify: false}
		},
		"oversized cell note": func(m *supportmatrix.Matrix) {
			method, _ := m.Method("hooks")
			support, _ := method.Support("cli")
			support.Cells["session"] = supportmatrix.Fact{Status: supportmatrix.StatusSupported, Note: strings.Repeat("n", supportmatrix.MaxNoteLength+1), Verify: false}
		},
		"too many cells": func(m *supportmatrix.Matrix) {
			method, _ := m.Method("hooks")
			support, _ := method.Support("cli")
			for index := range maxSupportMatrixEntries + 1 {
				support.Cells["capability-"+strconv.Itoa(index)] = supportmatrix.Fact{Status: supportmatrix.StatusSupported, Note: "", Verify: false}
			}
		},
		"invalid claim status": func(m *supportmatrix.Matrix) {
			method, _ := m.Method("hooks")
			method.Claims["cost"] = supportmatrix.Fact{Status: "invented", Note: "", Verify: false}
		},
		"too many claims": func(m *supportmatrix.Matrix) {
			method, _ := m.Method("hooks")
			for index := range maxSupportMatrixEntries + 1 {
				method.Claims["capability-"+strconv.Itoa(index)] = supportmatrix.Fact{Status: supportmatrix.StatusSupported, Note: "", Verify: false}
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			matrix := parseSupportMatrixEntryFixture(t)
			mutate(matrix)
			_, err := projectSupportMatrixEntry(matrix, input)
			require.ErrorIs(t, err, errSupportMatrixUnavailable)
		})
	}
}
