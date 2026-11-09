package captureredact

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/scanners"
)

// Synthetic credentials exercise provider-specific formats without live values.
const syntheticPAT = "ghp_b847c2d93a5e4f6071cb9368d014a2e75f69"

type scannerFunc func(context.Context, string) (scanners.Result, error)

func (f scannerFunc) Scan(ctx context.Context, text string) (scanners.Result, error) {
	return f(ctx, text)
}

func TestRedactTextCredentialFormats(t *testing.T) {
	t.Parallel()
	fineGrained := "github_pat_" + strings.Repeat("b847c2d93a5e4f6071cb9368d014a2e75f69", 3)[:82]
	cases := []struct {
		name string
		text string
		want string
	}{
		{name: "classic GitHub token in environment output", text: "MISE_GITHUB_TOKEN=" + syntheticPAT, want: "MISE_GITHUB_TOKEN=" + Replacement},
		{name: "fine grained GitHub token", text: "MISE_GITHUB_TOKEN=" + fineGrained, want: "MISE_GITHUB_TOKEN=" + Replacement},
		{name: "multiline Unicode", text: "結果 🐕\nMISE_GITHUB_TOKEN=" + syntheticPAT + "\nfin", want: "結果 🐕\nMISE_GITHUB_TOKEN=" + Replacement + "\nfin"},
		{name: "repeated credentials", text: syntheticPAT + " then " + syntheticPAT, want: Replacement + " then " + Replacement},
		{name: "ordinary transcript", text: "Run test:server and inspect the response.", want: "Run test:server and inspect the response."},
		{name: "ordinary identifiers", text: "request_id=019b1cde-1234-7123-8123-123456789abc\ncommit=0123456789abcdef0123456789abcdef01234567", want: "request_id=019b1cde-1234-7123-8123-123456789abc\ncommit=0123456789abcdef0123456789abcdef01234567"},
		{name: "AWS access identifier alone", text: "AWS_ACCESS_KEY_ID=AKIAIOSFODNN7EXAMPLE", want: "AWS_ACCESS_KEY_ID=AKIAIOSFODNN7EXAMPLE"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := RedactText(t.Context(), tc.text)
			require.Equal(t, tc.want, got)
			require.True(t, utf8.ValidString(got))
		})
	}
}

func TestRedactTextScansPastBatchLimit(t *testing.T) {
	t.Parallel()
	prefix := strings.Repeat("x", 60*1024) + "\n"
	got := RedactText(t.Context(), prefix+syntheticPAT)
	require.Len(t, got, len(prefix)+len(Replacement))
	require.True(t, strings.HasPrefix(got, prefix))
	require.True(t, strings.HasSuffix(got, Replacement))
}

func TestRedactJSONPreservesStructureAndNumbers(t *testing.T) {
	t.Parallel()
	input := []byte(" { \"large\":9007199254740993, \"decimal\":1.2300, \"exponent\":6.02e+23, \"negativeZero\":-0, \"tools\":[{\"output\":\"" + syntheticPAT + "\"}], \"ok\":true, \"empty\":null } ")
	before := string(input)
	got := RedactJSON(t.Context(), input)
	require.Equal(t, strings.ReplaceAll(before, syntheticPAT, Replacement), string(got))
	require.True(t, json.Valid(got))
	require.Equal(t, before, string(input), "the caller's bytes are not modified")
}

func TestRedactJSONDecodesEscapedCredentials(t *testing.T) {
	t.Parallel()
	input := []byte(`{"tool_result":{"text":"\u0067\u0068\u0070_b847c2d93a5e4f6071cb9368d014a2e75f69"}}`)
	got := RedactJSON(t.Context(), input)
	require.JSONEq(t, `{"tool_result":{"text":"[redacted]"}}`, string(got))
}

func TestRedactJSONHandlesEmbeddedJSONStrings(t *testing.T) {
	t.Parallel()
	inner := `{"MISE_GITHUB_TOKEN":"` + syntheticPAT + `","count":9007199254740993}`
	embedded, err := json.Marshal(map[string]string{"arguments": inner})
	require.NoError(t, err)
	outer, err := json.Marshal(map[string]string{"result": string(embedded)})
	require.NoError(t, err)
	got := RedactText(t.Context(), string(outer))
	var result map[string]string
	require.NoError(t, json.Unmarshal([]byte(got), &result))
	var arguments map[string]string
	require.NoError(t, json.Unmarshal([]byte(result["result"]), &arguments))
	require.Equal(t, strings.ReplaceAll(inner, syntheticPAT, Replacement), arguments["arguments"])
}

func TestRedactJSONCopiesUnchangedBytes(t *testing.T) {
	t.Parallel()
	input := []byte(" {\"quoted\":\"ordinary \\\"text\\\"\",\"numbers\":[1.000,2e+03]} \n")
	got := RedactJSON(t.Context(), input)
	require.Equal(t, input, got)
	got[0] = '\n'
	require.Equal(t, byte(' '), input[0])
}

func TestRedactJSONMalformedInputFailsClosed(t *testing.T) {
	t.Parallel()
	got := RedactJSON(t.Context(), []byte(`{"result":"`+syntheticPAT))
	require.Equal(t, `"[redacted]"`, string(got))
}

func TestRedactTextMergesOverlappingSpans(t *testing.T) {
	t.Parallel()
	redactor := NewWithScanner(scannerFunc(func(context.Context, string) (scanners.Result, error) {
		return scanners.Result{Completed: true, Findings: []scanners.Finding{
			{RuleID: "secret.first", Match: "cdef", StartPos: 2, EndPos: 6},
			{RuleID: "secret.second", Match: "efgh", StartPos: 4, EndPos: 8},
			{RuleID: "secret.duplicate", Match: "cdef", StartPos: 2, EndPos: 6},
		}}, nil
	}))
	require.Equal(t, "ab"+Replacement+"ij", redactor.RedactText(t.Context(), "abcdefghij"))
}

func TestRedactTextRejectsInvalidSpans(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		finding scanners.Finding
	}{
		{name: "negative offset", finding: scanners.Finding{Match: "a", StartPos: -1, EndPos: 1}},
		{name: "past end", finding: scanners.Finding{Match: "a", StartPos: 0, EndPos: 10}},
		{name: "empty span", finding: scanners.Finding{Match: "", StartPos: 0, EndPos: 0}},
		{name: "mismatched value", finding: scanners.Finding{Match: "different", StartPos: 0, EndPos: 1}},
		{name: "partial Unicode", finding: scanners.Finding{Match: string([]byte{0xc3}), StartPos: 1, EndPos: 2}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			redactor := NewWithScanner(scannerFunc(func(context.Context, string) (scanners.Result, error) {
				return scanners.Result{Completed: true, Findings: []scanners.Finding{tc.finding}}, nil
			}))
			require.Equal(t, Replacement, redactor.RedactText(t.Context(), "aéz"))
		})
	}
}

func TestRedactTextScanFailureFailsClosed(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		result scanners.Result
		err    error
	}{
		{name: "error", result: scanners.Result{Completed: true}, err: errors.New("scanner failed")},
		{name: "incomplete", result: scanners.Result{Completed: false}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			redactor := NewWithScanner(scannerFunc(func(context.Context, string) (scanners.Result, error) {
				return tc.result, tc.err
			}))
			require.Equal(t, Replacement, redactor.RedactText(t.Context(), syntheticPAT))
		})
	}
}

func TestRedactTextCancellationFailsClosed(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	redactor := NewWithScanner(scannerFunc(func(context.Context, string) (scanners.Result, error) {
		t.Fatal("a canceled capture must not enter the scanner")
		return scanners.Result{}, nil
	}))
	require.Equal(t, Replacement, redactor.RedactText(ctx, syntheticPAT))
	require.Equal(t, `"[redacted]"`, string(redactor.RedactJSON(ctx, []byte(`{"value":"`+syntheticPAT+`"}`))))
}

func TestRedactTextMissingScannerFailsClosed(t *testing.T) {
	t.Parallel()
	require.Equal(t, Replacement, NewWithScanner(nil).RedactText(t.Context(), syntheticPAT))
}

func TestRedactTextCancellationDuringScanFailsClosed(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	redactor := NewWithScanner(scannerFunc(func(context.Context, string) (scanners.Result, error) {
		cancel()
		return scanners.Result{Completed: true}, nil
	}))
	require.Equal(t, Replacement, redactor.RedactText(ctx, syntheticPAT))
}

func TestRedactTextExcludesGenericHeuristic(t *testing.T) {
	t.Parallel()
	redactor := NewWithScanner(scannerFunc(func(context.Context, string) (scanners.Result, error) {
		return scanners.Result{Completed: true, Findings: []scanners.Finding{{RuleID: genericAPIKeyRule, Match: "ordinary", StartPos: 0, EndPos: 8}}}, nil
	}))
	require.Equal(t, "ordinary", redactor.RedactText(t.Context(), "ordinary"))
}

func TestRedactValueCopiesJSONLikeContent(t *testing.T) {
	t.Parallel()
	input := map[string]any{
		"content": []any{syntheticPAT, json.Number("9007199254740993"), true, nil},
		"strings": []string{syntheticPAT},
		"raw":     json.RawMessage(`{"value":"` + syntheticPAT + `"}`),
		"bytes":   []byte(syntheticPAT),
		"named":   map[string]string{"result": syntheticPAT},
	}
	got, ok := RedactValue(t.Context(), input).(map[string]any)
	require.True(t, ok)
	require.Equal(t, []any{Replacement, json.Number("9007199254740993"), true, nil}, got["content"])
	require.Equal(t, []string{Replacement}, got["strings"])
	raw, ok := got["raw"].(json.RawMessage)
	require.True(t, ok)
	require.JSONEq(t, `{"value":"[redacted]"}`, string(raw))
	require.Equal(t, []byte(Replacement), got["bytes"])
	require.Equal(t, map[string]string{"result": Replacement}, got["named"])
	content, ok := input["content"].([]any)
	require.True(t, ok)
	require.Equal(t, syntheticPAT, content[0])
	require.Equal(t, []string{syntheticPAT}, input["strings"])
}

func TestRedactJSONWithReportKeepsOnlySafeMetadata(t *testing.T) {
	t.Parallel()
	input := []byte(`{"` + syntheticPAT + `":"MISE_GITHUB_TOKEN=` + syntheticPAT + `"}`)
	got, report := RedactJSONWithReport(t.Context(), input)
	require.JSONEq(t, `{"[redacted]":"MISE_GITHUB_TOKEN=[redacted]"}`, string(got))
	require.False(t, report.Incomplete)
	require.Len(t, report.Findings, 2)
	for _, finding := range report.Findings {
		require.Equal(t, "secret.github_pat", finding.RuleID)
		require.Equal(t, len(syntheticPAT), finding.EndPos-finding.StartPos)
		require.Regexp(t, `^\$strings\[\d+\]$`, finding.Path)
	}
	encoded, err := json.Marshal(report)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), syntheticPAT)
	require.NotContains(t, string(encoded), "MISE_GITHUB_TOKEN")
}

func TestRedactJSONWithReportMarksIncompleteScans(t *testing.T) {
	t.Parallel()
	redactor := NewWithScanner(scannerFunc(func(context.Context, string) (scanners.Result, error) {
		return scanners.Result{Completed: false}, nil
	}))
	got, report := redactor.RedactJSONWithReport(t.Context(), []byte(`"`+syntheticPAT+`"`))
	require.Equal(t, `"[redacted]"`, string(got))
	require.True(t, report.Incomplete)
	require.Empty(t, report.Findings)
}

func TestRedactJSONWithReportMarksMalformedInput(t *testing.T) {
	t.Parallel()
	got, report := RedactJSONWithReport(t.Context(), []byte(`{"broken":"`+syntheticPAT))
	require.Equal(t, `"[redacted]"`, string(got))
	require.True(t, report.Incomplete)
	require.Empty(t, report.Findings)
}

func TestRedactValueBoundsCyclesAndUnsupportedTypes(t *testing.T) {
	t.Parallel()
	input := map[string]any{}
	input["self"] = input
	got := RedactValue(t.Context(), input)
	encoded, err := json.Marshal(got)
	require.NoError(t, err)
	require.Contains(t, string(encoded), Replacement)
	require.Equal(t, Replacement, RedactValue(t.Context(), struct{ Text string }{Text: syntheticPAT}))
}

func TestRedactJSONBoundsEmbeddedStrings(t *testing.T) {
	t.Parallel()
	redactor := NewWithScanner(nil)
	got := redactor.redactJSON(t.Context(), []byte(`{"value":"`+syntheticPAT+`"}`), maxNesting+1)
	require.Equal(t, `"[redacted]"`, string(got))
}
