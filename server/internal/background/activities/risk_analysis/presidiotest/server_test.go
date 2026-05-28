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
	require.Contains(t, ids, "pii.email_address")
	for _, f := range results[0] {
		if f.RuleID == "pii.email_address" {
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

	require.Contains(t, ruleIDs(results[0]), "pii.credit_card")
	require.Contains(t, ruleIDs(results[1]), "pii.credit_card")
	require.NotContains(t, ruleIDs(results[2]), "pii.credit_card")
}

func TestMockServer_DetectsPhoneNumber(t *testing.T) {
	t.Parallel()
	_, client := newClient(t)

	results, err := client.AnalyzeBatch(t.Context(), []string{
		"call me at 425-882-8080 thanks",
	}, nil, nil)
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Contains(t, ruleIDs(results[0]), "pii.phone_number")
}

func TestMockServer_DetectsPersonName(t *testing.T) {
	t.Parallel()
	_, client := newClient(t)

	results, err := client.AnalyzeBatch(t.Context(), []string{
		"My name is John Smith and I'm here",
	}, nil, nil)
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Contains(t, ruleIDs(results[0]), "pii.person")
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
		require.NotEqual(t, "pii.phone_number", f.RuleID, "version string should not match phone regex")
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
		require.NotEqual(t, "pii.credit_card", f.RuleID)
	}
}

// Note: the entity filter list on AnalyzeBatch is sent verbatim to Presidio,
// which still speaks UPPER_SNAKE entity types. Only the rule_id written to
// risk_results is normalized to snake_case by ConvertFindings.
func TestMockServer_EntityFilterRespected(t *testing.T) {
	t.Parallel()
	_, client := newClient(t)

	results, err := client.AnalyzeBatch(t.Context(), []string{
		"call 425-882-8080 or email alice@example.com",
	}, []string{"EMAIL_ADDRESS"}, nil)
	require.NoError(t, err)
	require.Len(t, results, 1)

	ids := ruleIDs(results[0])
	require.Contains(t, ids, "pii.email_address")
	require.NotContains(t, ids, "pii.phone_number")
}

func TestDefaultDetector_CoversSupportedEntityCatalog(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name   string
		entity string
		text   string
	}{
		{name: "credit card", entity: "CREDIT_CARD", text: "Card 4111111111111111"},
		{name: "crypto", entity: "CRYPTO", text: "Wallet 1BoatSLRHtKNngkdXEeobR76b53LETtpyT"},
		{name: "date time", entity: "DATE_TIME", text: "Meet me on 2026-05-28 13:45"},
		{name: "domain name", entity: "DOMAIN_NAME", text: "Host example.org is healthy"},
		{name: "email", entity: "EMAIL_ADDRESS", text: "Email alice@example.com"},
		{name: "iban", entity: "IBAN_CODE", text: "IBAN GB33BUKB20201555555555"},
		{name: "ip", entity: "IP_ADDRESS", text: "IP 203.0.113.42 responded"},
		{name: "location", entity: "LOCATION", text: "Traveling to New York tomorrow"},
		{name: "mac", entity: "MAC_ADDRESS", text: "MAC 00:1B:44:11:3A:B7"},
		{name: "medical license", entity: "MEDICAL_LICENSE", text: "License MD123456"},
		{name: "nrp", entity: "NRP", text: "The patient is Christian"},
		{name: "person", entity: "PERSON", text: "John Smith checked in"},
		{name: "phone", entity: "PHONE_NUMBER", text: "Call 425-882-8080"},
		{name: "sg nric", entity: "SG_NRIC_FIN", text: "NRIC S1234567D"},
		{name: "uk nhs", entity: "UK_NHS", text: "NHS number 485 777 3456"},
		{name: "url", entity: "URL", text: "Visit https://example.com/path"},
		{name: "us bank", entity: "US_BANK_NUMBER", text: "Bank account 123456789012"},
		{name: "us driver license", entity: "US_DRIVER_LICENSE", text: "Driver license D1234567"},
		{name: "us itin", entity: "US_ITIN", text: "ITIN 912782345"},
		{name: "us passport", entity: "US_PASSPORT", text: "Passport 123456789"},
		{name: "us ssn", entity: "US_SSN", text: "SSN 123-45-6789"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			results := presidiotest.DefaultDetector(tc.text, []string{tc.entity})
			require.NotEmpty(t, results)
			require.Equal(t, tc.entity, results[0].EntityType)
		})
	}
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
			if f.RuleID == "pii.email_address" {
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
	require.Equal(t, "pii.custom_entity", results[0][0].RuleID)
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

	// The Presidio client fans out one HTTP request per text — failures
	// isolate to a single message and there is no internal sub-batching.
	require.Equal(t, int64(len(messages)), server.AnalyzeRequestCount())
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
