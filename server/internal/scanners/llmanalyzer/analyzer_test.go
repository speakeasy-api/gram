package llmanalyzer_test

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/judgemessage"
	"github.com/speakeasy-api/gram/server/internal/message"
	"github.com/speakeasy-api/gram/server/internal/scanners"
	"github.com/speakeasy-api/gram/server/internal/scanners/llmanalyzer"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func newAnalyzer(t *testing.T, completer llmanalyzer.Completer) *llmanalyzer.Analyzer {
	t.Helper()
	return llmanalyzer.NewAnalyzer(testenv.NewLogger(t), testenv.NewTracerProvider(t), completer)
}

func userRequest(body string) llmanalyzer.Request {
	return llmanalyzer.Request{
		OrgID:       "org-1",
		OrgSlug:     "acme",
		ProjectID:   "proj-1",
		ScanMode:    llmanalyzer.ScanModeSync,
		Message:     judgemessage.New(message.User, "", body),
		ToolCallIDs: nil,
	}
}

func TestAnalyze_MapsEachFlaggedKeyToFinding(t *testing.T) {
	t.Parallel()

	cases := []struct {
		key      string
		ruleID   string
		category string
	}{
		{key: llmanalyzer.KeySecretsLeak, ruleID: llmanalyzer.RuleSecret, category: "secrets"},
		{key: llmanalyzer.KeyPersonalDataLeak, ruleID: llmanalyzer.RulePII, category: "pii"},
		{key: llmanalyzer.KeyPromptInjection, ruleID: llmanalyzer.RulePromptInjection, category: "prompt_injection"},
		{key: llmanalyzer.KeyDestructiveToolCall, ruleID: llmanalyzer.RuleDestructiveTool, category: "destructive_tool"},
	}

	for _, tc := range cases {
		stub := &llmanalyzer.StubCompleter{
			Response:         llmanalyzer.VerdictJSON(map[string]int{tc.key: 1}, "because of "+tc.key),
			Err:              nil,
			PromptTokens:     12,
			CompletionTokens: 4,
			Model:            "risk-judge-4b",
			Calls:            nil,
			ParseFailures:    0,
		}
		analysis := newAnalyzer(t, stub).Analyze(t.Context(), userRequest("hello world"))

		require.NoError(t, analysis.Err, "key %s", tc.key)
		require.True(t, analysis.Result.Completed, "key %s", tc.key)
		require.Len(t, analysis.Result.Findings, 1, "key %s", tc.key)
		require.Equal(t, scanners.Finding{
			RuleID:              tc.ruleID,
			Description:         "because of " + tc.key,
			Match:               "",
			StartPos:            0,
			EndPos:              0,
			Tags:                []string{tc.category},
			Source:              llmanalyzer.Source,
			Confidence:          1,
			DeadLetterReason:    "",
			McpLookupToolCallID: "",
			SpanGroupKey:        "",
			Field:               "",
			Path:                "",
		}, analysis.Result.Findings[0], "key %s", tc.key)
		require.Equal(t, []string{tc.key}, analysis.Verdict.Flagged(), "key %s", tc.key)
		require.Equal(t, "risk-judge-4b", analysis.Completion.Model, "key %s", tc.key)
		require.Equal(t, 12, analysis.Completion.PromptTokens, "key %s", tc.key)
	}
}

func TestAnalyze_MultipleFlaggedKeysKeepCanonicalOrder(t *testing.T) {
	t.Parallel()

	stub := &llmanalyzer.StubCompleter{
		Response: llmanalyzer.VerdictJSON(map[string]int{
			llmanalyzer.KeyDestructiveToolCall: 1,
			llmanalyzer.KeySecretsLeak:         1,
			llmanalyzer.KeyPromptInjection:     1,
		}, "multiple"),
		Err:              nil,
		PromptTokens:     0,
		CompletionTokens: 0,
		Model:            "",
		Calls:            nil,
		ParseFailures:    0,
	}
	analysis := newAnalyzer(t, stub).Analyze(t.Context(), userRequest("hello"))

	require.NoError(t, analysis.Err)
	require.True(t, analysis.Result.Completed)
	ruleIDs := make([]string, 0, len(analysis.Result.Findings))
	for _, f := range analysis.Result.Findings {
		ruleIDs = append(ruleIDs, f.RuleID)
	}
	require.Equal(t, []string{
		llmanalyzer.RuleSecret,
		llmanalyzer.RulePromptInjection,
		llmanalyzer.RuleDestructiveTool,
	}, ruleIDs)
}

func TestAnalyze_AllZeroScoresIsCleanAndCompleted(t *testing.T) {
	t.Parallel()

	stub := &llmanalyzer.StubCompleter{
		Response:         llmanalyzer.VerdictJSON(nil, "nothing to report"),
		Err:              nil,
		PromptTokens:     0,
		CompletionTokens: 0,
		Model:            "",
		Calls:            nil,
		ParseFailures:    0,
	}
	analysis := newAnalyzer(t, stub).Analyze(t.Context(), userRequest("a perfectly ordinary message"))

	require.NoError(t, analysis.Err)
	require.True(t, analysis.Result.Completed)
	require.Empty(t, analysis.Result.Findings)
	require.NotNil(t, analysis.Result.Findings)
	require.Positive(t, analysis.Result.STokens)
	require.False(t, analysis.Truncated)
	require.Zero(t, stub.ParseFailures)
}

func TestAnalyze_ReasoningCappedAt500Runes(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("é", 700)
	stub := &llmanalyzer.StubCompleter{
		Response:         llmanalyzer.VerdictJSON(map[string]int{llmanalyzer.KeyPersonalDataLeak: 1}, long),
		Err:              nil,
		PromptTokens:     0,
		CompletionTokens: 0,
		Model:            "",
		Calls:            nil,
		ParseFailures:    0,
	}
	analysis := newAnalyzer(t, stub).Analyze(t.Context(), userRequest("hello"))

	require.NoError(t, analysis.Err)
	require.Len(t, analysis.Result.Findings, 1)
	require.Equal(t, 500, utf8.RuneCountInString(analysis.Result.Findings[0].Description))
	require.Equal(t, strings.Repeat("é", 500), analysis.Result.Findings[0].Description)
}

func TestAnalyze_TrimsContentBeforeRendering(t *testing.T) {
	t.Parallel()

	stub := &llmanalyzer.StubCompleter{
		Response:         llmanalyzer.VerdictJSON(nil, ""),
		Err:              nil,
		PromptTokens:     0,
		CompletionTokens: 0,
		Model:            "",
		Calls:            nil,
		ParseFailures:    0,
	}
	newAnalyzer(t, stub).Analyze(t.Context(), userRequest("  \n\tplease deploy  \n\n"))

	calls := stub.CallsSnapshot()
	require.Len(t, calls, 1)
	require.Equal(t, llmanalyzer.CallInfo{OrgID: "org-1", OrgSlug: "acme", ScanMode: llmanalyzer.ScanModeSync}, calls[0].Info)
	require.Len(t, calls[0].Messages, 2)
	require.Equal(t, "system", calls[0].Messages[0].Role)
	require.Equal(t, llmanalyzer.SystemPrompt, calls[0].Messages[0].Content)
	require.Equal(t, "user", calls[0].Messages[1].Role)
	require.Equal(t, llmanalyzer.BuildUserPrompt(llmanalyzer.PromptInput{
		Content:     "please deploy",
		ToolCalls:   nil,
		ToolOutcome: "",
	}), calls[0].Messages[1].Content)
}

func TestAnalyze_UsesRealToolCallIDsWhenAligned(t *testing.T) {
	t.Parallel()

	stub := &llmanalyzer.StubCompleter{
		Response:         llmanalyzer.VerdictJSON(nil, ""),
		Err:              nil,
		PromptTokens:     0,
		CompletionTokens: 0,
		Model:            "",
		Calls:            nil,
		ParseFailures:    0,
	}
	req := llmanalyzer.Request{
		OrgID:     "org-1",
		OrgSlug:   "acme",
		ProjectID: "proj-1",
		ScanMode:  llmanalyzer.ScanModeAsync,
		Message: judgemessage.NewForToolCalls([]judgemessage.ToolCall{
			judgemessage.NewToolCall("Read", `{"path": "README.md"}`),
			judgemessage.NewToolCall("Bash", `{"command": "ls"}`),
		}),
		ToolCallIDs: []string{"call_abc", "call_def"},
	}
	newAnalyzer(t, stub).Analyze(t.Context(), req)

	calls := stub.CallsSnapshot()
	require.Len(t, calls, 1)
	require.Equal(t, llmanalyzer.BuildUserPrompt(llmanalyzer.PromptInput{
		Content: "",
		ToolCalls: []llmanalyzer.ToolCall{
			{ID: "call_abc", Name: "Read", Arguments: `{"path": "README.md"}`},
			{ID: "call_def", Name: "Bash", Arguments: `{"command": "ls"}`},
		},
		ToolOutcome: "",
	}), calls[0].Messages[1].Content)
}

func TestAnalyze_UsesRealToolCallIDForSingleToolRequest(t *testing.T) {
	t.Parallel()

	stub := &llmanalyzer.StubCompleter{
		Response:         llmanalyzer.VerdictJSON(nil, ""),
		Err:              nil,
		PromptTokens:     0,
		CompletionTokens: 0,
		Model:            "",
		Calls:            nil,
		ParseFailures:    0,
	}
	req := llmanalyzer.Request{
		OrgID:       "org-1",
		OrgSlug:     "acme",
		ProjectID:   "proj-1",
		ScanMode:    llmanalyzer.ScanModeSync,
		Message:     judgemessage.New(message.ToolRequest, "Bash", `{"command": "ls"}`),
		ToolCallIDs: []string{"call_xyz"},
	}
	newAnalyzer(t, stub).Analyze(t.Context(), req)

	calls := stub.CallsSnapshot()
	require.Len(t, calls, 1)
	require.Contains(t, calls[0].Messages[1].Content, `{"id": "call_xyz", "type": "function", "function": {"name": "Bash"`)
}

func TestAnalyze_SynthesizesSingleToolCallIDWhenExtraIDs(t *testing.T) {
	t.Parallel()

	stub := &llmanalyzer.StubCompleter{
		Response:         llmanalyzer.VerdictJSON(nil, ""),
		Err:              nil,
		PromptTokens:     0,
		CompletionTokens: 0,
		Model:            "",
		Calls:            nil,
		ParseFailures:    0,
	}
	req := llmanalyzer.Request{
		OrgID:       "org-1",
		OrgSlug:     "acme",
		ProjectID:   "proj-1",
		ScanMode:    llmanalyzer.ScanModeSync,
		Message:     judgemessage.New(message.ToolRequest, "Bash", `{"command": "ls"}`),
		ToolCallIDs: []string{"call_first", "call_second"},
	}
	newAnalyzer(t, stub).Analyze(t.Context(), req)

	calls := stub.CallsSnapshot()
	require.Len(t, calls, 1)
	require.Contains(t, calls[0].Messages[1].Content, `"id": "toolu_0000001"`)
	require.NotContains(t, calls[0].Messages[1].Content, "call_first")
}

func TestAnalyze_CapsToolCallIDsHeadAndTailWithTheCalls(t *testing.T) {
	t.Parallel()

	stub := &llmanalyzer.StubCompleter{
		Response:         llmanalyzer.VerdictJSON(nil, ""),
		Err:              nil,
		PromptTokens:     0,
		CompletionTokens: 0,
		Model:            "",
		Calls:            nil,
		ParseFailures:    0,
	}
	const total = 60
	calls := make([]judgemessage.ToolCall, 0, total)
	ids := make([]string, 0, total)
	for i := range total {
		calls = append(calls, judgemessage.NewToolCall("Read", `{}`))
		ids = append(ids, fmt.Sprintf("call_%03d", i))
	}
	req := llmanalyzer.Request{
		OrgID:       "org-1",
		OrgSlug:     "acme",
		ProjectID:   "proj-1",
		Lane:        "sync",
		Message:     judgemessage.NewForToolCalls(calls),
		ToolCallIDs: ids,
	}
	analysis := newAnalyzer(t, stub).Analyze(t.Context(), req)
	require.True(t, analysis.Truncated)

	prompt := stub.CallsSnapshot()[0].Messages[1].Content
	require.Contains(t, prompt, `"id": "call_000"`, "head keeps its harness ids")
	require.Contains(t, prompt, `"id": "call_024"`)
	require.Contains(t, prompt, `"id": "call_035"`, "tail keeps its harness ids")
	require.Contains(t, prompt, `"id": "call_059"`)
	require.NotContains(t, prompt, `"id": "call_030"`, "dropped middle calls carry no id")
	require.NotContains(t, prompt, "toolu_", "no synthetic ids once the real ones align")
}

func TestAnalyze_SynthesizesToolCallIDsWhenMisaligned(t *testing.T) {
	t.Parallel()

	stub := &llmanalyzer.StubCompleter{
		Response:         llmanalyzer.VerdictJSON(nil, ""),
		Err:              nil,
		PromptTokens:     0,
		CompletionTokens: 0,
		Model:            "",
		Calls:            nil,
		ParseFailures:    0,
	}
	req := llmanalyzer.Request{
		OrgID:     "org-1",
		OrgSlug:   "acme",
		ProjectID: "proj-1",
		ScanMode:  llmanalyzer.ScanModeSync,
		Message: judgemessage.NewForToolCalls([]judgemessage.ToolCall{
			judgemessage.NewToolCall("Read", `{}`),
			judgemessage.NewToolCall("Bash", `{}`),
		}),
		ToolCallIDs: []string{"call_only_one"},
	}
	newAnalyzer(t, stub).Analyze(t.Context(), req)

	calls := stub.CallsSnapshot()
	require.Len(t, calls, 1)
	require.Contains(t, calls[0].Messages[1].Content, `"id": "toolu_0000001"`)
	require.Contains(t, calls[0].Messages[1].Content, `"id": "toolu_0000002"`)
	require.NotContains(t, calls[0].Messages[1].Content, "call_only_one")
}

func TestAnalyze_ParseErrorIsDeadLetter(t *testing.T) {
	t.Parallel()

	stub := &llmanalyzer.StubCompleter{
		Response:         "I cannot help with that.",
		Err:              nil,
		PromptTokens:     0,
		CompletionTokens: 0,
		Model:            "",
		Calls:            nil,
		ParseFailures:    0,
	}
	analysis := newAnalyzer(t, stub).Analyze(t.Context(), userRequest("hello"))

	require.ErrorIs(t, analysis.Err, llmanalyzer.ErrParse)
	require.Equal(t, 1, stub.ParseFailures)
	require.Equal(t, llmanalyzer.DeadLetterResult(llmanalyzer.ReasonParseError), analysis.Result)
	require.False(t, analysis.Result.Completed)
	require.Equal(t, "I cannot help with that.", analysis.Completion.Content)
}

func TestAnalyze_TimeoutIsDeadLetter(t *testing.T) {
	t.Parallel()

	stub := &llmanalyzer.StubCompleter{
		Response:         "",
		Err:              llmanalyzer.ErrTimeout,
		PromptTokens:     0,
		CompletionTokens: 0,
		Model:            "",
		Calls:            nil,
		ParseFailures:    0,
	}
	analysis := newAnalyzer(t, stub).Analyze(t.Context(), userRequest("hello"))

	require.ErrorIs(t, analysis.Err, llmanalyzer.ErrTimeout)
	require.Equal(t, llmanalyzer.DeadLetterResult(llmanalyzer.ReasonTimeout), analysis.Result)
	require.Zero(t, stub.ParseFailures)
}

func TestAnalyze_Upstream5xxIsDeadLetter(t *testing.T) {
	t.Parallel()

	stub := &llmanalyzer.StubCompleter{
		Response:         "",
		Err:              &llmanalyzer.UpstreamError{Status: http.StatusServiceUnavailable, Body: "down"},
		PromptTokens:     0,
		CompletionTokens: 0,
		Model:            "",
		Calls:            nil,
		ParseFailures:    0,
	}
	analysis := newAnalyzer(t, stub).Analyze(t.Context(), userRequest("hello"))

	require.Equal(t, llmanalyzer.DeadLetterResult(llmanalyzer.ReasonUpstream5xx), analysis.Result)
	require.Equal(t, "upstream_5xx", analysis.Result.Findings[0].DeadLetterReason)
	require.NotContains(t, analysis.Result.Findings[0].Description, "down")
}

func TestAnalyze_RateLimitedIsDeadLetter(t *testing.T) {
	t.Parallel()

	stub := &llmanalyzer.StubCompleter{
		Response:         "",
		Err:              &llmanalyzer.UpstreamError{Status: http.StatusTooManyRequests, Body: ""},
		PromptTokens:     0,
		CompletionTokens: 0,
		Model:            "",
		Calls:            nil,
		ParseFailures:    0,
	}
	analysis := newAnalyzer(t, stub).Analyze(t.Context(), userRequest("hello"))

	require.Equal(t, llmanalyzer.DeadLetterResult(llmanalyzer.ReasonRateLimited), analysis.Result)
}

func TestAnalyze_EmptyCompletionIsDeadLetter(t *testing.T) {
	t.Parallel()

	stub := &llmanalyzer.StubCompleter{
		Response:         "",
		Err:              llmanalyzer.ErrEmptyCompletion,
		PromptTokens:     0,
		CompletionTokens: 0,
		Model:            "",
		Calls:            nil,
		ParseFailures:    0,
	}
	analysis := newAnalyzer(t, stub).Analyze(t.Context(), userRequest("hello"))

	require.Equal(t, llmanalyzer.DeadLetterResult(llmanalyzer.ReasonEmptyCompletion), analysis.Result)
}

func TestAnalyze_NilCompleterIsDisabled(t *testing.T) {
	t.Parallel()

	analyzer := newAnalyzer(t, nil)
	require.False(t, analyzer.Enabled())

	analysis := analyzer.Analyze(t.Context(), userRequest("hello"))

	require.ErrorIs(t, analysis.Err, llmanalyzer.ErrDisabled)
	require.Equal(t, llmanalyzer.DeadLetterResult(llmanalyzer.ReasonDisabled), analysis.Result)
	require.Equal(t, "Risk analysis unavailable: disabled", analysis.Result.Findings[0].Description)
}

func TestAnalyze_EnabledWithCompleter(t *testing.T) {
	t.Parallel()

	stub := &llmanalyzer.StubCompleter{
		Response:         "",
		Err:              nil,
		PromptTokens:     0,
		CompletionTokens: 0,
		Model:            "",
		Calls:            nil,
		ParseFailures:    0,
	}
	require.True(t, newAnalyzer(t, stub).Enabled())
}

func TestDeadLetterReason(t *testing.T) {
	t.Parallel()

	require.Empty(t, llmanalyzer.DeadLetterReason(nil))
	require.Equal(t, "disabled", llmanalyzer.DeadLetterReason(llmanalyzer.ErrDisabled))
	require.Equal(t, "timeout", llmanalyzer.DeadLetterReason(llmanalyzer.ErrTimeout))
	require.Equal(t, "empty_completion", llmanalyzer.DeadLetterReason(llmanalyzer.ErrEmptyCompletion))
	require.Equal(t, "parse_error", llmanalyzer.DeadLetterReason(llmanalyzer.ErrParse))
	require.Equal(t, "rate_limited", llmanalyzer.DeadLetterReason(&llmanalyzer.UpstreamError{Status: 429, Body: ""}))
	require.Equal(t, "upstream_5xx", llmanalyzer.DeadLetterReason(&llmanalyzer.UpstreamError{Status: 500, Body: ""}))
	require.Equal(t, "upstream_5xx", llmanalyzer.DeadLetterReason(&llmanalyzer.UpstreamError{Status: 503, Body: ""}))
	require.Equal(t, "upstream_4xx", llmanalyzer.DeadLetterReason(&llmanalyzer.UpstreamError{Status: 400, Body: ""}))
	require.Equal(t, "request_error", llmanalyzer.DeadLetterReason(errors.New("connection reset")))
}

func TestDeadLetterResult_Shape(t *testing.T) {
	t.Parallel()

	result := llmanalyzer.DeadLetterResult("timeout")

	require.False(t, result.Completed)
	require.Zero(t, result.STokens)
	require.Len(t, result.Findings, 1)
	require.Equal(t, scanners.Finding{
		RuleID:              llmanalyzer.RuleDeadLetter,
		Description:         "Risk analysis unavailable: timeout",
		Match:               "",
		StartPos:            0,
		EndPos:              0,
		Tags:                []string{},
		Source:              llmanalyzer.Source,
		Confidence:          0,
		DeadLetterReason:    "timeout",
		McpLookupToolCallID: "",
		SpanGroupKey:        "",
		Field:               "",
		Path:                "",
	}, result.Findings[0])
	require.True(t, llmanalyzer.IsDeadLetter(result.Findings[0]))
}

func TestIsDeadLetter(t *testing.T) {
	t.Parallel()

	require.True(t, llmanalyzer.IsDeadLetter(llmanalyzer.DeadLetterResult("timeout").Findings[0]))
	require.False(t, llmanalyzer.IsDeadLetter(llmanalyzer.NewFinding(llmanalyzer.KeySecretsLeak, "leak")))

	otherSource := llmanalyzer.DeadLetterResult("timeout").Findings[0]
	otherSource.Source = "presidio"
	require.False(t, llmanalyzer.IsDeadLetter(otherSource))
}

func TestCategoryForKey(t *testing.T) {
	t.Parallel()

	require.Equal(t, "secrets", llmanalyzer.CategoryForKey(llmanalyzer.KeySecretsLeak))
	require.Equal(t, "pii", llmanalyzer.CategoryForKey(llmanalyzer.KeyPersonalDataLeak))
	require.Equal(t, "prompt_injection", llmanalyzer.CategoryForKey(llmanalyzer.KeyPromptInjection))
	require.Equal(t, "destructive_tool", llmanalyzer.CategoryForKey(llmanalyzer.KeyDestructiveToolCall))
	require.Empty(t, llmanalyzer.CategoryForKey("unknown"))
}

func allFindings() []scanners.Finding {
	return []scanners.Finding{
		llmanalyzer.NewFinding(llmanalyzer.KeySecretsLeak, "secret"),
		llmanalyzer.NewFinding(llmanalyzer.KeyPersonalDataLeak, "pii"),
		llmanalyzer.NewFinding(llmanalyzer.KeyPromptInjection, "injection"),
		llmanalyzer.NewFinding(llmanalyzer.KeyDestructiveToolCall, "destructive"),
	}
}

func ruleIDs(findings []scanners.Finding) []string {
	ids := make([]string, 0, len(findings))
	for _, f := range findings {
		ids = append(ids, f.RuleID)
	}
	return ids
}

func TestFindingsForSources_FiltersByCoveredSource(t *testing.T) {
	t.Parallel()

	require.Equal(t, []string{llmanalyzer.RuleSecret}, ruleIDs(llmanalyzer.FindingsForSources(allFindings(), []string{"gitleaks"})))
	require.Equal(t, []string{llmanalyzer.RulePII}, ruleIDs(llmanalyzer.FindingsForSource(allFindings(), "presidio")))
	require.Equal(t, []string{llmanalyzer.RulePromptInjection}, ruleIDs(llmanalyzer.FindingsForSource(allFindings(), "prompt_injection")))
	require.Equal(t, []string{llmanalyzer.RuleCLIDestructive}, ruleIDs(llmanalyzer.FindingsForSource(allFindings(), "cli_destructive")))
	require.Equal(t, []string{llmanalyzer.RuleDestructiveTool}, ruleIDs(llmanalyzer.FindingsForSource(allFindings(), "destructive_tool")))
	require.Equal(t,
		[]string{llmanalyzer.RuleSecret, llmanalyzer.RulePII},
		ruleIDs(llmanalyzer.FindingsForSources(allFindings(), []string{"presidio", "gitleaks", "custom"})),
	)
}

func TestFindingsForSources_UncoveredSourcesGetNothing(t *testing.T) {
	t.Parallel()

	got := llmanalyzer.FindingsForSources(allFindings(), []string{"shadow_mcp", "custom", "account_identity"})
	require.NotNil(t, got)
	require.Empty(t, got)

	require.Empty(t, llmanalyzer.FindingsForSources(allFindings(), nil))
}

func TestFindingsForSources_DeadLetterKeptOnlyForCoveredSources(t *testing.T) {
	t.Parallel()

	findings := llmanalyzer.DeadLetterResult("timeout").Findings

	kept := llmanalyzer.FindingsForSources(findings, []string{"gitleaks"})
	require.Len(t, kept, 1)
	require.True(t, llmanalyzer.IsDeadLetter(kept[0]))

	kept = llmanalyzer.FindingsForSources(findings, []string{"shadow_mcp", "presidio"})
	require.Len(t, kept, 1)

	require.Empty(t, llmanalyzer.FindingsForSources(findings, []string{"shadow_mcp"}))
}

func TestFindingsForSources_IgnoresForeignSources(t *testing.T) {
	t.Parallel()

	foreign := llmanalyzer.NewFinding(llmanalyzer.KeySecretsLeak, "from another engine")
	foreign.Source = "gitleaks"

	require.Empty(t, llmanalyzer.FindingsForSource([]scanners.Finding{foreign}, "gitleaks"))
}

func TestFindingsForSources_RelabelsSharedKeyPerSource(t *testing.T) {
	t.Parallel()

	verdict := []scanners.Finding{llmanalyzer.NewFinding(llmanalyzer.KeyDestructiveToolCall, "drops the table")}

	both := llmanalyzer.FindingsForSources(verdict, []string{"destructive_tool", "cli_destructive", "cli_destructive"})
	require.Equal(t, []string{llmanalyzer.RuleDestructiveTool, llmanalyzer.RuleCLIDestructive}, ruleIDs(both))
	require.Equal(t, []string{"destructive_tool"}, both[0].Tags)
	require.Equal(t, []string{"cli_destructive"}, both[1].Tags)
	require.Equal(t, "drops the table", both[1].Description)
	require.Equal(t, llmanalyzer.Source, both[1].Source)

	// The input is never mutated.
	require.Equal(t, llmanalyzer.RuleDestructiveTool, verdict[0].RuleID)
	require.Equal(t, []string{"destructive_tool"}, verdict[0].Tags)
}

func TestCategoryForSource(t *testing.T) {
	t.Parallel()

	for _, source := range llmanalyzer.CoveredSources {
		require.NotEmpty(t, llmanalyzer.CategoryForSource(source), source)
	}
	require.Equal(t, "cli_destructive", llmanalyzer.CategoryForSource("cli_destructive"))
	require.Empty(t, llmanalyzer.CategoryForSource("shadow_mcp"))
}
