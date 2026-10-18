package typesafedecisions

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func testClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		handler(w, r)
	}))
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
	var request *http.Request
	select {
	case request = <-requests:
	default:
		t.Fatal("evaluation succeeded without issuing a request")
	}
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
	var attempts atomic.Int32

	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("secret-bearing error body"))
	})

	_, err := client.Evaluate(t.Context(), "org-1", json.RawMessage(`{}`), testQuestions())

	require.Error(t, err)
	require.NotContains(t, err.Error(), "secret-bearing")
	require.EqualValues(t, 1, attempts.Load())
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

func TestEvaluateMissingDependencies(t *testing.T) {
	t.Parallel()
	client := New(nil, nil)
	_, err := client.Evaluate(t.Context(), "org", json.RawMessage(`{}`), testQuestions())
	require.ErrorIs(t, err, ErrUnavailable)
	client.resolveKey = func(context.Context, string) (string, error) {
		t.Fatal("resolver must not be called without transport")
		return "", nil
	}
	_, err = client.Evaluate(t.Context(), "org", json.RawMessage(`{}`), testQuestions())
	require.ErrorIs(t, err, ErrUnavailable)
	client.httpClient, client.resolveKey = http.DefaultClient, nil
	_, err = client.Evaluate(t.Context(), "org", json.RawMessage(`{}`), testQuestions())
	require.ErrorIs(t, err, ErrUnavailable)
}

func TestEvaluateRejectsMissingProbability(t *testing.T) {
	t.Parallel()
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"model":"typesafe/jev-1.13","answers":{"match":{"type":"noul"}},"usage":{"input_tokens":5,"output_tokens":1,"cost":0.1}}`))
	})
	_, err := client.Evaluate(t.Context(), "org", json.RawMessage(`{}`), testQuestions())
	require.ErrorContains(t, err, "invalid typesafe probability")
}

func TestEvaluateAcceptsZeroProbability(t *testing.T) {
	t.Parallel()
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"model":"typesafe/jev-1.13","answers":{"match":{"type":"noul","noul":0}},"usage":{"input_tokens":5,"output_tokens":1,"cost":0.1}}`))
	})
	result, err := client.Evaluate(t.Context(), "org", json.RawMessage(`{}`), testQuestions())
	require.NoError(t, err)
	require.Zero(t, result.Probabilities["match"])
}

func TestEvaluateHonorsParentDeadline(t *testing.T) {
	t.Parallel()
	release := make(chan struct{})
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-release:
		}
	})
	defer close(release)
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	_, err := client.Evaluate(ctx, "org", json.RawMessage(`{}`), testQuestions())
	require.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestEvaluateRedactsSDKDecodeErrors(t *testing.T) {
	t.Parallel()
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"model":"typesafe/jev-1.13","answers":{"match":{"type":"noul","noul":0.4}},"usage":{"input_tokens":"private-response-value","output_tokens":1,"cost":0.1}}`))
	})
	_, err := client.Evaluate(t.Context(), "org", json.RawMessage(`{}`), testQuestions())
	require.Error(t, err)
	require.NotContains(t, err.Error(), "private-response-value")
}

func TestEvaluatePreservesState(t *testing.T) {
	t.Parallel()
	for _, state := range []string{`"plain text"`, `{"id":9007199254740993}`, `[9007199254740993,"text"]`} {
		t.Run(state, func(t *testing.T) {
			t.Parallel()
			client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				var body struct {
					State     json.RawMessage     `json:"state"`
					Questions map[string]Question `json:"questions"`
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
					return
				}
				if string(body.State) != state {
					t.Errorf("state changed: %s", body.State)
				}
				if body.Questions["match"].Instructions != "does it match?" || body.Questions["match"].Criteria["true"] != "yes" || body.Questions["match"].Criteria["false"] != "no" {
					t.Error("question changed")
				}
				_, _ = w.Write([]byte(`{"model":"typesafe/jev-1.13","answers":{"match":{"type":"noul","noul":0.4}},"usage":{"input_tokens":5,"output_tokens":1,"cost":0.1}}`))
			})
			_, err := client.Evaluate(t.Context(), "org", json.RawMessage(state), testQuestions())
			require.NoError(t, err)
		})
	}
}

func TestEvaluateProviderThrottlingReturnsErrorWithoutRetry(t *testing.T) {
	t.Parallel()
	var attempts atomic.Int32
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.Header().Set("Retry-After", "60")
		w.WriteHeader(http.StatusTooManyRequests)
	})
	_, err := client.Evaluate(t.Context(), "org-1", json.RawMessage(`{}`), testQuestions())
	require.ErrorContains(t, err, "typesafe HTTP status 429")
	statusErr, ok := errors.AsType[*StatusError](err)
	require.True(t, ok, "callers can branch on the status")
	require.Equal(t, http.StatusTooManyRequests, statusErr.StatusCode)
	require.EqualValues(t, 1, attempts.Load())
}

func TestEvaluateRecognizesContextLengthErrorWithoutExposingBody(t *testing.T) {
	t.Parallel()
	var attempts atomic.Int32
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"code":400,"message":"private echoed evidence","metadata":{"error_type":"context_length_exceeded"}}}`))
	})
	_, err := client.Evaluate(t.Context(), "org-1", json.RawMessage(`{}`), testQuestions())
	require.ErrorIs(t, err, ErrContextLengthExceeded)
	require.NotContains(t, err.Error(), "private echoed evidence")
	require.EqualValues(t, 1, attempts.Load(), "the client itself must not retry")
}

func TestContextLengthErrorRequiresExactStructuredCode(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{
		`{"error":{"code":"context_length_exceeded"}}`,
		`{"error":{"code":400,"metadata":{"error_type":"context_length_exceeded"}}}`,
	} {
		require.True(t, isContextLengthError([]byte(raw)), raw)
	}
	for _, raw := range []string{
		`{"error":{"code":400,"message":"context_length_exceeded"}}`,
		`{"error":{"code":400,"metadata":{"error_type":"token_limit_exceeded"}}}`,
		`{"error":{"metadata":{"error_type":"rate_limit_exceeded"}}}`,
		`{"error":{"metadata":{"error_type":"max_tokens_exceeded"}}}`,
		`{"error":{"metadata":{"error_type":"context_length_exceeded_extra"}}}`,
		`{"error":{"code":"context_length_exceeded","metadata":{"error_type":"invalid_request"}}}`,
		`{"error":{"code":400}}`,
		`{"error":{"code":400,"message":"max_tokens_exceeded"}}`,
		`{"error":{"code":400,"message":"HTTP 400: {\"detail\":{\"error_type\":\"token_limit_exceeded\"}}"}}`,
		`{"error":{"code":400,"message":"HTTP 400: {\"detail\":{\"error_type\":\"max_tokens_exceeded_extra\"}}"}}`,
		`{"error":{"code":400,"message":"HTTP 400: malformed max_tokens_exceeded"}}`,
		`{"error":{"code":429,"message":"HTTP 400: {\"detail\":{\"error_type\":\"max_tokens_exceeded\"}}"}}`,
		`not json`,
	} {
		require.False(t, isContextLengthError([]byte(raw)), raw)
	}
}

func TestEvaluateOversizedErrorDoesNotTriggerContextRetry(t *testing.T) {
	t.Parallel()
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"code":"context_length_exceeded","message":"` + strings.Repeat("x", 64<<10) + `"}}`))
	})
	_, err := client.Evaluate(t.Context(), "org-1", json.RawMessage(`{}`), testQuestions())
	require.ErrorContains(t, err, "HTTP status 400")
	require.NotErrorIs(t, err, ErrContextLengthExceeded)
}

func TestEvaluateRecognizesLiveJevContextOverflow(t *testing.T) {
	t.Parallel()
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		// Exact error shape observed from a synthetic oversized Decisions call.
		_, _ = w.Write([]byte(`{"error":{"message":"HTTP 400: {\"detail\":{\"error_type\":\"max_tokens_exceeded\"}}","code":400}}`))
	})
	_, err := client.Evaluate(t.Context(), "org-1", json.RawMessage(`{}`), testQuestions())
	require.ErrorIs(t, err, ErrContextLengthExceeded)
}
