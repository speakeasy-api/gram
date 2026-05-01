package risk_analysis_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	risk_analysis "github.com/speakeasy-api/gram/server/internal/background/activities/risk_analysis"
)

// --- Real positives: PII that should be detected ---

func TestPresidio_DetectsPersonName(t *testing.T) {
	t.Parallel()
	client := infra.NewPresidioClient(t)
	results, err := client.AnalyzeBatch(t.Context(), []string{
		"My name is John Smith and I live in New York",
	}, nil, nil)
	require.NoError(t, err)
	require.Len(t, results, 1)

	findings := results[0]
	ruleIDs := findingRuleIDs(findings)
	assert.Contains(t, ruleIDs, "PERSON", "expected PERSON entity")
}

func TestPresidio_DetectsEmail(t *testing.T) {
	t.Parallel()
	client := infra.NewPresidioClient(t)
	results, err := client.AnalyzeBatch(t.Context(), []string{
		"Please contact me at john.smith@acmecorp.com for details",
	}, nil, nil)
	require.NoError(t, err)
	require.Len(t, results, 1)

	findings := results[0]
	ruleIDs := findingRuleIDs(findings)
	assert.Contains(t, ruleIDs, "EMAIL_ADDRESS", "expected EMAIL_ADDRESS entity")

	for _, f := range findings {
		if f.RuleID == "EMAIL_ADDRESS" {
			assert.Equal(t, "john.smith@acmecorp.com", f.Match)
			assert.InDelta(t, 1.0, f.Confidence, 0.1)
			assert.Equal(t, "presidio", f.Source)
		}
	}
}

func TestPresidio_BatchResultsMapBackToInputIndexes(t *testing.T) {
	t.Parallel()
	client := infra.NewPresidioClient(t)

	messages := make([]string, 75)
	emails := make([]string, len(messages))
	for i := range messages {
		emails[i] = fmt.Sprintf("remap%03d@example.com", i)
		messages[i] = fmt.Sprintf("message %03d contact %s end", i, emails[i])
	}

	results, err := client.AnalyzeBatch(t.Context(), messages, []string{"EMAIL_ADDRESS"}, nil)
	require.NoError(t, err)
	require.Len(t, results, len(messages))

	for i, findings := range results {
		var got string
		for _, f := range findings {
			if f.RuleID == "EMAIL_ADDRESS" {
				got = f.Match
				break
			}
		}
		assert.Equal(t, emails[i], got, "message %d mapped to wrong finding", i)
	}
}

func TestPresidio_DetectsCreditCard(t *testing.T) {
	t.Parallel()
	client := infra.NewPresidioClient(t)
	results, err := client.AnalyzeBatch(t.Context(), []string{
		"My credit card number is 4111111111111111",
		"Card: 5500-0000-0000-0004",
		"Amex 371449635398431",
	}, nil, nil)
	require.NoError(t, err)
	require.Len(t, results, 3)

	for i, findings := range results {
		ruleIDs := findingRuleIDs(findings)
		assert.Contains(t, ruleIDs, "CREDIT_CARD", "expected CREDIT_CARD for message %d", i)
	}
}

func TestPresidio_DetectsPhoneNumber(t *testing.T) {
	t.Parallel()
	client := infra.NewPresidioClient(t)
	results, err := client.AnalyzeBatch(t.Context(), []string{
		"Please call my phone number 425-882-8080 to confirm the appointment",
		"My phone is +44 20 7946 0958",
	}, nil, nil)
	require.NoError(t, err)
	require.Len(t, results, 2)

	// Phone detection varies by format; check at least one is detected
	anyDetected := false
	for _, findings := range results {
		ruleIDs := findingRuleIDs(findings)
		for _, id := range ruleIDs {
			if id == "PHONE_NUMBER" {
				anyDetected = true
			}
		}
	}
	assert.True(t, anyDetected, "expected at least one PHONE_NUMBER detection")
}

func TestPresidio_DetectsMultiplePIIInSingleMessage(t *testing.T) {
	t.Parallel()
	client := infra.NewPresidioClient(t)
	results, err := client.AnalyzeBatch(t.Context(), []string{
		"Patient Jane Doe (jane.doe@hospital.org) has credit card 4111111111111111. Call 555-123-4567.",
	}, nil, nil)
	require.NoError(t, err)
	require.Len(t, results, 1)

	findings := results[0]
	ruleIDs := findingRuleIDs(findings)
	assert.Contains(t, ruleIDs, "PERSON")
	assert.Contains(t, ruleIDs, "EMAIL_ADDRESS")
	assert.Contains(t, ruleIDs, "CREDIT_CARD")
}

// --- False positives: text that should NOT be flagged ---

func TestPresidio_NoFalsePositiveOnVersionNumbers(t *testing.T) {
	t.Parallel()
	client := infra.NewPresidioClient(t)
	results, err := client.AnalyzeBatch(t.Context(), []string{
		"Version 1.234.567.890 was released",
		"API v2.0.0-beta.1 is now available",
		"Build number: 20260423-001",
	}, nil, nil)
	require.NoError(t, err)
	for i, findings := range results {
		highConfidence := filterHighConfidence(findings, 0.7)
		assert.Empty(t, highConfidence, "expected no high-confidence PII findings for version string message %d, got: %v", i, highConfidence)
	}
}

func TestPresidio_NoFalsePositiveOnUUIDs(t *testing.T) {
	t.Parallel()
	client := infra.NewPresidioClient(t)
	results, err := client.AnalyzeBatch(t.Context(), []string{
		"Transaction ID: 550e8400-e29b-41d4-a716-446655440000",
		"Session: a1b2c3d4-e5f6-7890-abcd-ef1234567890",
	}, nil, nil)
	require.NoError(t, err)
	for i, findings := range results {
		highConfidence := filterHighConfidence(findings, 0.7)
		assert.Empty(t, highConfidence, "expected no high-confidence PII findings for UUID message %d, got: %v", i, highConfidence)
	}
}

func TestPresidio_NoFalsePositiveOnCodeSnippets(t *testing.T) {
	t.Parallel()
	client := infra.NewPresidioClient(t)
	results, err := client.AnalyzeBatch(t.Context(), []string{
		`func main() { fmt.Println("hello world") }`,
		`SELECT * FROM users WHERE id = 12345`,
		`const API_ENDPOINT = "https://api.example.com/v1"`,
	}, nil, nil)
	require.NoError(t, err)
	for i, findings := range results {
		piiFindings := filterBySource(findings, "presidio")
		highConfidence := filterHighConfidence(piiFindings, 0.8)
		for _, f := range highConfidence {
			// URL detections in code are expected and OK
			if f.RuleID != "URL" {
				t.Errorf("message %d: unexpected high-confidence PII %q (%s) in code snippet", i, f.RuleID, f.Match)
			}
		}
	}
}

func TestPresidio_CleanMessagesProduceNoFindings(t *testing.T) {
	t.Parallel()
	client := infra.NewPresidioClient(t)
	results, err := client.AnalyzeBatch(t.Context(), []string{
		"The deployment completed successfully.",
		"Please review the pull request when you get a chance.",
		"We should refactor the authentication module next sprint.",
		"The API response time improved by 30% after the optimization.",
	}, nil, nil)
	require.NoError(t, err)
	for i, findings := range results {
		highConfidence := filterHighConfidence(findings, 0.7)
		assert.Empty(t, highConfidence, "expected no high-confidence findings for clean message %d", i)
	}
}

// --- Combined scanner test: gitleaks + presidio ---

func TestCombinedScanners_BothSourcesAppear(t *testing.T) {
	t.Parallel()
	client := infra.NewPresidioClient(t)
	scanner := risk_analysis.NewScanner()

	// Message with both a secret (AWS key) and PII (email)
	content := "Here is my AWS key: AKIAIOSFODNN7REALKEY and my email is alice@example.com"

	gitleaksFindings, err := scanner.Scan(content)
	require.NoError(t, err)

	presidioResults, err := client.AnalyzeBatch(t.Context(), []string{content}, nil, nil)
	require.NoError(t, err)

	allFindings := slices.Concat(gitleaksFindings, presidioResults[0])

	sources := map[string]bool{}
	for _, f := range allFindings {
		sources[f.Source] = true
	}
	assert.True(t, sources["gitleaks"], "expected gitleaks findings")
	assert.True(t, sources["presidio"], "expected presidio findings")
}

// --- Stress test ---

func TestPresidio_StressBatch(t *testing.T) {
	t.Parallel()
	client := infra.NewPresidioClient(t)

	messages := make([]string, 200)
	for i := range messages {
		switch i % 4 {
		case 0:
			messages[i] = fmt.Sprintf("User %d email: user%d@company.com, card 4111111111111111", i, i)
		case 1:
			messages[i] = fmt.Sprintf("Build %d passed. Deploy to production.", i)
		case 2:
			messages[i] = fmt.Sprintf("Patient record for person_%d: diagnosis pending", i)
		case 3:
			messages[i] = fmt.Sprintf("API key: sk_test_%032d", i)
		}
	}

	start := time.Now()
	results, err := client.AnalyzeBatch(t.Context(), messages, nil, nil)
	elapsed := time.Since(start)
	require.NoError(t, err)
	require.Len(t, results, 200)

	// Should complete in reasonable time (< 60s for 200 messages sequentially)
	assert.Less(t, elapsed, 60*time.Second, "stress test took too long: %s", elapsed)

	// Count findings by type
	counts := map[string]int{}
	for _, findings := range results {
		for _, f := range findings {
			counts[f.RuleID]++
		}
	}
	t.Logf("Stress test completed in %s. Finding counts: %v", elapsed, counts)

	// Messages with emails should have findings
	assert.Positive(t, counts["EMAIL_ADDRESS"], "expected some EMAIL_ADDRESS detections")
}

// --- Helpers ---

func findingRuleIDs(findings []risk_analysis.Finding) []string {
	ids := make([]string, len(findings))
	for i, f := range findings {
		ids[i] = f.RuleID
	}
	return ids
}

func filterHighConfidence(findings []risk_analysis.Finding, threshold float64) []risk_analysis.Finding {
	var out []risk_analysis.Finding
	for _, f := range findings {
		if f.Confidence >= threshold {
			out = append(out, f)
		}
	}
	return out
}

func filterBySource(findings []risk_analysis.Finding, source string) []risk_analysis.Finding {
	var out []risk_analysis.Finding
	for _, f := range findings {
		if strings.EqualFold(f.Source, source) {
			out = append(out, f)
		}
	}
	return out
}
