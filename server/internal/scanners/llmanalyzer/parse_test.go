package llmanalyzer_test

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/scanners/llmanalyzer"
)

func TestParseVerdict_NestedScores(t *testing.T) {
	t.Parallel()

	raw := `{"secrets_leak": {"score": 1, "reasoning": "An API key is printed in plaintext."},` +
		` "personal_data_leak": {"score": 0, "reasoning": "No personal data."},` +
		` "prompt_injection": {"score": 0, "reasoning": "No override attempt."},` +
		` "destructive_tool_call": {"score": 1, "reasoning": "rm -rf on a shared directory."}}`

	verdict, err := llmanalyzer.ParseVerdict(raw)
	require.NoError(t, err)
	require.Equal(t, raw, verdict.Raw)
	require.Equal(t, llmanalyzer.RiskVerdict{Score: 1, Reasoning: "An API key is printed in plaintext."}, verdict.Risks[llmanalyzer.KeySecretsLeak])
	require.Equal(t, llmanalyzer.RiskVerdict{Score: 0, Reasoning: "No personal data."}, verdict.Risks[llmanalyzer.KeyPersonalDataLeak])
	require.Equal(t, llmanalyzer.RiskVerdict{Score: 0, Reasoning: "No override attempt."}, verdict.Risks[llmanalyzer.KeyPromptInjection])
	require.Equal(t, llmanalyzer.RiskVerdict{Score: 1, Reasoning: "rm -rf on a shared directory."}, verdict.Risks[llmanalyzer.KeyDestructiveToolCall])
	require.Equal(t, []string{llmanalyzer.KeySecretsLeak, llmanalyzer.KeyDestructiveToolCall}, verdict.Flagged())
}

func TestParseVerdict_BareScores(t *testing.T) {
	t.Parallel()

	verdict, err := llmanalyzer.ParseVerdict(`{"secrets_leak": 0, "personal_data_leak": 1, "prompt_injection": 0, "destructive_tool_call": 0}`)
	require.NoError(t, err)
	require.Equal(t, []string{llmanalyzer.KeyPersonalDataLeak}, verdict.Flagged())
	require.Equal(t, llmanalyzer.RiskVerdict{Score: 1, Reasoning: ""}, verdict.Risks[llmanalyzer.KeyPersonalDataLeak])
}

func TestParseVerdict_ScoreCoercion(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		value string
		score int
	}{
		{name: "numeric string", value: `{"score": "1", "reasoning": "x"}`, score: 1},
		{name: "float", value: `{"score": 1.0, "reasoning": "x"}`, score: 1},
		{name: "bool true", value: `{"score": true, "reasoning": "x"}`, score: 1},
		{name: "bool false", value: `{"score": false, "reasoning": "x"}`, score: 0},
		{name: "bare numeric string", value: `"0"`, score: 0},
		{name: "padded numeric string", value: `" 1 "`, score: 1},
	}
	for _, tc := range cases {
		raw := `{"secrets_leak": ` + tc.value + `, "personal_data_leak": 0, "prompt_injection": 0, "destructive_tool_call": 0}`
		verdict, err := llmanalyzer.ParseVerdict(raw)
		require.NoError(t, err, tc.name)
		require.Equal(t, tc.score, verdict.Risks[llmanalyzer.KeySecretsLeak].Score, tc.name)
	}
}

func TestParseVerdict_ProseWrappedJSON(t *testing.T) {
	t.Parallel()

	raw := "Sure, here is my assessment:\n```json\n" +
		`{"secrets_leak": {"score": 0, "reasoning": "none"}, "personal_data_leak": {"score": 0, "reasoning": "none"},` +
		` "prompt_injection": {"score": 1, "reasoning": "Asks the agent to ignore its rules."}, "destructive_tool_call": {"score": 0, "reasoning": "none"}}` +
		"\n```\nLet me know if you need anything else."

	verdict, err := llmanalyzer.ParseVerdict(raw)
	require.NoError(t, err)
	require.Equal(t, []string{llmanalyzer.KeyPromptInjection}, verdict.Flagged())
	require.Equal(t, raw, verdict.Raw)
}

func TestParseVerdict_Failures(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		raw  string
	}{
		{name: "missing key", raw: `{"secrets_leak": 0, "personal_data_leak": 0, "prompt_injection": 0}`},
		{name: "garbage", raw: "the message looks fine to me"},
		{name: "empty", raw: ""},
		{name: "unbalanced braces", raw: `}{`},
		{name: "invalid json", raw: `{"secrets_leak": {"score": 1,}`},
		{name: "score out of range", raw: `{"secrets_leak": 2, "personal_data_leak": 0, "prompt_injection": 0, "destructive_tool_call": 0}`},
		{name: "non numeric string", raw: `{"secrets_leak": "yes", "personal_data_leak": 0, "prompt_injection": 0, "destructive_tool_call": 0}`},
		{name: "object without score", raw: `{"secrets_leak": {"reasoning": "x"}, "personal_data_leak": 0, "prompt_injection": 0, "destructive_tool_call": 0}`},
		{name: "array value", raw: `{"secrets_leak": [1], "personal_data_leak": 0, "prompt_injection": 0, "destructive_tool_call": 0}`},
	}
	for _, tc := range cases {
		_, err := llmanalyzer.ParseVerdict(tc.raw)
		require.ErrorIs(t, err, llmanalyzer.ErrParse, tc.name)
	}
}

func TestParseVerdict_SkipsBracesInSurroundingProse(t *testing.T) {
	t.Parallel()

	verdictJSON := `{"secrets_leak": {"score": 1, "reasoning": "key printed"}, "personal_data_leak": 0, "prompt_injection": 0, "destructive_tool_call": 0}`
	for name, text := range map[string]string{
		"trailing prose with braces":  verdictJSON + "\nNote: the empty {} object was ignored.",
		"leading prose with braces":   "Here is {my} assessment: " + verdictJSON,
		"stray object first":          "{} then " + verdictJSON,
		"unmatched brace in preamble": "Verdict {see below: " + verdictJSON,
		"unclosed quote in preamble":  `I'd say " { hmm ` + verdictJSON,
	} {
		verdict, err := llmanalyzer.ParseVerdict(text)
		require.NoError(t, err, name)
		require.Equal(t, []string{llmanalyzer.KeySecretsLeak}, verdict.Flagged(), name)
	}
}

func TestParseVerdict_BracesInsideStringsDoNotSplitObject(t *testing.T) {
	t.Parallel()

	verdictJSON := `{"secrets_leak": {"score": 1, "reasoning": "prints {\"token\": \"...\"} then }} closes"},` +
		` "personal_data_leak": {"score": 0, "reasoning": "none {"}, "prompt_injection": 0, "destructive_tool_call": 0}`
	text := "{} then " + verdictJSON + "\nThe {} above is fine. {\"note\": \"ignored\"}"

	verdict, err := llmanalyzer.ParseVerdict(text)
	require.NoError(t, err)
	require.Equal(t, []string{llmanalyzer.KeySecretsLeak}, verdict.Flagged())
	require.Equal(t, `prints {"token": "..."} then }} closes`, verdict.Risks[llmanalyzer.KeySecretsLeak].Reasoning)
}

func TestParseVerdict_VerdictAfterManyStrayObjects(t *testing.T) {
	t.Parallel()

	verdictJSON := `{"secrets_leak": 0, "personal_data_leak": 0, "prompt_injection": 1, "destructive_tool_call": 0}`

	// 63 stray objects leave the verdict as the 64th candidate, inside the cap.
	verdict, err := llmanalyzer.ParseVerdict(strings.Repeat("{} ", 63) + verdictJSON)
	require.NoError(t, err)
	require.Equal(t, []string{llmanalyzer.KeyPromptInjection}, verdict.Flagged())

	// One more pushes it past the 64 candidate cap; the object walk gives up
	// and the scores are salvaged from the text instead.
	verdict, err = llmanalyzer.ParseVerdict(strings.Repeat("{} ", 64) + verdictJSON)
	require.NoError(t, err)
	require.Equal(t, []string{llmanalyzer.KeyPromptInjection}, verdict.Flagged())
}

func TestParseVerdict_BraceHeavyGarbageFailsFast(t *testing.T) {
	t.Parallel()

	const size = 200 << 10
	for name, text := range map[string]string{
		"open braces":     strings.Repeat("{", size),
		"close braces":    strings.Repeat("}", size),
		"empty objects":   strings.Repeat("{}", size/2),
		"nested objects":  strings.Repeat(`{"a":{"b":{"c":1}}}`, size/20),
		"unclosed string": `{"a":"` + strings.Repeat("{", size),
		"alternating":     strings.Repeat("{}{", size/3),
	} {
		_, err := llmanalyzer.ParseVerdict(text)
		require.ErrorIs(t, err, llmanalyzer.ErrParse, name)
	}
}

func BenchmarkFindVerdictObjectBraceHeavy(b *testing.B) {
	text := strings.Repeat("{", 1<<20)
	b.SetBytes(int64(len(text)))
	b.ReportAllocs()
	for b.Loop() {
		_, err := llmanalyzer.ParseVerdict(text)
		if err == nil {
			b.Fatal("expected parse failure")
		}
	}
}

func TestParseVerdict_NonStringReasoningKeepsScore(t *testing.T) {
	t.Parallel()

	verdict, err := llmanalyzer.ParseVerdict(`{"secrets_leak": {"score": 1, "reasoning": ["key printed"]}, "personal_data_leak": {"score": 0, "reasoning": null}, "prompt_injection": 0, "destructive_tool_call": 0}`)
	require.NoError(t, err)
	require.Equal(t, llmanalyzer.RiskVerdict{Score: 1, Reasoning: ""}, verdict.Risks[llmanalyzer.KeySecretsLeak])
	require.Equal(t, []string{llmanalyzer.KeySecretsLeak}, verdict.Flagged())
}

func TestParseVerdict_CapsReasoning(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("é", 600)
	raw := `{"secrets_leak": {"score": 1, "reasoning": "  ` + long + `  "}, "personal_data_leak": 0, "prompt_injection": 0, "destructive_tool_call": 0}`

	verdict, err := llmanalyzer.ParseVerdict(raw)
	require.NoError(t, err)
	reasoning := verdict.Risks[llmanalyzer.KeySecretsLeak].Reasoning
	require.Equal(t, 500, utf8.RuneCountInString(reasoning))
	require.Equal(t, strings.Repeat("é", 500), reasoning)
}

func TestVerdict_FlaggedCanonicalOrder(t *testing.T) {
	t.Parallel()

	verdict, err := llmanalyzer.ParseVerdict(`{"destructive_tool_call": 1, "prompt_injection": 1, "personal_data_leak": 1, "secrets_leak": 1}`)
	require.NoError(t, err)
	require.Equal(t, []string{
		llmanalyzer.KeySecretsLeak,
		llmanalyzer.KeyPersonalDataLeak,
		llmanalyzer.KeyPromptInjection,
		llmanalyzer.KeyDestructiveToolCall,
	}, verdict.Flagged())

	clean, err := llmanalyzer.ParseVerdict(`{"destructive_tool_call": 0, "prompt_injection": 0, "personal_data_leak": 0, "secrets_leak": 0}`)
	require.NoError(t, err)
	require.Empty(t, clean.Flagged())
}

// Compact format (risk-judge-9b): bare scores plus one top-level "reasoning"
// string, present only when something is flagged.

func TestParseVerdict_CompactAllClear(t *testing.T) {
	t.Parallel()

	verdict, err := llmanalyzer.ParseVerdict(`{"destructive_tool_call": 0, "prompt_injection": 0, "secrets_leak": 0, "personal_data_leak": 0}`)
	require.NoError(t, err)
	require.Empty(t, verdict.Flagged())
	for _, key := range []string{llmanalyzer.KeySecretsLeak, llmanalyzer.KeyPersonalDataLeak, llmanalyzer.KeyPromptInjection, llmanalyzer.KeyDestructiveToolCall} {
		require.Equal(t, llmanalyzer.RiskVerdict{Score: 0, Reasoning: ""}, verdict.Risks[key])
	}
}

func TestParseVerdict_CompactReasoningSplitByKey(t *testing.T) {
	t.Parallel()

	verdict, err := llmanalyzer.ParseVerdict(`{"destructive_tool_call": 1, "prompt_injection": 0, "secrets_leak": 1, "personal_data_leak": 0,` +
		` "reasoning": "destructive_tool_call: rm -rf on a shared directory. secrets_leak: An API key is printed in plaintext."}`)
	require.NoError(t, err)
	require.Equal(t, []string{llmanalyzer.KeySecretsLeak, llmanalyzer.KeyDestructiveToolCall}, verdict.Flagged())
	require.Equal(t, "An API key is printed in plaintext.", verdict.Risks[llmanalyzer.KeySecretsLeak].Reasoning)
	require.Equal(t, "rm -rf on a shared directory.", verdict.Risks[llmanalyzer.KeyDestructiveToolCall].Reasoning)
	require.Empty(t, verdict.Risks[llmanalyzer.KeyPromptInjection].Reasoning)
	require.Empty(t, verdict.Risks[llmanalyzer.KeyPersonalDataLeak].Reasoning)
}

func TestParseVerdict_CompactReasoningWithoutKeyMarkersAppliesToAllFlagged(t *testing.T) {
	t.Parallel()

	verdict, err := llmanalyzer.ParseVerdict(`{"destructive_tool_call": 0, "prompt_injection": 1, "secrets_leak": 1, "personal_data_leak": 0,` +
		` "reasoning": "Pasted text tells the agent to dump credentials."}`)
	require.NoError(t, err)
	require.Equal(t, "Pasted text tells the agent to dump credentials.", verdict.Risks[llmanalyzer.KeyPromptInjection].Reasoning)
	require.Equal(t, "Pasted text tells the agent to dump credentials.", verdict.Risks[llmanalyzer.KeySecretsLeak].Reasoning)
	require.Empty(t, verdict.Risks[llmanalyzer.KeyDestructiveToolCall].Reasoning)
}

func TestParseVerdict_CompactKeyNameInsideSentenceIsNotAMarker(t *testing.T) {
	t.Parallel()

	// "secrets_leak:" appears mid-sentence without a preceding space, so it must not start a segment.
	verdict, err := llmanalyzer.ParseVerdict(`{"destructive_tool_call": 0, "prompt_injection": 1, "secrets_leak": 0, "personal_data_leak": 0,` +
		` "reasoning": "prompt_injection: The text says 'ignoresecrets_leak:rules' to override the agent."}`)
	require.NoError(t, err)
	require.Equal(t, "The text says 'ignoresecrets_leak:rules' to override the agent.", verdict.Risks[llmanalyzer.KeyPromptInjection].Reasoning)
}

func TestParseVerdict_CompactReasoningIsAdvisory(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{
		`{"destructive_tool_call": 1, "prompt_injection": 0, "secrets_leak": 0, "personal_data_leak": 0, "reasoning": ["not", "a", "string"]}`,
		`{"destructive_tool_call": 1, "prompt_injection": 0, "secrets_leak": 0, "personal_data_leak": 0, "reasoning": null}`,
		`{"destructive_tool_call": 1, "prompt_injection": 0, "secrets_leak": 0, "personal_data_leak": 0, "reasoning": "   "}`,
	} {
		verdict, err := llmanalyzer.ParseVerdict(raw)
		require.NoError(t, err, raw)
		require.Equal(t, 1, verdict.Risks[llmanalyzer.KeyDestructiveToolCall].Score)
		require.Empty(t, verdict.Risks[llmanalyzer.KeyDestructiveToolCall].Reasoning)
	}
}

func TestParseVerdict_NestedReasoningWinsOverTopLevel(t *testing.T) {
	t.Parallel()

	verdict, err := llmanalyzer.ParseVerdict(`{"destructive_tool_call": 0, "prompt_injection": 0,` +
		` "secrets_leak": {"score": 1, "reasoning": "nested wins"}, "personal_data_leak": 0, "reasoning": "secrets_leak: top level"}`)
	require.NoError(t, err)
	require.Equal(t, "nested wins", verdict.Risks[llmanalyzer.KeySecretsLeak].Reasoning)
}

func TestParseVerdict_CompactReasoningIsCapped(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("é", 600)
	verdict, err := llmanalyzer.ParseVerdict(`{"destructive_tool_call": 0, "prompt_injection": 0, "secrets_leak": 1, "personal_data_leak": 0, "reasoning": "secrets_leak: ` + long + `"}`)
	require.NoError(t, err)
	require.Equal(t, 500, utf8.RuneCountInString(verdict.Risks[llmanalyzer.KeySecretsLeak].Reasoning))
}

func TestParseVerdict_CompactPartialMarkersLeaveUnmarkedRiskEmpty(t *testing.T) {
	t.Parallel()

	// Two flagged risks, a marker for only one: the marked risk gets its sentence
	// and the unmarked one stays empty rather than inheriting the whole string.
	verdict, err := llmanalyzer.ParseVerdict(`{"destructive_tool_call": 1, "prompt_injection": 0, "secrets_leak": 1, "personal_data_leak": 0,` +
		` "reasoning": "secrets_leak: An API key is printed in plaintext."}`)
	require.NoError(t, err)
	require.Equal(t, "An API key is printed in plaintext.", verdict.Risks[llmanalyzer.KeySecretsLeak].Reasoning)
	require.Empty(t, verdict.Risks[llmanalyzer.KeyDestructiveToolCall].Reasoning)
}

func TestParseVerdict_CompactMarkerAfterPunctuation(t *testing.T) {
	t.Parallel()

	// No space after the period: the second marker still starts a segment.
	verdict, err := llmanalyzer.ParseVerdict(`{"destructive_tool_call": 0, "prompt_injection": 1, "secrets_leak": 1, "personal_data_leak": 0,` +
		` "reasoning": "secrets_leak: A token is printed.prompt_injection: The text overrides the agent."}`)
	require.NoError(t, err)
	require.Equal(t, "A token is printed.", verdict.Risks[llmanalyzer.KeySecretsLeak].Reasoning)
	require.Equal(t, "The text overrides the agent.", verdict.Risks[llmanalyzer.KeyPromptInjection].Reasoning)
}

func TestParseVerdict_CompactQuotedKeyNameIsNotAMarker(t *testing.T) {
	t.Parallel()

	// A key name quoted inside a sentence (preceded by '"') must not split the
	// reasoning; the flagged prompt_injection risk has no marker of its own.
	verdict, err := llmanalyzer.ParseVerdict(`{"destructive_tool_call": 0, "prompt_injection": 1, "secrets_leak": 1, "personal_data_leak": 0,` +
		` "reasoning": "secrets_leak: The tool output contains a \"prompt_injection:\" header next to a token."}`)
	require.NoError(t, err)
	require.Equal(t, `The tool output contains a "prompt_injection:" header next to a token.`, verdict.Risks[llmanalyzer.KeySecretsLeak].Reasoning)
	require.Empty(t, verdict.Risks[llmanalyzer.KeyPromptInjection].Reasoning)
}

func TestParseVerdict_CleanShorthand(t *testing.T) {
	t.Parallel()

	for name, text := range map[string]string{
		"bare":            `{"risk": 0}`,
		"string score":    `{"risk": "0"}`,
		"boolean score":   `{"risk": false}`,
		"code fence":      "```json\n{\"risk\": 0}\n```",
		"prose around":    "All clear: {\"risk\": 0} (nothing to report)",
		"stray obj first": `{} {"risk": 0}`,
	} {
		verdict, err := llmanalyzer.ParseVerdict(text)
		require.NoError(t, err, name)
		require.Empty(t, verdict.Flagged(), name)
		require.Len(t, verdict.Risks, 4, name)
		for _, key := range []string{llmanalyzer.KeySecretsLeak, llmanalyzer.KeyPersonalDataLeak, llmanalyzer.KeyPromptInjection, llmanalyzer.KeyDestructiveToolCall} {
			require.Equal(t, llmanalyzer.RiskVerdict{Score: 0, Reasoning: ""}, verdict.Risks[key], name)
		}
		require.Equal(t, text, verdict.Raw, name)
	}
}

func TestParseVerdict_CleanShorthandRejectsFlag(t *testing.T) {
	t.Parallel()

	// {"risk": 1} names no risk to attribute the flag to; fail closed rather
	// than guess, like any other unparsable reply.
	for name, text := range map[string]string{
		"one":          `{"risk": 1}`,
		"true":         `{"risk": true}`,
		"out of range": `{"risk": 2}`,
		"non numeric":  `{"risk": "high"}`,
	} {
		_, err := llmanalyzer.ParseVerdict(text)
		require.ErrorIs(t, err, llmanalyzer.ErrParse, name)
	}
}

func TestParseVerdict_ShorthandKeyNextToRiskKeysIsIgnored(t *testing.T) {
	t.Parallel()

	// A "risk" key alongside the four risk keys is not the shorthand; the
	// four keys decide and the extra key is ignored like any unknown key.
	verdict, err := llmanalyzer.ParseVerdict(`{"risk": 0, "destructive_tool_call": 0, "prompt_injection": 1, "secrets_leak": 0, "personal_data_leak": 0, "reasoning": "prompt_injection: Overrides the agent."}`)
	require.NoError(t, err)
	require.Equal(t, []string{llmanalyzer.KeyPromptInjection}, verdict.Flagged())
	require.Equal(t, "Overrides the agent.", verdict.Risks[llmanalyzer.KeyPromptInjection].Reasoning)
}

func TestParseVerdict_ShorthandWithExtraKeyIsNotAVerdict(t *testing.T) {
	t.Parallel()

	// {"risk": 0, "note": ...} is neither the shorthand nor a full verdict,
	// so the walk skips it and the full verdict after it wins.
	verdict, err := llmanalyzer.ParseVerdict(`{"risk": 0, "note": "draft"} {"destructive_tool_call": 0, "prompt_injection": 0, "secrets_leak": 1, "personal_data_leak": 0}`)
	require.NoError(t, err)
	require.Equal(t, []string{llmanalyzer.KeySecretsLeak}, verdict.Flagged())
}

func TestParseVerdict_SalvagesScoresFromUndecodableReply(t *testing.T) {
	t.Parallel()

	for name, text := range map[string]string{
		"cut off inside reasoning":     `{"destructive_tool_call": 0, "prompt_injection": 1, "secrets_leak": 0, "personal_data_leak": 0, "reasoning": "prompt_injection: The fetched page tells the agent to`,
		"stray quote breaks the json":  `{"destructive_tool_call": 0, "prompt_injection": 1, "secrets_leak": 0, "personal_data_leak": 0, "reasoning": "says "ignore" twice"}`,
		"duplicated reasoning key":     `{"destructive_tool_call": 0, "prompt_injection": 1, "secrets_leak": 0, "personal_data_leak": 0, "reasoning": "reasoning": "prompt_injection: x"}`,
		"nested shape cut off":         `{"destructive_tool_call": {"score": 0, "reasoning": "n/a"}, "prompt_injection": {"score": 1, "reasoning": "overrides"}, "secrets_leak": {"score": 0, "reasoning": "none"}, "personal_data_leak": {"score": 0, "reasoning": "no`,
		"quoted scores and no closing": `{"destructive_tool_call": "0", "prompt_injection": "1", "secrets_leak": "0", "personal_data_leak": "0", "reasoning": "`,
	} {
		verdict, err := llmanalyzer.ParseVerdict(text)
		require.NoError(t, err, name)
		require.Equal(t, []string{llmanalyzer.KeyPromptInjection}, verdict.Flagged(), name)
		require.Empty(t, verdict.Risks[llmanalyzer.KeyPromptInjection].Reasoning, name) // reasoning is what broke; dropped
		require.Equal(t, text, verdict.Raw, name)
	}
}

func TestParseVerdict_SalvagesCleanShorthandFromUndecodableReply(t *testing.T) {
	t.Parallel()

	verdict, err := llmanalyzer.ParseVerdict(`{"risk": 0, "note": "all clear`)
	require.NoError(t, err)
	require.Empty(t, verdict.Flagged())
	require.Len(t, verdict.Risks, 4)
}

func TestParseVerdict_SalvageTakesFirstScorePerKey(t *testing.T) {
	t.Parallel()

	// A looping reply repeats the keys; the first scores are the verdict.
	text := strings.Repeat(`{"destructive_tool_call": 0, "prompt_injection": 0, "secrets_leak": 1, "personal_data_leak": 0, "reasoning": "secrets_leak: `, 3)
	verdict, err := llmanalyzer.ParseVerdict(text)
	require.NoError(t, err)
	require.Equal(t, []string{llmanalyzer.KeySecretsLeak}, verdict.Flagged())
}

func TestParseVerdict_SalvageNeedsEveryRiskKey(t *testing.T) {
	t.Parallel()

	for name, text := range map[string]string{
		"three keys only":         `{"destructive_tool_call": 0, "prompt_injection": 1, "secrets_leak": 0, "reasoning": "`,
		"score out of range":      `{"destructive_tool_call": 0, "prompt_injection": 2, "secrets_leak": 0, "personal_data_leak": 0, "reasoning": "`,
		"shorthand flagged":       `{"risk": 1, "note": "`,
		"shorthand next to a key": `{"risk": 0, "prompt_injection": 1, "reasoning": "`,
		"prose naming keys":       `I checked secrets_leak and prompt_injection and found nothing.`,
	} {
		_, err := llmanalyzer.ParseVerdict(text)
		require.ErrorIs(t, err, llmanalyzer.ErrParse, name)
	}
}
