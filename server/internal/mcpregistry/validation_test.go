package mcpregistry

import (
	"bytes"
	"encoding/json"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/speakeasy-api/gram/server/internal/mcpregistry/contract"
	"github.com/stretchr/testify/require"
	"os"
	"strings"
	"testing"
)

func TestRecordContract(t *testing.T) {
	t.Parallel()

	v, err := LoadValidator()
	require.NoError(t, err)

	raw, err := os.ReadFile("contract/testdata/contract-cases.json")
	require.NoError(t, err)
	var cases []struct {
		Name   string          `json:"name"`
		Record json.RawMessage `json:"record"`
		Valid  bool            `json:"valid"`
	}
	require.NoError(t, json.Unmarshal(raw, &cases))

	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) { t.Parallel(); require.Equal(t, tc.Valid, len(v.Validate(tc.Record)) == 0) })
	}
}

func TestValidationBoundaries(t *testing.T) {
	t.Parallel()

	v, err := LoadValidator()
	require.NoError(t, err)

	for _, raw := range []string{`{} {}`, `{} trailing`, `null`, `{"server":{"name":""}}`} {
		require.NotEmpty(t, v.Validate([]byte(raw)))
	}
	valid := []byte(`{"server":{"name":"io.example/test","description":"test","version":"1"}}`)
	require.Empty(t, v.Validate(valid))

	for _, suffix := range []string{" {}", " trailing"} {
		require.NotEmpty(t, v.Validate(append(append([]byte{}, valid...), suffix...)))
	}

	_, err = contract.Compile([]byte(`{"$ref":"https://example.invalid/schema"}`))
	require.Error(t, err)
}

func TestValidationDiagnostics(t *testing.T) {
	t.Parallel()

	v, err := LoadValidator()
	require.NoError(t, err)

	issues := v.Validate([]byte(`{"server":{"name":"SECRET_BAD_NAME","version":42,"description":false,"remotes":[{"type":"streamable-http","url":"SECRET_BAD_URL"}]}}`))
	paths := map[string]bool{}
	for _, issue := range issues {
		paths[issue.Path] = true
		require.NotContains(t, issue.Message, "SECRET")
		require.LessOrEqual(t, len(issue.Path), 256)
	}
	require.True(t, paths["/server/name"])
	require.True(t, paths["/server/version"])
	require.True(t, paths["/server/remotes/0/url"])

	remotes := make([]any, 100)
	for i := range remotes {
		remotes[i] = map[string]any{"type": "streamable-http", "url": "SECRET_BAD_URL"}
	}
	raw, err := json.Marshal(map[string]any{"server": map[string]any{"name": "io.example/test", "version": "1", "description": "test", "remotes": remotes}})
	require.NoError(t, err)
	require.Len(t, v.Validate(raw), 20)
}

func TestValidationSize(t *testing.T) {
	t.Parallel()

	v, err := LoadValidator()
	require.NoError(t, err)
	require.NotEmpty(t, v.Validate(make([]byte, (8<<20)+1)))
}

func TestDiagnosticPointerEscapingAndBounds(t *testing.T) {
	t.Parallel()

	for _, key := range []string{"a~/b", strings.Repeat("x", 300)} {
		schema, err := json.Marshal(map[string]any{"type": "object", "properties": map[string]any{key: map[string]any{"type": "string"}}})
		require.NoError(t, err)
		compiled, err := contract.Compile(schema)
		require.NoError(t, err)
		raw, err := json.Marshal(map[string]any{key: 42})
		require.NoError(t, err)
		value, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
		require.NoError(t, err)
		issues := validationIssues(compiled.Validate(value))
		require.Len(t, issues, 1)
		require.LessOrEqual(t, len(issues[0].Path), 256)
		if key == "a~/b" {
			require.Equal(t, "/a~0~1b", issues[0].Path)
		} else {
			require.Empty(t, issues[0].Path)
			require.Contains(t, issues[0].Message, "path truncated")
		}
	}
}

func TestApprovedRecordSizeLimit(t *testing.T) {
	t.Parallel()

	v, err := LoadValidator()
	require.NoError(t, err)

	prefix := `{"server":{"name":"io.example/test","description":"test","version":"1"},"extension":"`
	suffix := `"}`
	raw := []byte(prefix + strings.Repeat("x", (8<<20)-len(prefix)-len(suffix)) + suffix)
	require.Len(t, raw, 8<<20)
	require.Empty(t, v.Validate(raw))
	require.Equal(t, []Issue{{Message: "record exceeds byte limit"}}, v.Validate(append(raw, ' ')))
}
