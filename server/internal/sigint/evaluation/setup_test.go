package evaluation

import (
	"bytes"
	"crypto/x509"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/speakeasy-api/gram/server/internal/classifier/jev"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// modelRequests captures the HTTP boundary, including Jev's wire compilation.
// Expected requests live only in reviewed fixtures, never in this fake model.
type modelRequests struct {
	mu     sync.Mutex
	bodies []json.RawMessage
}

func (r *modelRequests) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.bodies)
}

func (r *modelRequests) snapshot(t *testing.T) []byte {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	data, err := json.Marshal(r.bodies)
	require.NoError(t, err)
	return data
}

func newRecordingClassifier(t *testing.T) (*jev.Classifier, *modelRequests) {
	t.Helper()
	requests := &modelRequests{}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "read model request", http.StatusBadRequest)
			return
		}
		requests.mu.Lock()
		requests.bodies = append(requests.bodies, json.RawMessage(body))
		requests.mu.Unlock()

		// Decode only enough to return successful canned answers. Request assertions
		// happen against the complete captured bytes on the test goroutine.
		var req struct {
			Questions map[string]struct {
				Type string `json:"type"`
			} `json:"questions"`
		}
		if json.Unmarshal(body, &req) != nil {
			http.Error(w, "decode model request", http.StatusBadRequest)
			return
		}
		answers := map[string]any{}
		for key, q := range req.Questions {
			switch q.Type {
			case "noul":
				answers[key] = map[string]any{"type": "noul", "noul": 0.8}
			case "choice":
				answers[key] = map[string]any{"type": "choice", "choice": "1", "probabilities": map[string]float64{"0": 0.2, "1": 0.8}, "confidence": 0.5}
			case "score":
				answers[key] = map[string]any{"type": "score", "score": 0.8, "probabilities": map[string]float64{"0": 0.2, "1": 0.8}, "confidence": 0.5}
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"model": "test-model", "answers": answers, "usage": map[string]int{"input_tokens": 100, "output_tokens": 20}})
	}))
	t.Cleanup(server.Close)
	roots := x509.NewCertPool()
	roots.AddCert(server.Certificate())
	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), nil, guardian.WithTLSRootCAs(roots))
	require.NoError(t, err)
	c, err := jev.New(policy, conv.NewSecret([]byte("test-key")), jev.WithEndpoint(server.URL))
	require.NoError(t, err)
	return c, requests
}

func requireGolden(t *testing.T, name string, actual []byte) {
	t.Helper()
	fixture, err := os.ReadFile(filepath.Join("testdata", name+".golden.yaml"))
	require.NoError(t, err)
	// Retain scalar text in the syntax tree before any YAML-to-float conversion.
	fixtureDecoder := yaml.NewDecoder(bytes.NewReader(fixture))
	var document yaml.Node
	require.NoError(t, fixtureDecoder.Decode(&document))
	require.ErrorIs(t, fixtureDecoder.Decode(new(yaml.Node)), io.EOF)
	expected := goldenYAMLValue(t, &document)
	decoder := json.NewDecoder(bytes.NewReader(actual))
	decoder.UseNumber()
	var value any
	require.NoError(t, decoder.Decode(&value))
	require.ErrorIs(t, decoder.Decode(new(any)), io.EOF)
	require.Equal(t, expected, value, "golden fixture: %s", name)
}

// goldenYAMLValue accepts JSON-shaped YAML without normalizing numeric literals.
// Mapping keys must be strings, and numeric scalars must use JSON number syntax.
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
