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

func callSupportMatrixEntryTool(t *testing.T, reader SupportMatrixReader, input GetSupportMatrixEntryInput, verified bool) *mcp.CallToolResult {
	t.Helper()
	server := mcp.NewServer(&mcp.Implementation{Name: "admin-mcp-test", Version: "0.1.0"}, nil)
	registerSupportMatrixEntryTool(server, reader)
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true})
	arguments, err := json.Marshal(input)
	require.NoError(t, err)
	request := httptest.NewRequest(http.MethodPost, "/admin-mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"get_support_matrix_entry","arguments":`+string(arguments)+`}}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json, text/event-stream")
	principal := staffPrincipal()
	principal.staff = nil
	ctx := request.Context()
	if verified {
		staff := &contextvalues.AdminAuthContext{SessionID: "browser-session", OIDCSubject: "staff-subject", Email: principal.Email}
		principal.staff = staff
		ctx = contextvalues.SetAdminAuthContext(ctx, staff)
	}
	request = request.WithContext(context.WithValue(ctx, principalKey{}, principal))
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

func TestGetSupportMatrixEntryProjectsEditableFields(t *testing.T) {
	t.Parallel()
	for _, input := range []GetSupportMatrixEntryInput{
		{Kind: "mapping", MethodID: "method-a", ProductID: "product-a"},
		{Kind: "reference", MethodID: "method-a"},
	} {
		t.Run(input.Kind, func(t *testing.T) {
			t.Parallel()
			matrix := supportMatrixFixture()
			reader := &recordingSupportMatrixReader{result: matrix}
			result := callSupportMatrixEntryTool(t, reader, input, true)
			require.False(t, result.IsError)
			require.NotNil(t, reader.input)
			require.Nil(t, reader.input.AdminSessionToken)
			data, err := json.Marshal(result.StructuredContent)
			require.NoError(t, err)
			var output SupportMatrixEntryOutput
			require.NoError(t, json.Unmarshal(data, &output))
			want, err := projectSupportMatrixEntry(matrix, input)
			require.NoError(t, err)
			require.Equal(t, want, output)
			require.NotContains(t, string(data), "operator plan notes")
		})
	}
}

func TestGetSupportMatrixEntryFailsClosed(t *testing.T) {
	t.Parallel()
	input := GetSupportMatrixEntryInput{Kind: "mapping", MethodID: "method-a", ProductID: "product-a"}
	t.Run("unverified caller", func(t *testing.T) {
		t.Parallel()
		reader := &recordingSupportMatrixReader{result: supportMatrixFixture()}
		result := callSupportMatrixEntryTool(t, reader, input, false)
		require.True(t, result.IsError)
		require.Nil(t, reader.input)
		requireSupportMatrixEntryError(t, result, errSupportMatrixUnavailable.Error())
	})
	t.Run("nil reader", func(t *testing.T) {
		t.Parallel()
		result := callSupportMatrixEntryTool(t, nil, input, true)
		require.True(t, result.IsError)
		requireSupportMatrixEntryError(t, result, errSupportMatrixUnavailable.Error())
	})
	for _, tc := range []struct {
		name   string
		mutate func(*gen.SupportMatrix)
	}{
		{"nil draft", func(m *gen.SupportMatrix) { m.Draft = nil }},
		{"nil method", func(m *gen.SupportMatrix) { m.Methods[0] = nil }},
		{"nil product", func(m *gen.SupportMatrix) { m.Products[0] = nil }},
		{"nil capability", func(m *gen.SupportMatrix) { m.Capabilities[0] = nil }},
		{"malformed catalog text", func(m *gen.SupportMatrix) { m.Methods[0].ID = strings.Repeat("x", maxSupportMatrixText+1) }},
		{"too many methods", func(m *gen.SupportMatrix) { m.Methods = make([]*gen.SupportMethod, maxSupportMatrixEntries+1) }},
		{"too many products", func(m *gen.SupportMatrix) { m.Products = make([]*gen.SupportPlatform, maxSupportMatrixEntries+1) }},
		{"too many capabilities", func(m *gen.SupportMatrix) { m.Capabilities = make([]*gen.SupportCapability, maxSupportMatrixEntries+1) }},
		{"too many mappings", func(m *gen.SupportMatrix) {
			m.Draft.Mappings = make(map[string]*gen.SupportMapping)
			for id := range supportMatrixFacts(maxSupportMatrixFacts + 1) {
				m.Draft.Mappings[id] = nil
			}
		}},
		{"too many references", func(m *gen.SupportMatrix) {
			m.Draft.References = make(map[string]map[string]*gen.SupportFact)
			for id := range supportMatrixFacts(maxSupportMatrixEntries + 1) {
				m.Draft.References[id] = nil
			}
		}},
		{"too many method facts", func(m *gen.SupportMatrix) { m.Methods[0].Facts = supportMatrixFacts(maxSupportMatrixFacts + 1) }},
		{"too many mapping facts", func(m *gen.SupportMatrix) {
			m.Draft.Mappings["method-a/product-a"].Facts = supportMatrixFacts(maxSupportMatrixFacts + 1)
		}},
		{"too many reference facts", func(m *gen.SupportMatrix) {
			m.Draft.References["method-a"] = supportMatrixFacts(maxSupportMatrixFacts + 1)
		}},
		{"too many total facts", func(m *gen.SupportMatrix) {
			m.Methods[0].Facts = supportMatrixFacts(maxSupportMatrixFacts / 2)
			m.Draft.Mappings["method-a/product-a"].Facts = supportMatrixFacts(maxSupportMatrixFacts / 2)
		}},
		{"nil mapping", func(m *gen.SupportMatrix) { m.Draft.Mappings["method-a/product-a"] = nil }},
		{"invalid draft", func(m *gen.SupportMatrix) { m.Draft.Mappings["method-a/product-a"].Applicability = "invalid" }},
		{"unknown capability", func(m *gen.SupportMatrix) { m.Capabilities = nil }},
		{"oversized conditions", func(m *gen.SupportMatrix) {
			m.Draft.Mappings["method-a/product-a"].Conditions = strings.Repeat("c", maxSupportMatrixText+1)
		}},
		{"oversized note", func(m *gen.SupportMatrix) {
			m.Draft.Mappings["method-a/product-a"].Facts["capability-a"].Note = strings.Repeat("n", maxSupportMatrixText+1)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			matrix := supportMatrixFixture()
			tc.mutate(matrix)
			result := callSupportMatrixEntryTool(t, &recordingSupportMatrixReader{result: matrix}, input, true)
			require.True(t, result.IsError)
			require.Nil(t, result.StructuredContent)
			requireSupportMatrixEntryError(t, result, errSupportMatrixUnavailable.Error())
		})
	}
	for _, tc := range []struct {
		name   string
		reader *recordingSupportMatrixReader
	}{
		{"nil matrix", &recordingSupportMatrixReader{}},
		{"reader error", &recordingSupportMatrixReader{err: errors.New("private backend details")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			result := callSupportMatrixEntryTool(t, tc.reader, input, true)
			require.True(t, result.IsError)
			requireSupportMatrixEntryError(t, result, errSupportMatrixUnavailable.Error())
		})
	}
}

func TestGetSupportMatrixEntryMapsTargetErrors(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		input   GetSupportMatrixEntryInput
		message string
	}{
		{GetSupportMatrixEntryInput{Kind: "invalid", MethodID: "method-a"}, "kind must be mapping or reference"},
		{GetSupportMatrixEntryInput{Kind: "reference", MethodID: "method-a", ProductID: "product-a"}, "references do not have a product ID"},
		{GetSupportMatrixEntryInput{Kind: "mapping", MethodID: "method-a", ProductID: "missing"}, "support matrix entry not found"},
	} {
		t.Run(tc.message, func(t *testing.T) {
			t.Parallel()
			result := callSupportMatrixEntryTool(t, &recordingSupportMatrixReader{result: supportMatrixFixture()}, tc.input, true)
			require.True(t, result.IsError)
			requireSupportMatrixEntryError(t, result, tc.message)
		})
	}
}

func TestProjectSupportMatrixEntryReturnsBoundedEditableFields(t *testing.T) {
	t.Parallel()
	matrix := supportMatrixFixture()
	mapping, err := projectSupportMatrixEntry(matrix, GetSupportMatrixEntryInput{Kind: "mapping", MethodID: "method-a", ProductID: "product-a"})
	require.NoError(t, err)
	require.Equal(t, "private operator condition", mapping.Conditions)
	require.Equal(t, []SupportMatrixEntryFact{{CapabilityID: "capability-a", Status: "supported", Note: "private coverage note", Verify: true}}, mapping.Facts)
	require.Equal(t, "private revision", mapping.Revision)

	reference, err := projectSupportMatrixEntry(matrix, GetSupportMatrixEntryInput{Kind: "reference", MethodID: "method-a"})
	require.NoError(t, err)
	require.Equal(t, []SupportMatrixEntryFact{{CapabilityID: "capability-a", Status: "partial", Note: "private reference note", Verify: true}}, reference.Facts)

	_, err = projectSupportMatrixEntry(matrix, GetSupportMatrixEntryInput{Kind: "mapping", MethodID: "method-a", ProductID: "missing"})
	require.ErrorContains(t, err, "not found")
	err = validateSupportMatrixEntryTarget(GetSupportMatrixEntryInput{Kind: "reference", MethodID: "method-a", ProductID: "product-a"})
	require.Error(t, err)
}

func TestSupportMatrixProjectionRejectsUnboundedNotes(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		kind   string
		mutate func(*gen.SupportMatrix)
	}{
		{"conditions", "mapping", func(m *gen.SupportMatrix) {
			m.Draft.Mappings["method-a/product-a"].Conditions = strings.Repeat("c", maxSupportMatrixText+1)
		}},
		{"mapping note", "mapping", func(m *gen.SupportMatrix) {
			m.Draft.Mappings["method-a/product-a"].Facts["capability-a"].Note = strings.Repeat("n", maxSupportMatrixText+1)
		}},
		{"reference note", "reference", func(m *gen.SupportMatrix) {
			m.Draft.References["method-a"]["capability-a"].Note = strings.Repeat("n", maxSupportMatrixText+1)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			matrix := supportMatrixFixture()
			tc.mutate(matrix)
			input := GetSupportMatrixEntryInput{Kind: tc.kind, MethodID: "method-a"}
			if tc.kind == "mapping" {
				input.ProductID = "product-a"
			}
			_, err := projectSupportMatrixEntry(matrix, input)
			require.ErrorIs(t, err, errSupportMatrixUnavailable)
		})
	}
}
