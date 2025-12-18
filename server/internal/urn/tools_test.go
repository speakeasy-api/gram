package urn_test

import (
	"database/sql/driver"
	"encoding/json"
	"strings"
	"testing"

	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
)

func TestNewTool(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		kind     urn.ToolKind
		source   string
		toolName string
		wantErr  error
	}{
		{
			name:     "valid function tool",
			kind:     urn.ToolKindFunction,
			source:   "my-source",
			toolName: "my-tool",
			wantErr:  nil,
		},
		{
			name:     "valid http tool",
			kind:     urn.ToolKindHTTP,
			source:   "api-server",
			toolName: "get-users",
			wantErr:  nil,
		},
		{
			name:     "valid externalmcp tool",
			kind:     urn.ToolKindExternalMCP,
			source:   "github",
			toolName: "proxy",
			wantErr:  nil,
		},
		{
			name:     "valid with numbers",
			kind:     urn.ToolKindFunction,
			source:   "source123",
			toolName: "tool456",
			wantErr:  nil,
		},
		{
			name:     "valid with underscores and dashes",
			kind:     urn.ToolKindHTTP,
			source:   "my_source-v2",
			toolName: "my_tool-name",
			wantErr:  nil,
		},
		{
			name:     "empty source",
			kind:     urn.ToolKindFunction,
			source:   "",
			toolName: "my-tool",
			wantErr:  urn.ErrInvalid,
		},
		{
			name:     "empty tool name",
			kind:     urn.ToolKindFunction,
			source:   "my-source",
			toolName: "",
			wantErr:  urn.ErrInvalid,
		},
		{
			name:     "invalid kind",
			kind:     urn.ToolKind("invalid"),
			source:   "my-source",
			toolName: "my-tool",
			wantErr:  urn.ErrInvalid,
		},
		{
			name:     "source too long",
			kind:     urn.ToolKindFunction,
			source:   strings.Repeat("a", 129), // maxSegmentLength+1
			toolName: "my-tool",
			wantErr:  urn.ErrInvalid,
		},
		{
			name:     "tool name too long",
			kind:     urn.ToolKindFunction,
			source:   "my-source",
			toolName: strings.Repeat("a", 129), // maxSegmentLength+1
			wantErr:  urn.ErrInvalid,
		},
		{
			name:     "source with invalid characters",
			kind:     urn.ToolKindFunction,
			source:   "my source!",
			toolName: "my-tool",
			wantErr:  urn.ErrInvalid,
		},
		{
			name:     "tool name with invalid characters",
			kind:     urn.ToolKindFunction,
			source:   "my-source",
			toolName: "my tool!",
			wantErr:  urn.ErrInvalid,
		},
		{
			name:     "source starting with dash",
			kind:     urn.ToolKindFunction,
			source:   "-my-source",
			toolName: "my-tool",
			wantErr:  nil,
		},
		{
			name:     "tool name ending with dash",
			kind:     urn.ToolKindFunction,
			source:   "my-source",
			toolName: "my-tool-",
			wantErr:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tool := urn.NewTool(tt.kind, tt.source, tt.toolName)

			if tt.wantErr != nil {
				// If we expect an error, the String method should return empty or we should get error when marshaling
				_, err := tool.MarshalJSON()
				require.ErrorIs(t, err, tt.wantErr)
			} else {
				require.NotEmpty(t, tool.String())
				require.Equal(t, tt.kind, tool.Kind)
				require.Equal(t, tt.source, tool.Source)
				require.Equal(t, tt.toolName, tool.Name)

				// Validate through marshaling operations
				_, err := tool.MarshalJSON()
				require.NoError(t, err)
				_, err = tool.MarshalText()
				require.NoError(t, err)
				_, err = tool.Value()
				require.NoError(t, err)
			}
		})
	}
}

func TestTool_String(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		tool urn.Tool
		want string
	}{
		{
			name: "function tool",
			tool: urn.NewTool(urn.ToolKindFunction, "my-source", "my-tool"),
			want: "tools:function:my-source:my-tool",
		},
		{
			name: "http tool",
			tool: urn.NewTool(urn.ToolKindHTTP, "api-server", "get-users"),
			want: "tools:http:api-server:get-users",
		},
		{
			name: "externalmcp tool",
			tool: urn.NewTool(urn.ToolKindExternalMCP, "github", "proxy"),
			want: "tools:externalmcp:github:proxy",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := tt.tool.String()
			require.Equal(t, tt.want, got)
		})
	}
}

func TestTool_MarshalJSON(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		tool    urn.Tool
		want    string
		wantErr error
	}{
		{
			name:    "valid function tool",
			tool:    urn.NewTool(urn.ToolKindFunction, "my-source", "my-tool"),
			want:    `"tools:function:my-source:my-tool"`,
			wantErr: nil,
		},
		{
			name:    "valid http tool",
			tool:    urn.NewTool(urn.ToolKindHTTP, "api-server", "get-users"),
			want:    `"tools:http:api-server:get-users"`,
			wantErr: nil,
		},
		{
			name:    "invalid tool - empty source",
			tool:    urn.NewTool(urn.ToolKindFunction, "", "my-tool"),
			want:    "",
			wantErr: urn.ErrInvalid,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := tt.tool.MarshalJSON()
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}

			require.NoError(t, err)
			require.Equal(t, tt.want, string(got))
		})
	}
}

func TestTool_UnmarshalJSON(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		input   string
		want    urn.Tool
		wantErr bool
	}{
		{
			name:    "valid function tool",
			input:   `"tools:function:my-source:my-tool"`,
			want:    urn.NewTool(urn.ToolKindFunction, "my-source", "my-tool"),
			wantErr: false,
		},
		{
			name:    "valid http tool",
			input:   `"tools:http:api-server:get-users"`,
			want:    urn.NewTool(urn.ToolKindHTTP, "api-server", "get-users"),
			wantErr: false,
		},
		{
			name:    "invalid json",
			input:   `invalid json`,
			want:    urn.Tool{Kind: "", Source: "", Name: ""},
			wantErr: true,
		},
		{
			name:    "non-string json",
			input:   `123`,
			want:    urn.Tool{Kind: "", Source: "", Name: ""},
			wantErr: true,
		},
		{
			name:    "invalid tool string - wrong prefix",
			input:   `"invalid:tool:string"`,
			want:    urn.Tool{Kind: "", Source: "", Name: ""},
			wantErr: true,
		},
		{
			name:    "empty string",
			input:   `""`,
			want:    urn.Tool{Kind: "", Source: "", Name: ""},
			wantErr: true,
		},
		{
			name:    "too few segments",
			input:   `"tools:function:my-source"`,
			want:    urn.Tool{Kind: "", Source: "", Name: ""},
			wantErr: true,
		},
		{
			name:    "too many segments",
			input:   `"tools:function:my-source:my-tool:extra"`,
			want:    urn.Tool{Kind: "", Source: "", Name: ""},
			wantErr: true,
		},
		{
			name:    "invalid tool kind",
			input:   `"tools:invalid:my-source:my-tool"`,
			want:    urn.Tool{Kind: "", Source: "", Name: ""},
			wantErr: true,
		},
		{
			name:    "empty kind",
			input:   `"tools::my-source:my-tool"`,
			want:    urn.Tool{Kind: "", Source: "", Name: ""},
			wantErr: true,
		},
		{
			name:    "empty source",
			input:   `"tools:function::my-tool"`,
			want:    urn.Tool{Kind: "", Source: "", Name: ""},
			wantErr: true,
		},
		{
			name:    "empty tool name",
			input:   `"tools:function:my-source:"`,
			want:    urn.Tool{Kind: "", Source: "", Name: ""},
			wantErr: true,
		},
		{
			name:    "source with invalid characters",
			input:   `"tools:function:my source:my-tool"`,
			want:    urn.Tool{Kind: "", Source: "", Name: ""},
			wantErr: true,
		},
		{
			name:    "tool name with invalid characters",
			input:   `"tools:function:my-source:my tool"`,
			want:    urn.Tool{Kind: "", Source: "", Name: ""},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var got urn.Tool
			err := got.UnmarshalJSON([]byte(tt.input))
			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.Equal(t, tt.want.Kind, got.Kind)
			require.Equal(t, tt.want.Source, got.Source)
			require.Equal(t, tt.want.Name, got.Name)
		})
	}
}

func TestTool_Scan(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		input   any
		want    urn.Tool
		wantErr bool
	}{
		{
			name:    "string input",
			input:   "tools:function:my-source:my-tool",
			want:    urn.NewTool(urn.ToolKindFunction, "my-source", "my-tool"),
			wantErr: false,
		},
		{
			name:    "byte slice input",
			input:   []byte("tools:http:api-server:get-users"),
			want:    urn.NewTool(urn.ToolKindHTTP, "api-server", "get-users"),
			wantErr: false,
		},
		{
			name:    "nil input",
			input:   nil,
			want:    urn.Tool{Kind: "", Source: "", Name: ""},
			wantErr: false,
		},
		{
			name:    "unsupported type",
			input:   123,
			want:    urn.Tool{Kind: "", Source: "", Name: ""},
			wantErr: true,
		},
		{
			name:    "invalid string",
			input:   "invalid:tool:string",
			want:    urn.Tool{Kind: "", Source: "", Name: ""},
			wantErr: true,
		},
		{
			name:    "empty string",
			input:   "",
			want:    urn.Tool{Kind: "", Source: "", Name: ""},
			wantErr: true,
		},
		{
			name:    "invalid characters",
			input:   "tools:function:my source:my-tool",
			want:    urn.Tool{Kind: "", Source: "", Name: ""},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var got urn.Tool
			err := got.Scan(tt.input)
			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			if tt.input != nil {
				require.Equal(t, tt.want.Kind, got.Kind)
				require.Equal(t, tt.want.Source, got.Source)
				require.Equal(t, tt.want.Name, got.Name)
			}
		})
	}
}

func TestTool_Value(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		tool    urn.Tool
		want    driver.Value
		wantErr bool
	}{
		{
			name:    "valid function tool",
			tool:    urn.NewTool(urn.ToolKindFunction, "my-source", "my-tool"),
			want:    "tools:function:my-source:my-tool",
			wantErr: false,
		},
		{
			name:    "valid http tool",
			tool:    urn.NewTool(urn.ToolKindHTTP, "api-server", "get-users"),
			want:    "tools:http:api-server:get-users",
			wantErr: false,
		},
		{
			name:    "invalid tool - empty source",
			tool:    urn.NewTool(urn.ToolKindFunction, "", "my-tool"),
			want:    "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := tt.tool.Value()
			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestTool_MarshalText(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		tool    urn.Tool
		want    []byte
		wantErr bool
	}{
		{
			name:    "valid function tool",
			tool:    urn.NewTool(urn.ToolKindFunction, "my-source", "my-tool"),
			want:    []byte("tools:function:my-source:my-tool"),
			wantErr: false,
		},
		{
			name:    "valid http tool",
			tool:    urn.NewTool(urn.ToolKindHTTP, "api-server", "get-users"),
			want:    []byte("tools:http:api-server:get-users"),
			wantErr: false,
		},
		{
			name:    "invalid tool - empty name",
			tool:    urn.NewTool(urn.ToolKindFunction, "my-source", ""),
			want:    nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := tt.tool.MarshalText()
			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestTool_UnmarshalText(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		input   []byte
		want    urn.Tool
		wantErr bool
	}{
		{
			name:    "valid function tool",
			input:   []byte("tools:function:my-source:my-tool"),
			want:    urn.NewTool(urn.ToolKindFunction, "my-source", "my-tool"),
			wantErr: false,
		},
		{
			name:    "valid http tool",
			input:   []byte("tools:http:api-server:get-users"),
			want:    urn.NewTool(urn.ToolKindHTTP, "api-server", "get-users"),
			wantErr: false,
		},
		{
			name:    "invalid tool string",
			input:   []byte("invalid:tool:string"),
			want:    urn.Tool{Kind: "", Source: "", Name: ""},
			wantErr: true,
		},
		{
			name:    "empty input",
			input:   []byte(""),
			want:    urn.Tool{Kind: "", Source: "", Name: ""},
			wantErr: true,
		},
		{
			name:    "invalid characters",
			input:   []byte("tools:function:my source:my-tool"),
			want:    urn.Tool{Kind: "", Source: "", Name: ""},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var got urn.Tool
			err := got.UnmarshalText(tt.input)
			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.Equal(t, tt.want.Kind, got.Kind)
			require.Equal(t, tt.want.Source, got.Source)
			require.Equal(t, tt.want.Name, got.Name)
		})
	}
}

func TestTool_roundTrip(t *testing.T) {
	t.Parallel()
	original := urn.NewTool(urn.ToolKindHTTP, "api-server", "get-users-v2")

	// Test JSON round trip
	jsonData, err := json.Marshal(original)
	require.NoError(t, err)

	var fromJSON urn.Tool
	err = json.Unmarshal(jsonData, &fromJSON)
	require.NoError(t, err)
	require.Equal(t, original.Kind, fromJSON.Kind)
	require.Equal(t, original.Source, fromJSON.Source)
	require.Equal(t, original.Name, fromJSON.Name)

	// Test text round trip
	textData, err := original.MarshalText()
	require.NoError(t, err)

	var fromText urn.Tool
	err = fromText.UnmarshalText(textData)
	require.NoError(t, err)
	require.Equal(t, original.Kind, fromText.Kind)
	require.Equal(t, original.Source, fromText.Source)
	require.Equal(t, original.Name, fromText.Name)

	// Test database round trip
	value, err := original.Value()
	require.NoError(t, err)

	var fromDB urn.Tool
	err = fromDB.Scan(value)
	require.NoError(t, err)
	require.Equal(t, original.Kind, fromDB.Kind)
	require.Equal(t, original.Source, fromDB.Source)
	require.Equal(t, original.Name, fromDB.Name)
}

func TestTool_edgeCases(t *testing.T) {
	t.Parallel()

	t.Run("single character segments", func(t *testing.T) {
		t.Parallel()
		tool := urn.NewTool(urn.ToolKindFunction, "a", "b")

		// Should be valid - test through marshaling
		_, err := tool.MarshalJSON()
		require.NoError(t, err)
	})

	t.Run("boundary slug patterns", func(t *testing.T) {
		t.Parallel()
		validCases := []string{
			"a",
			"a1",
			"a1b2c3",
			"abc-def",
			"abc_def",
			"abc-def-ghi",
			"abc_def_ghi",
			"a1-b2_c3",
			"-abc", // starts with dash
			"_abc", // starts with underscore
			"abc-", // ends with dash
			"abc_", // ends with underscore
		}

		for _, validCase := range validCases {
			t.Run("valid_"+validCase, func(t *testing.T) {
				t.Parallel()
				tool := urn.NewTool(urn.ToolKindFunction, validCase, validCase)

				// Test validation through marshaling
				_, err := tool.MarshalJSON()
				require.NoError(t, err)
			})
		}

		invalidCases := []string{
			"AB",  // uppercase
			"a b", // space
			"a.b", // dot
			"a@b", // at symbol
		}

		for _, invalidCase := range invalidCases {
			t.Run("invalid_"+invalidCase, func(t *testing.T) {
				t.Parallel()
				tool := urn.NewTool(urn.ToolKindFunction, invalidCase, "valid")

				// Test validation through marshaling - should fail
				_, err := tool.MarshalJSON()
				require.Error(t, err)
			})
		}
	})
}

func TestTool_validationCaching(t *testing.T) {
	t.Parallel()

	// Test that validation results are consistent across multiple calls
	tool := urn.NewTool(urn.ToolKindFunction, "my-source", "my-tool")

	// Multiple calls to operations that trigger validation should be consistent
	str1 := tool.String()
	str2 := tool.String()
	require.Equal(t, str1, str2)
	require.NotEmpty(t, str1)

	json1, err1 := tool.MarshalJSON()
	require.NoError(t, err1)
	json2, err2 := tool.MarshalJSON()
	require.NoError(t, err2)
	require.JSONEq(t, string(json1), string(json2))

	// Test with invalid tool
	invalidTool := urn.NewTool(urn.ToolKindFunction, "", "my-tool")

	_, err1 = invalidTool.MarshalJSON()
	require.Error(t, err1)
	_, err2 = invalidTool.MarshalJSON()
	require.Error(t, err2)
	// Error messages should be consistent
	require.Equal(t, err1.Error(), err2.Error())
}
