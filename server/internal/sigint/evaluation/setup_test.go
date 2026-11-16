package evaluation

import (
	"crypto/x509"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

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
