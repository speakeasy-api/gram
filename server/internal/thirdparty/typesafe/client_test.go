package typesafe

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func testClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return &Client{httpClient: server.Client(), apiKey: "test-key", endpoint: server.URL}
}

func testQuestions() map[string]Question {
	return map[string]Question{
		"match": {Type: "noul", Instructions: "does it match?", Criteria: map[string]string{"true": "yes", "false": "no"}},
	}
}

func TestEvaluateSuccess(t *testing.T) {
	t.Parallel()

	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))
		var req struct {
			Model     string              `json:"model"`
			State     json.RawMessage     `json:"state"`
			Questions map[string]Question `json:"questions"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		require.Equal(t, Model, req.Model)
		require.Contains(t, req.Questions, "match")

		_, err := w.Write([]byte(`{"model":"jev-1.13.0","answers":{"match":{"type":"noul","noul":0.75}},"usage":{"input_tokens":10,"output_tokens":2}}`))
		require.NoError(t, err)
	})

	result, err := client.Evaluate(t.Context(), json.RawMessage(`{"foo":"bar"}`), testQuestions())

	require.NoError(t, err)
	require.Equal(t, 0.75, result.Probabilities["match"])
	require.Equal(t, Model, result.Model)
	require.Equal(t, 10, result.InputTokens)
	require.Equal(t, 2, result.OutputTokens)
}

func TestEvaluateInvalidInput(t *testing.T) {
	t.Parallel()

	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("must not call typesafe with invalid input")
	})

	_, err := client.Evaluate(t.Context(), json.RawMessage(`not json`), testQuestions())
	require.Error(t, err)

	_, err = client.Evaluate(t.Context(), json.RawMessage(`{}`), nil)
	require.Error(t, err)
}

func TestEvaluateNonOKStatus(t *testing.T) {
	t.Parallel()

	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("secret-bearing error body"))
	})

	_, err := client.Evaluate(t.Context(), json.RawMessage(`{}`), testQuestions())

	require.Error(t, err)
	require.NotContains(t, err.Error(), "secret-bearing")
}

func TestEvaluateModelMismatch(t *testing.T) {
	t.Parallel()

	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"model":"some-other-model","answers":{"match":{"type":"noul","noul":0.5}},"usage":{"input_tokens":1,"output_tokens":1}}`))
	})

	_, err := client.Evaluate(t.Context(), json.RawMessage(`{}`), testQuestions())

	require.ErrorContains(t, err, "invalid typesafe response metadata")
}

func TestEvaluateMissingAnswer(t *testing.T) {
	t.Parallel()

	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"model":"jev-1.13.0","answers":{},"usage":{"input_tokens":1,"output_tokens":1}}`))
	})

	_, err := client.Evaluate(t.Context(), json.RawMessage(`{}`), testQuestions())

	require.ErrorContains(t, err, "invalid typesafe response metadata")
}

func TestEvaluateOutOfRangeProbability(t *testing.T) {
	t.Parallel()

	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"model":"jev-1.13.0","answers":{"match":{"type":"noul","noul":1.5}},"usage":{"input_tokens":1,"output_tokens":1}}`))
	})

	_, err := client.Evaluate(t.Context(), json.RawMessage(`{}`), testQuestions())

	require.ErrorContains(t, err, "invalid typesafe probability")
}

func TestUnavailableEvaluatorReturnsErrUnavailable(t *testing.T) {
	t.Parallel()

	_, err := (Unavailable{}).Evaluate(t.Context(), json.RawMessage(`{}`), testQuestions())

	require.ErrorIs(t, err, ErrUnavailable)
}
