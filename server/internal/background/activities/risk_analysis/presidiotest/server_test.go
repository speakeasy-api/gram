package presidiotest_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	risk_analysis "github.com/speakeasy-api/gram/server/internal/background/activities/risk_analysis"
	"github.com/speakeasy-api/gram/server/internal/background/activities/risk_analysis/presidiotest"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func newClient(t *testing.T) (*presidiotest.MockServer, *risk_analysis.PresidioClient) {
	t.Helper()
	server := presidiotest.NewMockServer(nil)
	t.Cleanup(server.Close)
	client := risk_analysis.NewPresidioClient(
		server.URL(),
		testenv.NewTracerProvider(t),
		testenv.NewMeterProvider(t),
		testenv.NewLogger(t),
	)
	return server, client
}

func ruleIDs(findings []risk_analysis.Finding) []string {
	out := make([]string, len(findings))
	for i, f := range findings {
		out[i] = f.RuleID
	}
	return out
}

func TestMockServer_DetectsEmail(t *testing.T) {
	t.Parallel()
	_, client := newClient(t)

	results, err := client.AnalyzeBatch(t.Context(), []string{
		"contact me at john.smith@acmecorp.com",
	}, nil, nil)
	require.NoError(t, err)
	require.Len(t, results, 1)

	ids := ruleIDs(results[0])
	require.Contains(t, ids, "EMAIL_ADDRESS")
	for _, f := range results[0] {
		if f.RuleID == "EMAIL_ADDRESS" {
			require.Equal(t, "john.smith@acmecorp.com", f.Match)
			require.Equal(t, "presidio", f.Source)
		}
	}
}

func TestMockServer_DetectsCreditCardWithLuhnCheck(t *testing.T) {
	t.Parallel()
	_, client := newClient(t)

	results, err := client.AnalyzeBatch(t.Context(), []string{
		"My credit card is 4111111111111111",
		"Card: 5500-0000-0000-0004",
		"Bogus card 4111111111111112 not detected",
	}, nil, nil)
	require.NoError(t, err)
	require.Len(t, results, 3)

	require.Contains(t, ruleIDs(results[0]), "CREDIT_CARD")
	require.Contains(t, ruleIDs(results[1]), "CREDIT_CARD")
	require.NotContains(t, ruleIDs(results[2]), "CREDIT_CARD")
}

func TestMockServer_DetectsPhoneNumber(t *testing.T) {
	t.Parallel()
	_, client := newClient(t)

	results, err := client.AnalyzeBatch(t.Context(), []string{
		"call me at 425-882-8080 thanks",
	}, nil, nil)
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Contains(t, ruleIDs(results[0]), "PHONE_NUMBER")
}

func TestMockServer_DetectsPersonName(t *testing.T) {
	t.Parallel()
	_, client := newClient(t)

	results, err := client.AnalyzeBatch(t.Context(), []string{
		"My name is John Smith and I'm here",
	}, nil, nil)
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Contains(t, ruleIDs(results[0]), "PERSON")
}

func TestMockServer_NoFalsePositiveOnVersionString(t *testing.T) {
	t.Parallel()
	_, client := newClient(t)

	results, err := client.AnalyzeBatch(t.Context(), []string{
		"Version 1.234.567.890 was released",
	}, nil, nil)
	require.NoError(t, err)
	require.Len(t, results, 1)

	for _, f := range results[0] {
		require.NotEqual(t, "PHONE_NUMBER", f.RuleID, "version string should not match phone regex")
	}
}

func TestMockServer_NoFalsePositiveOnUUID(t *testing.T) {
	t.Parallel()
	_, client := newClient(t)

	results, err := client.AnalyzeBatch(t.Context(), []string{
		"Transaction: 550e8400-e29b-41d4-a716-446655440000",
	}, nil, nil)
	require.NoError(t, err)
	require.Len(t, results, 1)

	for _, f := range results[0] {
		require.NotEqual(t, "CREDIT_CARD", f.RuleID)
	}
}

func TestMockServer_EntityFilterRespected(t *testing.T) {
	t.Parallel()
	_, client := newClient(t)

	results, err := client.AnalyzeBatch(t.Context(), []string{
		"call 425-882-8080 or email alice@example.com",
	}, []string{"EMAIL_ADDRESS"}, nil)
	require.NoError(t, err)
	require.Len(t, results, 1)

	ids := ruleIDs(results[0])
	require.Contains(t, ids, "EMAIL_ADDRESS")
	require.NotContains(t, ids, "PHONE_NUMBER")
}

func TestMockServer_BatchResultsMapBackToInputIndexes(t *testing.T) {
	t.Parallel()
	_, client := newClient(t)

	const n = 75
	emails := make([]string, n)
	messages := make([]string, n)
	for i := range messages {
		emails[i] = "user" + strconv.Itoa(i) + "@example.com"
		messages[i] = "message " + strconv.Itoa(i) + " contact " + emails[i] + " end"
	}

	results, err := client.AnalyzeBatch(t.Context(), messages, []string{"EMAIL_ADDRESS"}, nil)
	require.NoError(t, err)
	require.Len(t, results, n)

	for i, findings := range results {
		var got string
		for _, f := range findings {
			if f.RuleID == "EMAIL_ADDRESS" {
				got = f.Match
				break
			}
		}
		require.Equal(t, emails[i], got, "message %d mapped to wrong finding", i)
	}
}

func TestMockServer_CustomDetectorOverride(t *testing.T) {
	t.Parallel()
	server, client := newClient(t)

	server.SetDetector(func(text string, _ []string) []presidiotest.Result {
		return []presidiotest.Result{{
			EntityType: "CUSTOM_ENTITY",
			Start:      0,
			End:        len([]rune(text)),
			Score:      1.0,
		}}
	})

	results, err := client.AnalyzeBatch(t.Context(), []string{"anything"}, nil, nil)
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Len(t, results[0], 1)
	require.Equal(t, "CUSTOM_ENTITY", results[0][0].RuleID)
	require.Equal(t, "anything", results[0][0].Match)
}

func TestMockServer_AnalyzeRequestCount(t *testing.T) {
	t.Parallel()
	server, client := newClient(t)

	messages := make([]string, 75)
	for i := range messages {
		messages[i] = "msg " + strconv.Itoa(i)
	}

	_, err := client.AnalyzeBatch(t.Context(), messages, nil, nil)
	require.NoError(t, err)

	// The Presidio client no longer sub-batches: it issues exactly one
	// /analyze request per call regardless of input size. Fan-out (and
	// therefore request count) is the orchestrator's responsibility.
	require.Equal(t, int64(1), server.AnalyzeRequestCount())
}

func TestMockServer_HealthEndpointReturnsOK(t *testing.T) {
	t.Parallel()
	server := presidiotest.NewMockServer(nil)
	t.Cleanup(server.Close)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL()+"/health", nil)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	require.Equal(t, http.StatusOK, resp.StatusCode)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Contains(t, string(body), "Presidio Analyzer")
}

func TestMockServer_ScoreThresholdFiltersResults(t *testing.T) {
	t.Parallel()
	server := presidiotest.NewMockServer(nil)
	t.Cleanup(server.Close)

	// PHONE_NUMBER scores 0.75 in the default detector; threshold 0.9 drops it.
	high := postAnalyze(t, server.URL(), `{"text":["call 425-882-8080"],"language":"en","score_threshold":0.9}`)
	require.Len(t, high, 1)
	for _, r := range high[0] {
		require.NotEqual(t, "PHONE_NUMBER", r.EntityType)
	}

	low := postAnalyze(t, server.URL(), `{"text":["call 425-882-8080"],"language":"en","score_threshold":0.5}`)
	require.Len(t, low, 1)
	hasPhone := false
	for _, r := range low[0] {
		if r.EntityType == "PHONE_NUMBER" {
			hasPhone = true
		}
	}
	require.True(t, hasPhone)
}

func postAnalyze(t *testing.T, baseURL, body string) [][]presidiotest.Result {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, baseURL+"/analyze", bytes.NewReader([]byte(body)))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var out [][]presidiotest.Result
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	return out
}
