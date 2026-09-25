package adminmcp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
)

type recordingSupportMatrixReader struct {
	input  *gen.GetSupportMatrixPayload
	result *gen.SupportMatrix
	err    error
}

func (r *recordingSupportMatrixReader) GetSupportMatrix(_ context.Context, input *gen.GetSupportMatrixPayload) (*gen.SupportMatrix, error) {
	r.input = input
	return r.result, r.err
}

func callSupportMatrixTool(t *testing.T, reader SupportMatrixReader) (int, string, json.RawMessage) {
	t.Helper()
	server := mcp.NewServer(&mcp.Implementation{Name: "admin-mcp-test", Version: "0.1.0"}, nil)
	registerSupportMatrixTools(server, reader)
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true})
	request := httptest.NewRequest(http.MethodPost, "/admin-mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"get_support_matrix","arguments":{}}}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json, text/event-stream")
	staff := &contextvalues.AdminAuthContext{SessionID: "browser-session", OIDCSubject: "staff-subject", Email: "staff@example.test"}
	principal := staffPrincipal()
	principal.staff = staff
	request = request.WithContext(contextvalues.SetAdminAuthContext(context.WithValue(request.Context(), principalKey{}, principal), staff))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	var message struct {
		Result struct {
			StructuredContent json.RawMessage `json:"structuredContent"`
		} `json:"result"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &message))
	return response.Code, response.Body.String(), message.Result.StructuredContent
}

func supportMatrixFixture() *gen.SupportMatrix {
	return &gen.SupportMatrix{
		Methods: []*gen.SupportMethod{{
			ID: "method-a", Name: "Method A", Vendor: "Vendor A", Plans: "operator plan notes",
			Facts: map[string]*gen.SupportFact{"capability-a": {Status: "partial", Note: "private reference note", Verify: true}},
		}},
		Products:     []*gen.SupportPlatform{{ID: "product-a", Name: "Product A", Vendor: "Vendor B", Family: "agent", Surface: "desktop"}},
		Capabilities: []*gen.SupportCapability{{ID: "capability-a", Name: "Capability A", Group: "Group A"}},
		Draft: &gen.SupportDraft{
			Mappings: map[string]*gen.SupportMapping{"method-a/product-a": {
				Applicability: "applicable", Conditions: "private operator condition",
				Facts: map[string]*gen.SupportFact{"capability-a": {Status: "supported", Note: "private coverage note", Verify: true}},
			}},
			References: map[string]map[string]*gen.SupportFact{"method-a": {
				"capability-a": {Status: "partial", Note: "private reference note", Verify: true},
			}},
		},
		Revision: "private revision",
	}
}

func TestGetSupportMatrixProjectsBoundedFactsOnly(t *testing.T) {
	t.Parallel()
	reads := &recordingSupportMatrixReader{result: supportMatrixFixture()}
	status, body, data := callSupportMatrixTool(t, reads)
	require.Equal(t, http.StatusOK, status, body)
	require.NotNil(t, reads.input)
	require.Nil(t, reads.input.AdminSessionToken)

	var output GetSupportMatrixOutput
	require.NotEmpty(t, data, body)
	require.NoError(t, json.Unmarshal(data, &output))
	require.Len(t, output.Methods, 1)
	require.Equal(t, []SupportMatrixFact{{CapabilityID: "capability-a", Status: "partial"}}, output.Methods[0].Facts)
	require.Equal(t, []SupportMatrixMapping{{MethodID: "method-a", ProductID: "product-a", Applicability: "applicable", Facts: []SupportMatrixFact{{CapabilityID: "capability-a", Status: "supported"}}}}, output.Mappings)
	require.Equal(t, []SupportMatrixReference{{MethodID: "method-a", Facts: []SupportMatrixFact{{CapabilityID: "capability-a", Status: "partial"}}}}, output.References)
	for _, omitted := range []string{"operator plan notes", "private operator condition", "private coverage note", "private reference note", "private revision", `"note"`, `"conditions"`, `"verify"`, `"plans"`, `"revision"`} {
		require.NotContains(t, body, omitted)
	}
}

func TestGetSupportMatrixFailsClosed(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		reader SupportMatrixReader
	}{
		{name: "missing reader"},
		{name: "error", reader: &recordingSupportMatrixReader{err: errors.New("private database failure")}},
		{name: "nil result", reader: &recordingSupportMatrixReader{}},
		{name: "nil draft", reader: &recordingSupportMatrixReader{result: &gen.SupportMatrix{}}},
		{name: "oversized catalog", reader: &recordingSupportMatrixReader{result: &gen.SupportMatrix{Draft: &gen.SupportDraft{}, Methods: make([]*gen.SupportMethod, maxSupportMatrixEntries+1)}}},
		{name: "invalid fact", reader: &recordingSupportMatrixReader{result: func() *gen.SupportMatrix {
			matrix := supportMatrixFixture()
			matrix.Draft.References["method-a"]["capability-a"].Status = "invented"
			return matrix
		}()}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, body, _ := callSupportMatrixTool(t, tc.reader)
			require.Contains(t, body, `"isError":true`)
			require.NotContains(t, body, "private database failure")
		})
	}
}
