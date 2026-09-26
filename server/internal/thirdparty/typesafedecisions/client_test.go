package typesafedecisions

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

func testClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return &Client{httpClient: server.Client(), resolveKey: func(context.Context, string) (string, error) { return "test-key", nil }, endpoint: server.URL}
}

func testQuestions() map[string]Question {
	return map[string]Question{
		"match": {Type: "noul", Instructions: "does it match?", Criteria: map[string]string{"true": "yes", "false": "no"}},
	}
}

func TestEvaluateSuccess(t *testing.T) {
	t.Parallel()

	requests := make(chan *http.Request, 1)
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		requests <- r.Clone(r.Context())
		var req struct {
			Model     string              `json:"model"`
			State     json.RawMessage     `json:"state"`
			Questions map[string]Question `json:"questions"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
			return
		}
		if req.Model != Model {
			t.Errorf("unexpected model: %s", req.Model)
		}
		if _, ok := req.Questions["match"]; !ok {
			t.Error("missing question")
		}

		_, err := w.Write([]byte(`{"model":"typesafe/jev-1.13","answers":{"match":{"type":"noul","noul":0.75}},"usage":{"input_tokens":10,"output_tokens":2,"cost":0.00042}}`))
		if err != nil {
			t.Error(err)
		}
	})

	result, err := client.Evaluate(t.Context(), "org-1", json.RawMessage(`{"foo":"bar"}`), testQuestions())

	require.NoError(t, err)
	require.InDelta(t, 0.75, result.Probabilities["match"], 1e-9)
	require.Equal(t, Model, result.Model)
	request := <-requests
	require.Equal(t, http.MethodPost, request.Method)
	require.Equal(t, "Bearer test-key", request.Header.Get("Authorization"))
	require.InDelta(t, 0.00042, result.CostUSD, 1e-12)
	require.Equal(t, 10, result.InputTokens)
	require.Equal(t, 2, result.OutputTokens)
}

func TestEvaluateInvalidInput(t *testing.T) {
	t.Parallel()
	var called atomic.Bool

	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		called.Store(true)
	})

	_, err := client.Evaluate(t.Context(), "org-1", json.RawMessage(`not json`), testQuestions())
	require.Error(t, err)

	_, err = client.Evaluate(t.Context(), "org-1", json.RawMessage(`{}`), nil)
	require.Error(t, err)
	require.False(t, called.Load())
}

func TestEvaluateNonOKStatus(t *testing.T) {
	t.Parallel()

	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("secret-bearing error body"))
	})

	_, err := client.Evaluate(t.Context(), "org-1", json.RawMessage(`{}`), testQuestions())

	require.Error(t, err)
	require.NotContains(t, err.Error(), "secret-bearing")
}

func TestEvaluateModelMismatch(t *testing.T) {
	t.Parallel()

	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"model":"some-other-model","answers":{"match":{"type":"noul","noul":0.5}},"usage":{"input_tokens":1,"output_tokens":1,"cost":0.00021}}`))
	})

	_, err := client.Evaluate(t.Context(), "org-1", json.RawMessage(`{}`), testQuestions())

	require.ErrorContains(t, err, "invalid typesafe response metadata")
}

func TestEvaluateMissingAnswer(t *testing.T) {
	t.Parallel()

	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"model":"typesafe/jev-1.13","answers":{},"usage":{"input_tokens":1,"output_tokens":1,"cost":0.00021}}`))
	})

	_, err := client.Evaluate(t.Context(), "org-1", json.RawMessage(`{}`), testQuestions())

	require.ErrorContains(t, err, "invalid typesafe response metadata")
}

func TestEvaluateOutOfRangeProbability(t *testing.T) {
	t.Parallel()

	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"model":"typesafe/jev-1.13","answers":{"match":{"type":"noul","noul":1.5}},"usage":{"input_tokens":1,"output_tokens":1,"cost":0.00021}}`))
	})

	_, err := client.Evaluate(t.Context(), "org-1", json.RawMessage(`{}`), testQuestions())

	require.ErrorContains(t, err, "invalid typesafe probability")
}

func TestUnavailableEvaluatorReturnsErrUnavailable(t *testing.T) {
	t.Parallel()

	_, err := (Unavailable{}).Evaluate(t.Context(), "org-1", json.RawMessage(`{}`), testQuestions())

	require.ErrorIs(t, err, ErrUnavailable)
}

func TestOpenRouterEvaluateAcceptsBuildSuffixedModel(t *testing.T) {
	t.Parallel()

	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Model string `json:"model"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
			return
		}
		if req.Model != Model {
			t.Errorf("unexpected model: %s", req.Model)
		}

		_, err := w.Write([]byte(`{"model":"typesafe/jev-1.13-20260917","answers":{"match":{"type":"noul","noul":0.4}},"usage":{"input_tokens":5,"output_tokens":1,"cost":0.00021}}`))
		if err != nil {
			t.Error(err)
		}
	})

	result, err := client.Evaluate(t.Context(), "org-1", json.RawMessage(`{}`), testQuestions())

	require.NoError(t, err)
	require.InDelta(t, 0.4, result.Probabilities["match"], 1e-9)
}

func TestOpenRouterEvaluateRejectsUnrelatedModel(t *testing.T) {
	t.Parallel()

	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"model":"some-other-model","answers":{"match":{"type":"noul","noul":0.4}},"usage":{"input_tokens":5,"output_tokens":1,"cost":0.00021}}`))
	})

	_, err := client.Evaluate(t.Context(), "org-1", json.RawMessage(`{}`), testQuestions())

	require.ErrorContains(t, err, "invalid typesafe response metadata")
}

func TestEvaluateRejectsAdjacentModel(t *testing.T) {
	t.Parallel()
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"model":"typesafe/jev-1.130","answers":{"match":{"type":"noul","noul":0.4}},"usage":{"input_tokens":5,"output_tokens":1,"cost":0.1}}`))
	})
	_, err := client.Evaluate(t.Context(), "org-1", json.RawMessage(`{}`), testQuestions())
	require.ErrorContains(t, err, "invalid typesafe response metadata")
}

func TestEvaluateResolvesOrganizationKey(t *testing.T) {
	t.Parallel()
	var called atomic.Bool
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) { called.Store(true) })
	client.resolveKey = func(ctx context.Context, orgID string) (string, error) {
		require.Equal(t, "org-1", orgID)
		return "unset", nil
	}
	_, err := client.Evaluate(t.Context(), "org-1", json.RawMessage(`{}`), testQuestions())
	require.ErrorIs(t, err, ErrUnavailable)
	require.False(t, called.Load())
}

func TestNewUsesOpenRouter(t *testing.T) {
	t.Parallel()
	client := New(http.DefaultClient, nil)
	require.Equal(t, "https://openrouter.ai/api/alpha/decisions", client.endpoint)
}

func TestEvaluateRejectsMissingCost(t *testing.T) {
	t.Parallel()
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"model":"typesafe/jev-1.13","answers":{"match":{"type":"noul","noul":0.4}},"usage":{"input_tokens":5,"output_tokens":1}}`))
	})
	_, err := client.Evaluate(t.Context(), "org-1", json.RawMessage(`{}`), testQuestions())
	require.ErrorContains(t, err, "invalid typesafe response metadata")
}

func TestEvaluateRejectsNegativeCost(t *testing.T) {
	t.Parallel()
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"model":"typesafe/jev-1.13","answers":{"match":{"type":"noul","noul":0.4}},"usage":{"input_tokens":5,"output_tokens":1,"cost":-1}}`))
	})
	_, err := client.Evaluate(t.Context(), "org-1", json.RawMessage(`{}`), testQuestions())
	require.ErrorContains(t, err, "invalid typesafe response metadata")
}

func TestEvaluateRejectsOversizedResponse(t *testing.T) {
	t.Parallel()
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"padding":"` + strings.Repeat("a", 1<<20) + `"}`))
	})
	_, err := client.Evaluate(t.Context(), "org-1", json.RawMessage(`{}`), testQuestions())
	require.ErrorContains(t, err, "typesafe response exceeds limit")
}

func TestEvaluateRejectsMalformedJSON(t *testing.T) {
	t.Parallel()
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{not json`))
	})
	_, err := client.Evaluate(t.Context(), "org-1", json.RawMessage(`{}`), testQuestions())
	require.ErrorContains(t, err, "invalid typesafe response JSON")
}

func TestEvaluateRejectsNegativeTokens(t *testing.T) {
	t.Parallel()
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"model":"typesafe/jev-1.13","answers":{"match":{"type":"noul","noul":0.4}},"usage":{"input_tokens":-1,"output_tokens":1,"cost":0.1}}`))
	})
	_, err := client.Evaluate(t.Context(), "org-1", json.RawMessage(`{}`), testQuestions())
	require.ErrorContains(t, err, "invalid typesafe response metadata")
}

func TestEvaluateRejectsNonNoulAnswerType(t *testing.T) {
	t.Parallel()
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"model":"typesafe/jev-1.13","answers":{"match":{"type":"other","noul":0.4}},"usage":{"input_tokens":5,"output_tokens":1,"cost":0.1}}`))
	})
	_, err := client.Evaluate(t.Context(), "org-1", json.RawMessage(`{}`), testQuestions())
	require.ErrorContains(t, err, "invalid typesafe probability")
}

func TestEvaluateRejectsNullProbability(t *testing.T) {
	t.Parallel()
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"model":"typesafe/jev-1.13","answers":{"match":{"type":"noul","noul":null}},"usage":{"input_tokens":5,"output_tokens":1,"cost":0.1}}`))
	})
	_, err := client.Evaluate(t.Context(), "org-1", json.RawMessage(`{}`), testQuestions())
	require.ErrorContains(t, err, "invalid typesafe probability")
}
