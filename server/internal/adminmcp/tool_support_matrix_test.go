package adminmcp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
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
	return callSupportMatrixToolAs(t, reader, &contextvalues.AdminAuthContext{SessionID: "browser-session", OIDCSubject: "staff-subject", Email: staffPrincipal().Email})
}

// callSupportMatrixToolAs attaches staff to the principal and request context; nil models a principal without verified staff.
func callSupportMatrixToolAs(t *testing.T, reader SupportMatrixReader, staff *contextvalues.AdminAuthContext) (int, string, json.RawMessage) {
	t.Helper()
	server := mcp.NewServer(&mcp.Implementation{Name: "admin-mcp-test", Version: "0.1.0"}, nil)
	registerSupportMatrixTools(server, reader)
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true})
	request := httptest.NewRequest(http.MethodPost, "/admin-mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"get_support_matrix","arguments":{}}}`))
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
		Capabilities: []*gen.SupportCapability{{ID: "capability-a", Name: "Capability A", Group: "Group A"}},
		Platforms:    []*gen.SupportPlatform{{ID: "product-a", Name: "Product A", Vendor: "Vendor B", Family: "agent", Surface: "desktop"}},
		Methods: []*gen.SupportMethod{{
			ID: "method-a", Name: "Method A", Vendor: "Vendor A", Plans: "operator plan notes",
			Claims: map[string]*gen.SupportFact{"capability-a": {Status: "partial", Note: "private reference note", Verify: true}},
			Platforms: []*gen.SupportPlatformSupport{{
				Platform: "product-a", Applicability: "applicable",
				Accounts: &gen.SupportAccounts{Personal: "unsupported", Team: "supported", Enterprise: "unknown"},
				Os:       &gen.SupportOS{Mac: new("supported")}, Note: "private operator condition",
				Cells: map[string]*gen.SupportFact{"capability-a": {Status: "supported", Note: "private coverage note", Verify: true}},
			}},
		}},
		Revision: "private revision",
	}
}

//go:fix inline
func supportMatrixFacts(count int) map[string]*gen.SupportFact {
	facts := make(map[string]*gen.SupportFact, count)
	for index := range count {
		facts["capability-"+strconv.Itoa(index)] = &gen.SupportFact{Status: "supported"}
	}
	return facts
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
	require.Equal(t, []SupportMatrixMapping{{
		MethodID: "method-a", ProductID: "product-a", Applicability: "applicable",
		Accounts: SupportMatrixAccounts{Personal: "unsupported", Team: "supported", Enterprise: "unknown"},
		Facts:    []SupportMatrixFact{{CapabilityID: "capability-a", Status: "supported"}},
	}}, output.Mappings)
	require.Equal(t, []SupportMatrixReference{{MethodID: "method-a", Facts: []SupportMatrixFact{{CapabilityID: "capability-a", Status: "partial"}}}}, output.References)
	for _, omitted := range []string{"operator plan notes", "private operator condition", "private coverage note", "private reference note", "private revision", `"note"`, `"os"`, `"verify"`, `"plans"`, `"revision"`} {
		require.NotContains(t, body, omitted)
	}
}

func TestGetSupportMatrixOrdersMapFactsDeterministically(t *testing.T) {
	t.Parallel()
	matrix := supportMatrixFixture()
	accounts := &gen.SupportAccounts{Personal: "supported", Team: "supported", Enterprise: "supported"}
	matrix.Methods[0].Platforms = append(matrix.Methods[0].Platforms, &gen.SupportPlatformSupport{
		Platform: "product-b", Applicability: "unknown", Accounts: accounts,
		Cells: map[string]*gen.SupportFact{"capability-b": {Status: "partial"}, "capability-a": {Status: "na"}},
	})
	matrix.Methods = append(matrix.Methods, &gen.SupportMethod{ID: "method-b", Name: "Method B", Vendor: "Vendor A",
		Claims:    map[string]*gen.SupportFact{"capability-c": {Status: "supported"}, "capability-b": {Status: "impossible"}, "capability-a": {Status: "unknown"}},
		Platforms: []*gen.SupportPlatformSupport{{Platform: "product-b", Applicability: "na", Accounts: accounts, Cells: map[string]*gen.SupportFact{}}},
	})
	matrix.Platforms = append(matrix.Platforms, &gen.SupportPlatform{ID: "product-b", Name: "Product B", Vendor: "Vendor B", Family: "agent", Surface: "cli"})
	matrix.Capabilities = append(matrix.Capabilities, &gen.SupportCapability{ID: "capability-b", Name: "Capability B", Group: "Group A"})

	all := SupportMatrixAccounts{Personal: "supported", Team: "supported", Enterprise: "supported"}
	want := GetSupportMatrixOutput{
		Methods: []SupportMatrixMethod{
			{ID: "method-a", Name: "Method A", Vendor: "Vendor A", Facts: []SupportMatrixFact{{CapabilityID: "capability-a", Status: "partial"}}},
			{ID: "method-b", Name: "Method B", Vendor: "Vendor A", Facts: []SupportMatrixFact{
				{CapabilityID: "capability-a", Status: "unknown"}, {CapabilityID: "capability-b", Status: "impossible"}, {CapabilityID: "capability-c", Status: "supported"},
			}},
		},
		Products: []SupportMatrixProduct{
			{ID: "product-a", Name: "Product A", Vendor: "Vendor B", Family: "agent", Surface: "desktop"},
			{ID: "product-b", Name: "Product B", Vendor: "Vendor B", Family: "agent", Surface: "cli"},
		},
		Capabilities: []SupportMatrixCapability{
			{ID: "capability-a", Name: "Capability A", Group: "Group A"},
			{ID: "capability-b", Name: "Capability B", Group: "Group A"},
		},
		Mappings: []SupportMatrixMapping{
			{MethodID: "method-a", ProductID: "product-a", Applicability: "applicable", Accounts: SupportMatrixAccounts{Personal: "unsupported", Team: "supported", Enterprise: "unknown"}, Facts: []SupportMatrixFact{{CapabilityID: "capability-a", Status: "supported"}}},
			{MethodID: "method-a", ProductID: "product-b", Applicability: "unknown", Accounts: all, Facts: []SupportMatrixFact{{CapabilityID: "capability-a", Status: "na"}, {CapabilityID: "capability-b", Status: "partial"}}},
			{MethodID: "method-b", ProductID: "product-b", Applicability: "na", Accounts: all, Facts: []SupportMatrixFact{}},
		},
		References: []SupportMatrixReference{
			{MethodID: "method-a", Facts: []SupportMatrixFact{{CapabilityID: "capability-a", Status: "partial"}}},
			{MethodID: "method-b", Facts: []SupportMatrixFact{{CapabilityID: "capability-a", Status: "unknown"}, {CapabilityID: "capability-b", Status: "impossible"}, {CapabilityID: "capability-c", Status: "supported"}}},
		},
	}
	// Map iteration is randomised, so repeated calls catch any unsorted projection.
	for range 10 {
		_, body, data := callSupportMatrixTool(t, &recordingSupportMatrixReader{result: matrix})
		var output GetSupportMatrixOutput
		require.NoError(t, json.Unmarshal(data, &output), body)
		require.Equal(t, want, output)
	}
}

func TestGetSupportMatrixRequiresVerifiedStaff(t *testing.T) {
	t.Parallel()
	for name, staff := range map[string]*contextvalues.AdminAuthContext{
		"missing staff context":  nil,
		"mismatched staff email": {SessionID: "browser-session", OIDCSubject: "staff-subject", Email: "someone-else@example.test"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			reads := &recordingSupportMatrixReader{result: supportMatrixFixture()}
			_, body, _ := callSupportMatrixToolAs(t, reads, staff)
			require.Contains(t, body, `"isError":true`)
			require.Nil(t, reads.input)
		})
	}
}

func TestGetSupportMatrixFailsClosed(t *testing.T) {
	t.Parallel()
	oversizedPlatforms := make([]*gen.SupportPlatformSupport, maxSupportMatrixEntries+1)
	for _, tc := range []struct {
		name   string
		reader SupportMatrixReader
	}{
		{name: "missing reader"},
		{name: "error", reader: &recordingSupportMatrixReader{err: errors.New("private database failure")}},
		{name: "nil result", reader: &recordingSupportMatrixReader{}},
		{name: "oversized methods", reader: &recordingSupportMatrixReader{result: &gen.SupportMatrix{Methods: make([]*gen.SupportMethod, maxSupportMatrixEntries+1)}}},
		{name: "oversized products", reader: &recordingSupportMatrixReader{result: &gen.SupportMatrix{Platforms: make([]*gen.SupportPlatform, maxSupportMatrixEntries+1)}}},
		{name: "oversized capabilities", reader: &recordingSupportMatrixReader{result: &gen.SupportMatrix{Capabilities: make([]*gen.SupportCapability, maxSupportMatrixEntries+1)}}},
		{name: "oversized platforms on a method", reader: &recordingSupportMatrixReader{result: func() *gen.SupportMatrix {
			matrix := supportMatrixFixture()
			matrix.Methods[0].Platforms = oversizedPlatforms
			return matrix
		}()}},
		{name: "too many mappings", reader: &recordingSupportMatrixReader{result: func() *gen.SupportMatrix {
			matrix := supportMatrixFixture()
			accounts := &gen.SupportAccounts{Personal: "supported", Team: "supported", Enterprise: "supported"}
			// Eleven methods on two hundred platforms each: every entry is within
			// bounds on its own, the mapping count is not.
			matrix.Methods = nil
			for method := range 11 {
				platforms := make([]*gen.SupportPlatformSupport, 0, maxSupportMatrixEntries)
				for platform := range maxSupportMatrixEntries {
					platforms = append(platforms, &gen.SupportPlatformSupport{Platform: "product-" + strconv.Itoa(platform), Applicability: "na", Accounts: accounts, Cells: map[string]*gen.SupportFact{}})
				}
				matrix.Methods = append(matrix.Methods, &gen.SupportMethod{ID: "method-" + strconv.Itoa(method), Name: "Method", Vendor: "Vendor", Claims: map[string]*gen.SupportFact{}, Platforms: platforms})
			}
			return matrix
		}()}},
		{name: "too many total facts", reader: &recordingSupportMatrixReader{result: func() *gen.SupportMatrix {
			matrix := supportMatrixFixture()
			// Claims count twice (method and reference), so half the cap on their own pushes the total over it with the cell.
			matrix.Methods[0].Claims = supportMatrixFacts(maxSupportMatrixFacts / 2)
			return matrix
		}()}},
		{name: "invalid claim", reader: &recordingSupportMatrixReader{result: func() *gen.SupportMatrix {
			matrix := supportMatrixFixture()
			matrix.Methods[0].Claims["capability-a"].Status = "invented"
			return matrix
		}()}},
		{name: "invalid cell", reader: &recordingSupportMatrixReader{result: func() *gen.SupportMatrix {
			matrix := supportMatrixFixture()
			matrix.Methods[0].Platforms[0].Cells["capability-a"].Status = "invented"
			return matrix
		}()}},
		{name: "invalid applicability", reader: &recordingSupportMatrixReader{result: func() *gen.SupportMatrix {
			matrix := supportMatrixFixture()
			matrix.Methods[0].Platforms[0].Applicability = "sometimes"
			return matrix
		}()}},
		{name: "invalid eligibility", reader: &recordingSupportMatrixReader{result: func() *gen.SupportMatrix {
			matrix := supportMatrixFixture()
			matrix.Methods[0].Platforms[0].Accounts.Team = "maybe"
			return matrix
		}()}},
		{name: "missing accounts", reader: &recordingSupportMatrixReader{result: func() *gen.SupportMatrix {
			matrix := supportMatrixFixture()
			matrix.Methods[0].Platforms[0].Accounts = nil
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
