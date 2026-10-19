package testenv

import (
	"bytes"
	"encoding/json"
	"flag"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

var updateGoldens = flag.Bool("update", false, "regenerate YAML goldens from captured JSON output")

// Serialize update/read pairs for shared fixtures and reject conflicting outputs
// rather than letting the last parallel test silently win.
var goldenUpdates = struct {
	mu     sync.Mutex
	values map[string]any
}{mu: sync.Mutex{}, values: make(map[string]any)}

// RequireYAMLGolden compares one captured JSON document against a YAML fixture.
// fixturePath is relative to the calling test's working directory or absolute.
// Missing or mismatched fixtures fail normal runs. With -args -update, fixtures
// are generated from actualJSON and must be reviewed before committing.
//
// Comparison ignores object-key order and formatting, preserves exact numeric
// scalar text and string contents, and rejects trailing documents. YAML keys
// must be strings and numeric scalars must use JSON number syntax. Aliases and
// duplicate keys are rejected. Parallel tests may share a fixture if their
// captured JSON values agree when updating it.
func RequireYAMLGolden(t *testing.T, fixturePath string, actualJSON []byte) {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(actualJSON))
	decoder.UseNumber()
	var value any
	require.NoError(t, decoder.Decode(&value))
	require.ErrorIs(t, decoder.Decode(new(any)), io.EOF)
	path, err := filepath.Abs(fixturePath)
	require.NoError(t, err)
	if *updateGoldens {
		goldenUpdates.mu.Lock()
		defer goldenUpdates.mu.Unlock()
		if previous, ok := goldenUpdates.values[path]; ok {
			require.Equal(t, previous, value, "tests sharing golden %s produced different output", fixturePath)
		} else {
			data := encodeGoldenYAML(t, actualJSON)
			require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
			require.NoError(t, os.WriteFile(path, data, 0o600))
			goldenUpdates.values[path] = value
		}
	}
	fixture, err := os.ReadFile(filepath.Clean(path))
	require.NoError(t, err)
	// Retain scalar text in the syntax tree before any YAML-to-float conversion.
	fixtureDecoder := yaml.NewDecoder(bytes.NewReader(fixture))
	var document yaml.Node
	require.NoError(t, fixtureDecoder.Decode(&document))
	require.ErrorIs(t, fixtureDecoder.Decode(new(yaml.Node)), io.EOF)
	expected := goldenYAMLValue(t, &document)
	require.Equal(t, expected, value, "golden fixture: %s", fixturePath)
}

// encodeGoldenYAML parses captured JSON into a syntax tree, preserving scalar
// text and object order. Clearing JSON flow styles produces readable block YAML
// without passing numbers through float64 or injecting explanatory comments.
func encodeGoldenYAML(t *testing.T, actual []byte) []byte {
	t.Helper()
	var document yaml.Node
	require.NoError(t, yaml.Unmarshal(actual, &document))
	var blockStyle func(*yaml.Node)
	blockStyle = func(node *yaml.Node) {
		node.Style = 0
		for _, child := range node.Content {
			blockStyle(child)
		}
	}
	blockStyle(&document)
	var output bytes.Buffer
	encoder := yaml.NewEncoder(&output)
	encoder.SetIndent(2)
	require.NoError(t, encoder.Encode(&document))
	require.NoError(t, encoder.Close())
	return output.Bytes()
}

// goldenYAMLValue accepts JSON-shaped YAML without normalizing numeric literals.
func goldenYAMLValue(t *testing.T, node *yaml.Node) any {
	t.Helper()
	switch node.Kind {
	case yaml.DocumentNode:
		require.Len(t, node.Content, 1)
		return goldenYAMLValue(t, node.Content[0])
	case yaml.MappingNode:
		value := make(map[string]any, len(node.Content)/2)
		for i := 0; i < len(node.Content); i += 2 {
			key := node.Content[i]
			require.Equal(t, "!!str", key.Tag, "golden keys must be strings at line %d", key.Line)
			require.NotContains(t, value, key.Value, "duplicate golden key at line %d", key.Line)
			value[key.Value] = goldenYAMLValue(t, node.Content[i+1])
		}
		return value
	case yaml.SequenceNode:
		value := make([]any, len(node.Content))
		for i, child := range node.Content {
			value[i] = goldenYAMLValue(t, child)
		}
		return value
	case yaml.ScalarNode:
		if node.Tag == "!!int" || node.Tag == "!!float" {
			require.NotEmpty(t, node.Value)
			number := json.Number(node.Value)
			_, err := json.Marshal(number)
			require.NoError(t, err, "golden numbers must use JSON syntax at line %d", node.Line)
			return number
		}
		require.Contains(t, []string{"!!str", "!!bool", "!!null"}, node.Tag)
		var value any
		require.NoError(t, node.Decode(&value))
		return value
	default:
		require.FailNow(t, "goldens require JSON-shaped YAML without aliases", "line %d", node.Line)
		return nil
	}
}
