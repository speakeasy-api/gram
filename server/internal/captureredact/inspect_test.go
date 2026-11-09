package captureredact

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/scanners"
	"github.com/speakeasy-api/gram/server/internal/scanners/gitleaks"
)

func TestInspectTextPreservesOriginalPlaintextFindings(t *testing.T) {
	t.Parallel()
	text := "結果 🐕\nMISE_GITHUB_TOKEN=" + syntheticPAT + "\nagain: " + syntheticPAT
	scanner := gitleaks.NewScanner()
	want, err := scanner.Scan(t.Context(), text)
	require.NoError(t, err)
	want.Findings = filterTestFindings(want.Findings)
	got, err := NewWithScanner(scanner).InspectText(t.Context(), text)
	require.NoError(t, err)
	require.Equal(t, want, got)
}

func filterTestFindings(findings []scanners.Finding) []scanners.Finding {
	out := make([]scanners.Finding, 0, len(findings))
	for _, finding := range findings {
		if RedactsRule(finding.RuleID) {
			out = append(out, finding)
		}
	}
	return out
}

func TestInspectTextFindsEscapedAndNestedJSONCredentials(t *testing.T) {
	t.Parallel()
	escaped := `{"result":"\u0067hp_` + strings.TrimPrefix(syntheticPAT, "ghp_") + `"}`
	oneLevel, err := json.Marshal(map[string]string{"arguments": escaped})
	require.NoError(t, err)
	twoLevels, err := json.Marshal(map[string]string{"tool_result": string(oneLevel)})
	require.NoError(t, err)
	for _, input := range []string{escaped, string(oneLevel), string(twoLevels)} {
		t.Run(input[:min(len(input), 25)], func(t *testing.T) {
			t.Parallel()
			got, err := shared().InspectText(t.Context(), input)
			require.NoError(t, err)
			require.True(t, got.Completed)
			require.Len(t, got.Findings, 1)
			finding := got.Findings[0]
			require.Equal(t, syntheticPAT, finding.Match)
			require.Equal(t, "capture.decoded_json", finding.Field, "decoded matches need the separate field marker")
			require.Regexp(t, `^\$strings\[\d+\]$`, finding.Path)
			redacted := RedactText(t.Context(), input)
			require.NotContains(t, redacted, strings.TrimPrefix(syntheticPAT, "ghp_"))
		})
	}
}

func TestInspectTextDoesNotDuplicateLiteralAndEscapedOccurrences(t *testing.T) {
	t.Parallel()
	escaped := `\u0067hp_` + strings.TrimPrefix(syntheticPAT, "ghp_")
	input := `{"literal":"` + syntheticPAT + `","encoded":"` + escaped + `","second":"` + syntheticPAT + `","other":"` + escaped + `"}`
	got, err := shared().InspectText(t.Context(), input)
	require.NoError(t, err)
	require.Len(t, got.Findings, 4)
	literal, decoded := 0, 0
	for _, finding := range got.Findings {
		if finding.Field == DecodedJSONField {
			decoded++
		} else {
			literal++
			require.Equal(t, syntheticPAT, input[finding.StartPos:finding.EndPos])
		}
	}
	require.Equal(t, 2, literal)
	require.Equal(t, 2, decoded)
}

func TestInspectTextPreservesLiteralOffsetsAcrossJSONEscapes(t *testing.T) {
	t.Parallel()
	input := `{"text":"\uD83D\uDC15\n\"quoted\" ` + syntheticPAT + `"}`
	got, err := shared().InspectText(t.Context(), input)
	require.NoError(t, err)
	require.Len(t, got.Findings, 1)
	require.Empty(t, got.Findings[0].Field)
	require.Equal(t, strings.Index(input, syntheticPAT), got.Findings[0].StartPos)
	require.Equal(t, syntheticPAT, input[got.Findings[0].StartPos:got.Findings[0].EndPos])
}

func TestJSONInspectionAndRedactionKeepAWSCrossFieldContext(t *testing.T) {
	t.Parallel()
	// Synthetic credential pair, shared with the detector's format fixtures.
	const accessID = "ASIAZ2XY3WNBQR5TUVWX"
	const secret = "wJalrXUtnFEMIbKp7MDoRZfiCYqTvHgNsQ8xLcWd"
	input := `{"AccessKeyId":"` + accessID + `","SecretAccessKey":"` + secret + `"}`
	wrapped, err := json.Marshal(map[string]string{"arguments": input})
	require.NoError(t, err)
	for _, text := range []string{input, string(wrapped)} {
		t.Run(text[:min(len(text), 25)], func(t *testing.T) {
			t.Parallel()
			got, err := shared().InspectText(t.Context(), text)
			require.NoError(t, err)
			require.Len(t, got.Findings, 1)
			require.Equal(t, gitleaks.SecretAccessKeyRuleID, got.Findings[0].RuleID)
			require.Equal(t, secret, got.Findings[0].Match)
			redacted := RedactText(t.Context(), text)
			require.NotContains(t, redacted, secret)
			require.Contains(t, redacted, accessID)
			require.True(t, json.Valid([]byte(redacted)))
		})
	}
}

func TestJSONInspectionAndRedactionDecodeAWSContext(t *testing.T) {
	t.Parallel()
	// Synthetic values mirror the detector's format fixtures.
	const accessID = "ASIAZ2XY3WNBQR5TUVWX"
	const secret = "wJalrXUtnFEMIbKp7MDoRZfiCYqTvHgNsQ8xLcWd"
	const session = "9x34WiYVTCAnxboiPBmqNrCFKD/3tyqFMmVo8mc0cNlyFFd93N5Z/o8gIIyX0hOWxvWRp/dO9Smp" +
		"CVXShsKReY3ef4sMrpkDz2IbZR9eAQyIioVhzt6O9p894tYWerLw9pGMezSLJLyNJ4DQO/A2CU2e8QsY1Buamkhl"
	cases := []struct {
		name, input, rule, match, field string
	}{
		{
			name: "escaped sibling and secret", rule: gitleaks.SecretAccessKeyRuleID, match: secret, field: "SecretAccessKey",
			input: `{"note":"結果 \uD83D\uDC15","AccessKeyId":"\u0041` + accessID[1:] + `","SecretAccessKey":"\u0077` + secret[1:] + `"}`,
		},
		{
			name: "escaped label and session token", rule: gitleaks.SessionTokenRuleID, match: session, field: "SessionToken",
			input: `{"note":"結果 \uD83D\uDC15","Sess\u0069onToken":"\u0039` + session[1:] + `"}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			for depth := range 3 {
				input := tc.input
				for range depth {
					wrapped, err := json.Marshal(map[string]string{"arguments": input})
					require.NoError(t, err)
					input = string(wrapped)
				}
				result, err := shared().InspectText(t.Context(), input)
				require.NoError(t, err)
				require.True(t, result.Completed)
				require.Len(t, result.Findings, 1, "context and leaf inspection must count one occurrence")
				finding := result.Findings[0]
				require.Equal(t, tc.rule, finding.RuleID)
				require.Equal(t, tc.match, finding.Match, "trusted preparation must fingerprint the decoded value")
				require.Equal(t, "capture.decoded_json", finding.Field)
				require.Regexp(t, `^\$strings\[\d+\]$`, finding.Path)
				require.Equal(t, len(tc.match), finding.EndPos-finding.StartPos)
				redacted, report := RedactJSONWithReport(t.Context(), []byte(input))
				require.False(t, report.Incomplete)
				require.Len(t, report.Findings, 1)
				require.Equal(t, tc.rule, report.Findings[0].RuleID)
				for range depth {
					var wrapper map[string]string
					require.NoError(t, json.Unmarshal(redacted, &wrapper))
					redacted = []byte(wrapper["arguments"])
				}
				var fields map[string]string
				require.NoError(t, json.Unmarshal(redacted, &fields))
				require.Equal(t, Replacement, fields[tc.field])
				require.Equal(t, "結果 🐕", fields["note"])
			}
		})
	}
}

func TestJSONContextScanFailureClosesDocument(t *testing.T) {
	t.Parallel()
	input := `{"result":"\u0067hp_` + strings.TrimPrefix(syntheticPAT, "ghp_") + `"}`
	redactor := NewWithScanner(scannerFunc(func(_ context.Context, text string) (scanners.Result, error) {
		return scanners.Result{Completed: !strings.Contains(text, syntheticPAT)}, nil
	}))
	redacted, report := redactor.RedactJSONWithReport(t.Context(), []byte(input))
	require.Equal(t, `"[redacted]"`, string(redacted))
	require.True(t, report.Incomplete)
	result, err := redactor.InspectText(t.Context(), input)
	require.Error(t, err)
	require.False(t, result.Completed)
}

func TestJSONInspectionPreservesContextAcrossEncodedDocuments(t *testing.T) {
	t.Parallel()
	const accessID = "ASIAZ2XY3WNBQR5TUVWX"
	const secret = "wJalrXUtnFEMIbKp7MDoRZfiCYqTvHgNsQ8xLcWd"
	const session = "9x34WiYVTCAnxboiPBmqNrCFKD/3tyqFMmVo8mc0cNlyFFd93N5Z/o8gIIyX0hOWxvWRp/dO9Smp" +
		"CVXShsKReY3ef4sMrpkDz2IbZR9eAQyIioVhzt6O9p894tYWerLw9pGMezSLJLyNJ4DQO/A2CU2e8QsY1Buamkhl"
	inner, err := json.Marshal(`{"SecretAccessKey":"\u0077` + secret[1:] + `"}`)
	require.NoError(t, err)
	quotedToken, err := json.Marshal(`"\u0039` + session[1:] + `"`)
	require.NoError(t, err)
	cases := []struct {
		name, input, rule, match, field string
	}{
		{
			name: "identifier outside encoded document", rule: gitleaks.SecretAccessKeyRuleID, match: secret, field: "arguments",
			input: `{"AccessKeyId":"` + accessID + `","arguments":` + string(inner) + `}`,
		},
		{
			name: "label outside encoded JSON string", rule: gitleaks.SessionTokenRuleID, match: session, field: "SessionToken",
			input: `{"Sess\u0069onToken":` + string(quotedToken) + `}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			result, err := shared().InspectText(t.Context(), tc.input)
			require.NoError(t, err)
			require.Len(t, result.Findings, 1)
			require.Equal(t, tc.rule, result.Findings[0].RuleID)
			require.Equal(t, tc.match, result.Findings[0].Match)
			require.Equal(t, "capture.decoded_json", result.Findings[0].Field)
			require.Equal(t, len(tc.match), result.Findings[0].EndPos-result.Findings[0].StartPos)
			redacted, report := RedactJSONWithReport(t.Context(), []byte(tc.input))
			require.False(t, report.Incomplete)
			require.Len(t, report.Findings, 1)
			var fields map[string]string
			require.NoError(t, json.Unmarshal(redacted, &fields))
			if tc.field == "arguments" {
				var arguments map[string]string
				require.NoError(t, json.Unmarshal([]byte(fields[tc.field]), &arguments))
				require.Equal(t, Replacement, arguments["SecretAccessKey"])
				require.Equal(t, accessID, fields["AccessKeyId"])
			} else {
				var token string
				require.NoError(t, json.Unmarshal([]byte(fields[tc.field]), &token))
				require.Equal(t, Replacement, token)
			}
		})
	}
}
