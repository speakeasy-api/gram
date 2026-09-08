package externalmcp

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParameterHeadersSDKValues(t *testing.T) {
	t.Parallel()
	// Canonical vectors from go-sdk v1.7.0's TestEncodeHeaderValue and
	// TestUnmarshalPrimitive, exercised through the schema-driven adapter.
	tests := []struct {
		name, typ, raw, want string
		omit                 bool
	}{
		{"plain ASCII", "string", `"us-west1"`, "us-west1", false},
		{"empty", "string", `""`, "=?base64??=", false},
		{"internal spaces", "string", `"us west 1"`, "us west 1", false},
		{"leading space", "string", `" us-west1"`, "=?base64?IHVzLXdlc3Qx?=", false},
		{"trailing space", "string", `"us-west1 "`, "=?base64?dXMtd2VzdDEg?=", false},
		{"both spaces", "string", `" us-west1 "`, "=?base64?IHVzLXdlc3QxIA==?=", false},
		{"Unicode", "string", `"日本語"`, "=?base64?5pel5pys6Kqe?=", false},
		{"mixed Unicode", "string", `"Hello, 世界"`, "=?base64?SGVsbG8sIOS4lueVjA==?=", false},
		{"newline", "string", `"line1\nline2"`, "=?base64?bGluZTEKbGluZTI=?=", false},
		{"CRLF", "string", `"line1\r\nline2"`, "=?base64?bGluZTENCmxpbmUy?=", false},
		{"tab", "string", `"\tindented"`, "=?base64?CWluZGVudGVk?=", false},
		{"internal tab", "string", `"a\tb"`, "=?base64?YQli?=", false},
		{"DEL", "string", `"\u007f"`, "=?base64?fw==?=", false},
		{"NUL", "string", `"\u0000"`, "=?base64?AA==?=", false},
		{"sentinel literal", "string", `"=?base64?literal?="`, "=?base64?PT9iYXNlNjQ/bGl0ZXJhbD89?=", false},
		{"sentinel empty", "string", `"=?base64??="`, "=?base64?PT9iYXNlNjQ/Pz0=?=", false},
		{"uppercase sentinel", "string", `"=?BASE64?abc?="`, "=?BASE64?abc?=", false},
		{"partial sentinel", "string", `"=?base64?abc"`, "=?base64?abc", false},
		{"reserved printable", "string", `"a,b;c=\"d\" /?:@[]{}\\!#$%&'()*+<>~"`, "a,b;c=\"d\" /?:@[]{}\\!#$%&'()*+<>~", false},
		{"integer", "integer", `42`, "42", false},
		{"zero", "integer", `0`, "0", false},
		{"negative", "integer", `-7`, "-7", false},
		{"max safe", "integer", `9007199254740991`, "9007199254740991", false},
		{"min safe", "integer", `-9007199254740991`, "-9007199254740991", false},
		{"integer-valued float", "integer", `42.0`, "42", false},
		{"exponent", "integer", `4.2e1`, "42", false},
		{"negative zero", "integer", `-0.0`, "0", false},
		{"true", "boolean", `true`, "true", false},
		{"false", "boolean", `false`, "false", false},
		// The SDK does not validate argument types against the schema.
		{"primitive type mismatch", "integer", `"text"`, "text", false},
		{"fraction", "integer", `3.14`, "", true},
		{"above safe boundary", "integer", `9007199254740992`, "", true},
		{"below safe boundary", "integer", `-9007199254740992`, "", true},
		{"rounded unsafe integer", "integer", `9007199254740993`, "", true},
		{"negative unsafe integer", "integer", `-9007199254740993`, "", true},
		{"overflow", "integer", `1e999`, "", true},
		{"null", "string", `null`, "", true},
		{"array", "string", `[1,2]`, "", true},
		{"object", "string", `{"a":1}`, "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			schema := json.RawMessage(`{"properties":{"value":{"type":"` + tt.typ + `","x-mcp-header":"Value"}}}`)
			args := json.RawMessage(`{"value":` + tt.raw + `,"unrelated":900719925474099312345}`)
			original := bytes.Clone(args)
			headers, err := parameterHeaders(schema, args)
			require.NoError(t, err)
			require.Equal(t, original, []byte(args), "argument bytes must not change")
			if tt.omit {
				require.Empty(t, headers)
			} else {
				require.Equal(t, http.Header{"Mcp-Param-Value": []string{tt.want}}, headers)
			}
		})
	}
}

func TestParameterHeadersNested(t *testing.T) {
	t.Parallel()
	schema := json.RawMessage(`{"type":"object","properties":{
		"region":{"type":"string","x-mcp-header":"Region"},
		"query":{"type":["string","null"]},
		"options":{"type":"object","properties":{
			"priority":{"type":"integer","x-mcp-header":"Priority"},
			"nested":{"properties":{"flag":{"type":"boolean","x-mcp-header":"Flag"}}}
		}}
	}}`)
	headers, err := parameterHeaders(schema, json.RawMessage(`{"region":"us-west1","query":"SELECT 1","options":{"priority":42.0,"nested":{"flag":false}}}`))
	require.NoError(t, err)
	require.Equal(t, http.Header{
		"Mcp-Param-Region":   []string{"us-west1"},
		"Mcp-Param-Priority": []string{"42"},
		"Mcp-Param-Flag":     []string{"false"},
	}, headers)
	for _, args := range []string{`{}`, `null`, ``, `{"options":null}`, `{"options":[]}`, `{"options":42}`, `{"options":{"nested":"text"}}`, `{"options.priority":42}`} {
		headers, err := parameterHeaders(schema, json.RawMessage(args))
		require.NoError(t, err)
		require.Empty(t, headers)
	}
}

func TestParameterHeadersAnnotationFreeSchemas(t *testing.T) {
	t.Parallel()
	for _, schema := range []string{
		`{}`, `true`, `false`,
		`{"type":["object","null"],"properties":{"value":{"type":["string","null"]},"allowed":true,"forbidden":false}}`,
		`{"$ref":"#/$defs/value","$defs":{"value":{"anyOf":[{"type":"string"},{"type":"number"}]}}}`,
		`{"properties":{"list":{"type":"array","items":{"type":"string"}},"value":{"enum":[1,"a",null]}}}`,
	} {
		headers, err := parameterHeaders(json.RawMessage(schema), json.RawMessage(`{"value":"private-value"}`))
		require.NoError(t, err, schema)
		require.Empty(t, headers)
	}
}

func TestParameterHeadersInvalidSchemas(t *testing.T) {
	t.Parallel()
	for _, schema := range []string{
		``, `null`, `[]`, `42`, `"schema"`, `{`, `{} {}`,
		`{"properties":null}`, `{"properties":[]}`, `{"properties":"bad"}`,
		`{"properties":{"value":null}}`, `{"properties":{"value":[]}}`,
		`{"properties":{"value":{"properties":1}}}`,
		`{"type":null}`, `{"type":42}`, `{"type":"unknown"}`, `{"type":[]}`,
		`{"type":["string","string"]}`, `{"type":["string",42]}`,
		`{"properties":{"value":{"x-mcp-header":"Value"}}}`,
		`{"properties":{"value":{"type":"number","x-mcp-header":"Value"}}}`,
		`{"properties":{"value":{"type":"object","x-mcp-header":"Value"}}}`,
		`{"properties":{"value":{"type":"array","x-mcp-header":"Value"}}}`,
		`{"properties":{"value":{"type":["string","null"],"x-mcp-header":"Value"}}}`,
		`{"properties":{"value":{"type":["string"],"x-mcp-header":"Value"}}}`,
	} {
		headers, err := parameterHeaders(json.RawMessage(schema), json.RawMessage(`{"value":"private-value"}`))
		require.Error(t, err, schema)
		require.NotContains(t, err.Error(), "private-value")
		require.Nil(t, headers)
	}
}

func TestParameterHeadersNames(t *testing.T) {
	t.Parallel()
	for _, name := range []any{nil, true, 42, []string{"Value"}, map[string]string{"name": "Value"}, "", "has space", "bad:colon", "bad/slash", "bad=value", "bad,comma", "bad\r\nInjected", "bad\tname", "日本語", "bad\x7f", "bad\x00"} {
		raw, err := json.Marshal(name)
		require.NoError(t, err)
		schema := json.RawMessage(`{"properties":{"value":{"type":"string","x-mcp-header":` + string(raw) + `}}}`)
		headers, err := parameterHeaders(schema, nil)
		require.Error(t, err, string(raw))
		require.Nil(t, headers)
	}
	name := "AZaz09!#$%&'*+-.^_`|~"
	raw, err := json.Marshal(name)
	require.NoError(t, err)
	headers, err := parameterHeaders(json.RawMessage(`{"properties":{"value":{"type":"string","x-mcp-header":`+string(raw)+`}}}`), json.RawMessage(`{"value":"ok"}`))
	require.NoError(t, err)
	require.Equal(t, "ok", headers.Get("Mcp-Param-"+name))

	for _, nested := range []bool{false, true} {
		other := `"other":{"type":"string","x-mcp-header":"vALue"}`
		if nested {
			other = `"outer":{"properties":{` + other + `}}`
		}
		schema := json.RawMessage(`{"properties":{"value":{"type":"string","x-mcp-header":"Value"},` + other + `}}`)
		headers, err := parameterHeaders(schema, nil)
		require.EqualError(t, err, "duplicate x-mcp-header name (case-insensitive)")
		require.Nil(t, headers)
	}
}

func TestParameterHeadersInvalidArguments(t *testing.T) {
	t.Parallel()
	schema := json.RawMessage(`{"properties":{"value":{"type":"string","x-mcp-header":"Value"}}}`)
	for _, args := range []string{`{"value":"private-value"`, `"private-value"`, `["private-value"]`, `42`, `{} {}`} {
		headers, err := parameterHeaders(schema, json.RawMessage(args))
		require.EqualError(t, err, "invalid MCP tool arguments")
		require.Nil(t, headers)
	}
}

func TestParameterHeadersSchemaBudgets(t *testing.T) {
	t.Parallel()
	t.Run("bytes", func(t *testing.T) {
		// Even ignored metadata and whitespace must count before parsing.
		schema := `{ "description":"` + strings.Repeat("x", maxParameterHeaderSchemaBytes-len(`{ "description":""}`)) + `"}`
		require.Len(t, schema, maxParameterHeaderSchemaBytes)
		_, err := parameterHeaders(json.RawMessage(schema), nil)
		require.NoError(t, err)
		headers, err := parameterHeaders(json.RawMessage(schema+" "), nil)
		require.EqualError(t, err, "MCP tool input schema exceeds byte limit")
		require.Nil(t, headers)
	})
	t.Run("depth", func(t *testing.T) {
		schema := `{"type":"string","x-mcp-header":"Value"}`
		for range maxParameterHeaderDepth {
			schema = `{"properties":{"nested":` + schema + `}}`
		}
		_, err := parameterHeaders(json.RawMessage(schema), nil)
		require.NoError(t, err)
		schema = `{"properties":{"nested":` + schema + `}}`
		headers, err := parameterHeaders(json.RawMessage(schema), nil)
		require.EqualError(t, err, "MCP tool input schema exceeds depth limit")
		require.Nil(t, headers)
	})
	t.Run("nodes", func(t *testing.T) {
		// Boolean and unannotated nodes consume the same budget as bindings.
		properties := make(map[string]json.RawMessage)
		properties["value"] = json.RawMessage(`{"type":"string","x-mcp-header":"Value"}`)
		for i := range maxParameterHeaderNodes - 2 {
			properties[strconv.Itoa(i)] = json.RawMessage(`true`)
		}
		schema, err := json.Marshal(map[string]any{"properties": properties})
		require.NoError(t, err)
		headers, err := parameterHeaders(schema, json.RawMessage(`{"value":"ok"}`))
		require.NoError(t, err)
		require.Equal(t, "ok", headers.Get("Mcp-Param-Value"))
		properties["overflow"] = json.RawMessage(`{}`)
		schema, err = json.Marshal(map[string]any{"properties": properties})
		require.NoError(t, err)
		headers, err = parameterHeaders(schema, json.RawMessage(`{"value":"private-value"}`))
		require.EqualError(t, err, "MCP tool input schema exceeds node limit")
		require.Nil(t, headers, "never return partial headers")
	})
}
