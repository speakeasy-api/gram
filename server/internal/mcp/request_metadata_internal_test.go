package mcp

import (
	"encoding/json"
	"maps"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcp/mcpmetrics"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpversions"
	"github.com/speakeasy-api/gram/server/internal/mcpjsonrpc"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// requestMeta20260728 is a complete MCP 2026-07-28 `_meta` object.
var requestMeta20260728 = map[string]any{
	metaProtocolVersionKey:    mcpversions.Version20260728,
	metaClientCapabilitiesKey: map[string]any{},
}

var resolution20260728 = mcpversions.Resolution{
	Declared: mcpversions.Version20260728,
	InEffect: mcpversions.Version20260728,
}

func metadataTestRequest(t *testing.T, method string, params map[string]any) *rawRequest {
	t.Helper()

	if params == nil {
		// A JSON array stands in for params that are not an object.
		return &rawRequest{JSONRPC: "2.0", ID: mcpjsonrpc.NumberID(1), Method: method, Params: json.RawMessage(`["not","an","object"]`)}
	}
	bs, err := json.Marshal(params)
	require.NoError(t, err)
	return &rawRequest{JSONRPC: "2.0", ID: mcpjsonrpc.NumberID(1), Method: method, Params: bs}
}

func metadataTestHeader(pairs ...string) http.Header {
	header := http.Header{}
	for i := 0; i+1 < len(pairs); i += 2 {
		header.Add(pairs[i], pairs[i+1])
	}
	return header
}

func TestValidateRequestMetadata(t *testing.T) {
	t.Parallel()

	toolsCallParams := map[string]any{"name": "get_weather", "arguments": map[string]any{}, "_meta": requestMeta20260728}
	toolsListParams := map[string]any{"_meta": requestMeta20260728}
	resourcesReadParams := map[string]any{"uri": "file:///élève.txt", "_meta": requestMeta20260728}

	tests := []struct {
		name   string
		method string
		params map[string]any
		header http.Header
		// want is the expected error code, or zero when the request passes.
		want oops.MCPCode
	}{
		{
			name:   "valid tools/call",
			method: mcpversions.MethodToolsCall,
			params: toolsCallParams,
			header: metadataTestHeader("MCP-Protocol-Version", "2026-07-28", "Mcp-Method", "tools/call", "Mcp-Name", "get_weather"),
		},
		{
			name:   "valid tools/list without Mcp-Name",
			method: mcpversions.MethodToolsList,
			params: toolsListParams,
			header: metadataTestHeader("MCP-Protocol-Version", "2026-07-28", "Mcp-Method", "tools/list"),
		},
		{
			name:   "valid base64 Mcp-Name for resources/read",
			method: mcpversions.MethodResourcesRead,
			params: resourcesReadParams,
			header: metadataTestHeader("MCP-Protocol-Version", "2026-07-28", "Mcp-Method", "resources/read", "Mcp-Name", "=?base64?ZmlsZTovLy/DqWzDqHZlLnR4dA==?="),
		},
		{
			name:   "optional clientInfo may be absent",
			method: mcpversions.MethodToolsList,
			params: map[string]any{"_meta": map[string]any{metaProtocolVersionKey: "2026-07-28", metaClientCapabilitiesKey: map[string]any{"elicitation": map[string]any{}}}},
			header: metadataTestHeader("MCP-Protocol-Version", "2026-07-28", "Mcp-Method", "tools/list"),
		},
		{
			name:   "missing protocol version header",
			method: mcpversions.MethodToolsList,
			params: toolsListParams,
			header: metadataTestHeader("Mcp-Method", "tools/list"),
			want:   oops.MCPCodeHeaderMismatch,
		},
		{
			name:   "malformed protocol version header",
			method: mcpversions.MethodToolsList,
			params: toolsListParams,
			header: metadataTestHeader("MCP-Protocol-Version", "2026-07-28\x00", "Mcp-Method", "tools/list"),
			want:   oops.MCPCodeHeaderMismatch,
		},
		{
			name:   "missing Mcp-Method",
			method: mcpversions.MethodToolsList,
			params: toolsListParams,
			header: metadataTestHeader("MCP-Protocol-Version", "2026-07-28"),
			want:   oops.MCPCodeHeaderMismatch,
		},
		{
			name:   "Mcp-Method mismatch",
			method: mcpversions.MethodToolsCall,
			params: toolsCallParams,
			header: metadataTestHeader("MCP-Protocol-Version", "2026-07-28", "Mcp-Method", "tools/list", "Mcp-Name", "get_weather"),
			want:   oops.MCPCodeHeaderMismatch,
		},
		{
			name:   "Mcp-Method values are case-sensitive",
			method: mcpversions.MethodToolsList,
			params: toolsListParams,
			header: metadataTestHeader("MCP-Protocol-Version", "2026-07-28", "Mcp-Method", "Tools/List"),
			want:   oops.MCPCodeHeaderMismatch,
		},
		{
			name:   "Mcp-Method cannot use base64 encoding",
			method: mcpversions.MethodToolsList,
			params: toolsListParams,
			header: metadataTestHeader("MCP-Protocol-Version", "2026-07-28", "Mcp-Method", "=?base64?dG9vbHMvbGlzdA==?="),
			want:   oops.MCPCodeHeaderMismatch,
		},
		{
			name:   "protocol version agreement compares the original body value",
			method: mcpversions.MethodToolsList,
			params: map[string]any{"_meta": map[string]any{metaProtocolVersionKey: " 2026-07-28 ", metaClientCapabilitiesKey: map[string]any{}}},
			header: metadataTestHeader("MCP-Protocol-Version", "2026-07-28", "Mcp-Method", "tools/list"),
			want:   oops.MCPCodeHeaderMismatch,
		},
		{
			name:   "null name cannot match an empty Mcp-Name",
			method: mcpversions.MethodToolsCall,
			params: map[string]any{"name": nil, "_meta": requestMeta20260728},
			header: metadataTestHeader("MCP-Protocol-Version", "2026-07-28", "Mcp-Method", "tools/call", "Mcp-Name", ""),
			want:   oops.MCPCodeHeaderMismatch,
		},
		{
			name:   "repeated Mcp-Method",
			method: mcpversions.MethodToolsList,
			params: toolsListParams,
			header: metadataTestHeader("MCP-Protocol-Version", "2026-07-28", "Mcp-Method", "tools/list", "Mcp-Method", "tools/list"),
			want:   oops.MCPCodeHeaderMismatch,
		},
		{
			name:   "missing Mcp-Name on tools/call",
			method: mcpversions.MethodToolsCall,
			params: toolsCallParams,
			header: metadataTestHeader("MCP-Protocol-Version", "2026-07-28", "Mcp-Method", "tools/call"),
			want:   oops.MCPCodeHeaderMismatch,
		},
		{
			name:   "Mcp-Name mismatch",
			method: mcpversions.MethodToolsCall,
			params: toolsCallParams,
			header: metadataTestHeader("MCP-Protocol-Version", "2026-07-28", "Mcp-Method", "tools/call", "Mcp-Name", "delete_everything"),
			want:   oops.MCPCodeHeaderMismatch,
		},
		{
			name:   "Mcp-Name with invalid base64",
			method: mcpversions.MethodToolsCall,
			params: toolsCallParams,
			header: metadataTestHeader("MCP-Protocol-Version", "2026-07-28", "Mcp-Method", "tools/call", "Mcp-Name", "=?base64?SGVsbG8?="),
			want:   oops.MCPCodeHeaderMismatch,
		},
		{
			name:   "Mcp-Name with no body name to compare",
			method: mcpversions.MethodPromptsGet,
			params: toolsListParams,
			header: metadataTestHeader("MCP-Protocol-Version", "2026-07-28", "Mcp-Method", "prompts/get", "Mcp-Name", "greeting"),
			want:   oops.MCPCodeHeaderMismatch,
		},
		{
			name:   "missing _meta",
			method: mcpversions.MethodToolsList,
			params: map[string]any{},
			header: metadataTestHeader("MCP-Protocol-Version", "2026-07-28", "Mcp-Method", "tools/list"),
			want:   oops.MCPCodeInvalidParams,
		},
		{
			name:   "missing _meta protocol version",
			method: mcpversions.MethodToolsList,
			params: map[string]any{"_meta": map[string]any{metaClientCapabilitiesKey: map[string]any{}}},
			header: metadataTestHeader("MCP-Protocol-Version", "2026-07-28", "Mcp-Method", "tools/list"),
			want:   oops.MCPCodeInvalidParams,
		},
		{
			name:   "mistyped _meta protocol version",
			method: mcpversions.MethodToolsList,
			params: map[string]any{"_meta": map[string]any{metaProtocolVersionKey: 20260728, metaClientCapabilitiesKey: map[string]any{}}},
			header: metadataTestHeader("MCP-Protocol-Version", "2026-07-28", "Mcp-Method", "tools/list"),
			want:   oops.MCPCodeInvalidParams,
		},
		{
			name:   "missing client capabilities",
			method: mcpversions.MethodToolsList,
			params: map[string]any{"_meta": map[string]any{metaProtocolVersionKey: "2026-07-28"}},
			header: metadataTestHeader("MCP-Protocol-Version", "2026-07-28", "Mcp-Method", "tools/list"),
			want:   oops.MCPCodeInvalidParams,
		},
		{
			name:   "params not an object",
			method: mcpversions.MethodToolsList,
			params: nil,
			header: metadataTestHeader("MCP-Protocol-Version", "2026-07-28", "Mcp-Method", "tools/list"),
			want:   oops.MCPCodeInvalidParams,
		},
		{
			name:   "client capabilities not an object",
			method: mcpversions.MethodToolsList,
			params: map[string]any{"_meta": map[string]any{metaProtocolVersionKey: "2026-07-28", metaClientCapabilitiesKey: []string{"elicitation"}}},
			header: metadataTestHeader("MCP-Protocol-Version", "2026-07-28", "Mcp-Method", "tools/list"),
			want:   oops.MCPCodeInvalidParams,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := validateRequestMetadata(tc.header, metadataTestRequest(t, tc.method, tc.params), resolution20260728)
			if tc.want == 0 {
				require.NoError(t, err)
				return
			}
			var mcpErr *oops.MCPError
			require.ErrorAs(t, err, &mcpErr)
			require.Equal(t, tc.want, mcpErr.Code)
		})
	}
}

func TestValidateRequestMetadata_SkipsRevisionsBefore20260728(t *testing.T) {
	t.Parallel()

	req := metadataTestRequest(t, mcpversions.MethodToolsCall, map[string]any{"name": "get_weather"})
	resolution := mcpversions.Resolution{Declared: mcpversions.Version20251125, InEffect: mcpversions.Version20251125}

	require.NoError(t, validateRequestMetadata(http.Header{}, req, resolution))
}

func TestValidateRequestMetadata_SkipsNotifications(t *testing.T) {
	t.Parallel()

	req := &rawRequest{JSONRPC: "2.0", ID: mcpjsonrpc.ID{}, Method: mcpversions.MethodNotificationsCancelled, Params: nil}

	require.NoError(t, validateRequestMetadata(http.Header{}, req, resolution20260728))
}

func TestValidateRequestMetadata_DoesNotEchoHeaderValues(t *testing.T) {
	t.Parallel()

	req := metadataTestRequest(t, mcpversions.MethodToolsCall, map[string]any{"name": "get_weather", "_meta": requestMeta20260728})
	header := metadataTestHeader("MCP-Protocol-Version", "2026-07-28", "Mcp-Method", "tools/call", "Mcp-Name", "<script>hostile</script>")

	err := validateRequestMetadata(header, req, resolution20260728)
	require.Error(t, err)
	require.NotContains(t, err.Error(), "hostile")
}

func TestValidateSupportedProtocolVersion_ConflictPrecedesUnsupportedVersion(t *testing.T) {
	t.Parallel()

	// A supported header and an unsupported `_meta` declaration disagree:
	// MCP 2026-07-28 orders HeaderMismatch ahead of
	// UnsupportedProtocolVersionError.
	supported := append(mcpversions.SupportedHostedToolset(), mcpversions.Version20260728)
	req := metadataTestRequest(t, mcpversions.MethodToolsList, map[string]any{"_meta": map[string]any{metaProtocolVersionKey: "v999.0.0"}})
	resolution := mcpversions.Resolve(mcpversions.Version20260728, supported)

	err := validateSupportedProtocolVersion(req, resolution, supported)
	var mcpErr *oops.MCPError
	require.ErrorAs(t, err, &mcpErr)
	require.Equal(t, oops.MCPCodeHeaderMismatch, mcpErr.Code)
}

func TestValidateRequestMetadata_NullParamsCannotVerifyMcpName(t *testing.T) {
	t.Parallel()

	req := &rawRequest{JSONRPC: "2.0", ID: mcpjsonrpc.NumberID(1), Method: mcpversions.MethodToolsCall, Params: json.RawMessage(`null`)}
	header := metadataTestHeader("MCP-Protocol-Version", "2026-07-28", "Mcp-Method", "tools/call", "Mcp-Name", "get_weather")

	err := validateRequestMetadata(header, req, resolution20260728)
	var mcpErr *oops.MCPError
	require.ErrorAs(t, err, &mcpErr)
	require.Equal(t, oops.MCPCodeHeaderMismatch, mcpErr.Code)
}

func TestValidateMetaDeclaredProtocolVersion_GoverningRevision(t *testing.T) {
	t.Parallel()

	mistypedMeta := json.RawMessage(`{"_meta":{"io.modelcontextprotocol/protocolVersion":20260728}}`)
	tests := []struct {
		name     string
		header   string
		params   json.RawMessage
		wantCode oops.MCPCode
		wantRev  string
	}{
		{name: "malformed header, recognized metadata", header: "2025-11-25\x00", params: json.RawMessage(`{"_meta":{"io.modelcontextprotocol/protocolVersion":"2025-06-18"}}`), wantCode: oops.MCPCodeHeaderMismatch, wantRev: mcpversions.Version20250618},
		{name: "malformed header, unrecognized metadata", header: "2025-11-25\x00", params: json.RawMessage(`{"_meta":{"io.modelcontextprotocol/protocolVersion":"2025-12-01"}}`), wantCode: oops.MCPCodeHeaderMismatch, wantRev: mcpversions.Latest()},
		{name: "malformed metadata, recognized header", header: mcpversions.Version20251125, params: mistypedMeta, wantCode: oops.MCPCodeInvalidParams, wantRev: mcpversions.Version20251125},
		{name: "malformed metadata, unrecognized header", header: "foo", params: mistypedMeta, wantCode: oops.MCPCodeInvalidParams, wantRev: mcpversions.Latest()},
		{name: "malformed metadata, no header", header: "", params: mistypedMeta, wantCode: oops.MCPCodeInvalidParams, wantRev: mcpversions.Latest()},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			req := &rawRequest{JSONRPC: "2.0", ID: mcpjsonrpc.NumberID(1), Method: mcpversions.MethodToolsList, Params: tc.params}
			err := validateMetaDeclaredProtocolVersion(req, tc.header)

			var declErr *declarationError
			require.ErrorAs(t, err, &declErr)
			require.Equal(t, tc.wantRev, declErr.revision)
			require.Equal(t, tc.wantCode, declErr.err.Code)
		})
	}
}

// prepareTestRequest runs a POST body through the hosted gate.
func prepareTestRequest(t *testing.T, body string, header http.Header, supported []string) (*httptest.ResponseRecorder, bool) {
	t.Helper()

	r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/mcp/example", strings.NewReader(body))
	maps.Copy(r.Header, header)
	w := httptest.NewRecorder()
	s := &Service{metrics: mcpmetrics.NewMetrics(testenv.NewMeterProvider(t).Meter("test"), testenv.NewLogger(t))}

	_, handled, err := s.prepareTerminatedMCPRequest(w, r, testenv.NewLogger(t), 1<<20, supported, mcpmetrics.SurfaceHosting)
	require.NoError(t, err)
	return w, handled
}

func TestPrepareTerminatedMCPRequest_ConflictingDeclarationsAnswer400(t *testing.T) {
	t.Parallel()

	body := `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"2025-06-18"}}}`
	w, handled := prepareTestRequest(t, body, metadataTestHeader("MCP-Protocol-Version", "2025-11-25"), mcpversions.SupportedHostedToolset())

	require.True(t, handled)
	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Contains(t, w.Body.String(), `"code":-32020`)
}

func TestPrepareTerminatedMCPRequest_ConflictingNotificationIsAcknowledged(t *testing.T) {
	t.Parallel()

	body := `{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"2025-06-18"}}}`
	w, handled := prepareTestRequest(t, body, metadataTestHeader("MCP-Protocol-Version", "2025-11-25"), mcpversions.SupportedHostedToolset())

	require.True(t, handled)
	require.Equal(t, http.StatusAccepted, w.Code)
	require.Empty(t, w.Body.String())
}

func TestPrepareTerminatedMCPRequest_ValidatesRequestMetadataWhen20260728IsSupported(t *testing.T) {
	t.Parallel()

	supported := append(mcpversions.SupportedHostedToolset(), mcpversions.Version20260728)
	body := `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}`

	w, handled := prepareTestRequest(t, body, metadataTestHeader("MCP-Protocol-Version", "2026-07-28"), supported)
	require.True(t, handled, "a missing Mcp-Method header must be rejected")
	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Contains(t, w.Body.String(), `"code":-32020`)

	w, handled = prepareTestRequest(t, body, metadataTestHeader("MCP-Protocol-Version", "2026-07-28", "Mcp-Method", "tools/list"), supported)
	require.False(t, handled, "a complete 2026-07-28 request passes the gate")
	require.Equal(t, http.StatusOK, w.Code, "nothing is written for a request that passes")
}

func TestPrepareTerminatedMCPRequest_SkipsRequestMetadataBefore20260728(t *testing.T) {
	t.Parallel()

	body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"get_weather"}}`
	w, handled := prepareTestRequest(t, body, metadataTestHeader("MCP-Protocol-Version", "2025-11-25"), mcpversions.SupportedHostedToolset())

	require.False(t, handled)
	require.Equal(t, http.StatusOK, w.Code)
}
