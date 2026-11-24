package mcpfeedback_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcpfeedback"
)

func TestParseReportAcceptsEveryField(t *testing.T) {
	t.Parallel()

	report, err := mcpfeedback.ParseReport(json.RawMessage(`{
		"content": "  search returned nothing although the page shows rows.  ",
		"severity": "Degraded",
		"workaround": "Widened the range.",
		"intention": "Find a failing request.",
		"tool": "search_logs",
		"model": "Claude in Claude Code",
		"something_else": {"ignored": true}
	}`))
	require.NoError(t, err)
	require.Equal(t, mcpfeedback.Report{
		Content:    "search returned nothing although the page shows rows.",
		Severity:   mcpfeedback.SeverityDegraded,
		Workaround: "Widened the range.",
		Intention:  "Find a failing request.",
		ToolName:   "search_logs",
		Model:      "Claude in Claude Code",
	}, report)
}

func TestParseReportNeedsOnlyContent(t *testing.T) {
	t.Parallel()

	report, err := mcpfeedback.ParseReport(json.RawMessage(`{"content":"The tool descriptions are clear."}`))
	require.NoError(t, err)
	require.Equal(t, "The tool descriptions are clear.", report.Content)
	require.Equal(t, mcpfeedback.SeverityUnspecified, report.Severity)
	require.Empty(t, report.Workaround)
	require.Empty(t, report.ToolName)
}

func TestParseReportDropsWhatAColumnCannotHold(t *testing.T) {
	t.Parallel()

	report, err := mcpfeedback.ParseReport(json.RawMessage(`{"content":"first line\r\nsecond\u0000 line\ttabbed\u0007"}`))
	require.NoError(t, err)
	require.Equal(t, "first line\nsecond line\ttabbed", report.Content)

	// C1 controls, bidi overrides and zero-width marks go too, so stored text
	// reads as what it says.
	report, err = mcpfeedback.ParseReport(json.RawMessage(`{"content":"refund\u0085 failed\u202e for\u200b order"}`))
	require.NoError(t, err)
	require.Equal(t, "refund failed for order", report.Content)
}

func TestParseReportRefusesWhatAnAgentCanCorrect(t *testing.T) {
	t.Parallel()

	long := func(n int) string { return strings.Repeat("é", n) }

	cases := []struct {
		name      string
		arguments string
		message   string
	}{
		{name: "no arguments", arguments: ``, message: "arguments must be an object with a content field"},
		{name: "null arguments", arguments: `null`, message: "arguments must be an object with a content field"},
		{name: "array arguments", arguments: `["content"]`, message: "arguments must be an object with a content field"},
		{name: "missing content", arguments: `{"severity":"blocked"}`, message: "content is required"},
		{name: "blank content", arguments: `{"content":"  \n "}`, message: "content is required"},
		{name: "content is not a string", arguments: `{"content":42}`, message: "every field must be a string"},
		{name: "unknown severity", arguments: `{"content":"x","severity":"urgent"}`, message: "severity must be blocked, degraded or cosmetic"},
		{name: "content too long", arguments: `{"content":"` + long(mcpfeedback.MaxContentLength+1) + `"}`, message: "content must be at most 2000 characters"},
		{name: "workaround too long", arguments: `{"content":"x","workaround":"` + long(mcpfeedback.MaxWorkaroundLength+1) + `"}`, message: "workaround must be at most 1000 characters"},
		{name: "intention too long", arguments: `{"content":"x","intention":"` + long(mcpfeedback.MaxIntentionLength+1) + `"}`, message: "intention must be at most 1000 characters"},
		{name: "tool too long", arguments: `{"content":"x","tool":"` + long(mcpfeedback.MaxReportedToolNameLength+1) + `"}`, message: "tool must be at most 100 characters"},
		{name: "model too long", arguments: `{"content":"x","model":"` + long(mcpfeedback.MaxModelLength+1) + `"}`, message: "model must be at most 200 characters"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := mcpfeedback.ParseReport(json.RawMessage(tc.arguments))
			var invalid *mcpfeedback.InvalidReportError
			require.ErrorAs(t, err, &invalid)
			require.Equal(t, tc.message, invalid.Message)
		})
	}
}

// The limits count characters, not bytes: a report at the limit in a
// multi-byte script is accepted.
func TestParseReportCountsCharacters(t *testing.T) {
	t.Parallel()

	content := strings.Repeat("é", mcpfeedback.MaxContentLength)
	report, err := mcpfeedback.ParseReport(json.RawMessage(`{"content":"` + content + `"}`))
	require.NoError(t, err)
	require.Equal(t, content, report.Content)
}

func TestParseSeverity(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		value string
		want  mcpfeedback.Severity
	}{
		{value: "", want: mcpfeedback.SeverityUnspecified},
		{value: "blocked", want: mcpfeedback.SeverityBlocked},
		// Agents are not exact about case or spacing.
		{value: " DEGRADED", want: mcpfeedback.SeverityDegraded},
		{value: "cosmetic", want: mcpfeedback.SeverityCosmetic},
	} {
		t.Run(fmt.Sprintf("%q", tc.value), func(t *testing.T) {
			t.Parallel()
			got, ok := mcpfeedback.ParseSeverity(tc.value)
			require.True(t, ok)
			require.Equal(t, tc.want, got)
		})
	}

	_, ok := mcpfeedback.ParseSeverity("critical")
	require.False(t, ok)
}
