package externalmcp

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
)

// newListClient connects to a server whose tools/list answers come from list.
func newListClient(t *testing.T, list func() any) *Client {
	t.Helper()
	server := limitServer{respond: func(w http.ResponseWriter, method string, id json.RawMessage) bool {
		if method != "tools/list" {
			return false
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": id, "result": list()})
		return true
	}}
	c, err := newLimitClient(t, t.Context(), server, nil)
	require.NoError(t, err)
	return c
}

func TestListToolsSkipsOversizedSchema(t *testing.T) {
	t.Parallel()
	const plainSchema = `{"type":"object","properties":{"owner":{"type":"string"}}}`
	oversized := `{"description":"` + strings.Repeat("x", maxToolSchemaBytes) + `"}`
	c := newListClient(t, func() any {
		return map[string]any{"tools": []any{
			map[string]any{"name": "before", "inputSchema": json.RawMessage(plainSchema)},
			map[string]any{"name": "oversized", "inputSchema": json.RawMessage(oversized)},
			map[string]any{"name": "after", "inputSchema": json.RawMessage(plainSchema)},
		}}
	})
	tools, err := c.ListTools(t.Context())
	require.NoError(t, err)
	require.Len(t, tools, 2)
	require.Equal(t, "before", tools[0].Name)
	require.Equal(t, "after", tools[1].Name)
	require.JSONEq(t, plainSchema, string(tools[0].Schema))
}

func TestListToolsAggregateBounds(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, schema, want string
		perPage, pages     int
	}{
		{"tool-count", `{}`, "exceeds tool limit", maxListedTools / 2, 3},
		{"empty-pages", `{}`, "exceeds page limit", 0, maxListedPages},
		{"schema-bytes", `{"description":"` + strings.Repeat("x", 32<<10) + `"}`, "exceeds schema byte limit", 64, 4},
		{"skipped-schema-bytes", `{"description":"` + strings.Repeat("x", maxToolSchemaBytes+1) + `"}`, "exceeds schema byte limit", 1, 8},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var pages atomic.Int32
			c := newListClient(t, func() any {
				page := pages.Add(1)
				tools := make([]any, tc.perPage)
				for i := range tools {
					tools[i] = map[string]any{"name": fmt.Sprintf("tool-%d-%d", page, i), "inputSchema": json.RawMessage(tc.schema)}
				}
				return map[string]any{"tools": tools, "nextCursor": fmt.Sprint(page)}
			})
			tools, err := c.ListTools(t.Context())
			require.ErrorContains(t, err, tc.want)
			require.Nil(t, tools)
			require.EqualValues(t, tc.pages, pages.Load())
		})
	}
}

func TestListToolsJSONDiagnosticRedactsNumericValue(t *testing.T) {
	t.Parallel()
	const sentinel = "9876543210123456789e1000"
	schema := `{"type":"object","properties":{"private-field-sentinel":{"const":` + sentinel + `}}}`
	listing := map[string]any{"tools": []any{map[string]any{"name": "lookup", "inputSchema": json.RawMessage(schema)}}}

	// Prove that this valid JSON reaches SDK response decoding and produces a
	// typed decoder error whose original message contains the remote value.
	raw, err := json.Marshal(listing)
	require.NoError(t, err)
	var decoded mcp.ListToolsResult
	err = json.Unmarshal(raw, &decoded)
	var diagnostic *json.UnmarshalTypeError
	require.ErrorAs(t, err, &diagnostic)
	require.ErrorContains(t, err, sentinel)

	tools, err := newListClient(t, func() any { return listing }).ListTools(t.Context())
	require.Nil(t, tools)
	require.ErrorContains(t, err, "list tools from external mcp server")
	// The SDK may report offset zero; preserve the decoder metadata without
	// substituting the value-bearing original diagnostic.
	require.Regexp(t, `JSON type mismatch at byte offset [0-9]+ \(destination float64\)`, err.Error())
	require.NotContains(t, err.Error(), sentinel)
	require.NotContains(t, err.Error(), "private-field-sentinel")
}
