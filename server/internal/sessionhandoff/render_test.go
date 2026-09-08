package sessionhandoff_test

import (
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/sessionhandoff"
)

func sampleTranscript() *sessionhandoff.Transcript {
	return &sessionhandoff.Transcript{
		Session: sessionhandoff.SessionMeta{
			Title:        "quokka-ledger cutover",
			SessionID:    "0f4a3b2c-1111-4222-8333-444455556666",
			ChatID:       "b6c2f2b8-7777-4888-9999-aaaabbbbcccc",
			Source:       "claude-code",
			Cwd:          "/tmp/quokka",
			LastActivity: time.Date(2026, 8, 1, 10, 1, 0, 0, time.UTC),
		},
		Turns: []sessionhandoff.Turn{
			{Role: "user", Text: "Map the banyan scheduler jobs blocking the quokka-ledger cutover."},
			{Role: "assistant", Text: "Three jobs block the cutover."},
			{Role: "assistant", ToolName: "Edit", ToolInput: `{"file_path":"/tmp/quokka/scheduler.go","old":"a","new":"b"}`},
			{Role: "tool", ToolResult: "edited ok\nsecond line detail"},
			{Role: "assistant", Text: "hedgehog-sweep needs --lease-mode=advisory; README update remains."},
		},
	}
}

// section returns the body of a "## name" section of the handoff.
func section(t *testing.T, markdown, name string) string {
	t.Helper()
	_, after, ok := strings.Cut(markdown, "## "+name+"\n\n")
	require.True(t, ok, "handoff has no %q section\n---\n%s", name, markdown)
	if before, _, cut := strings.Cut(after, "\n## "); cut {
		return before
	}
	return after
}

func TestRenderHandoffStructure(t *testing.T) {
	t.Parallel()
	h := sessionhandoff.Render(sampleTranscript(), sessionhandoff.Options{Budget: 0, RedactToolPayloads: false})

	for _, want := range []string{
		"# Session handoff — quokka-ledger cutover",
		"Recalled from claude-code · source session 0f4a3b2c-1111-4222-8333-444455556666 · gram chat b6c2f2b8-7777-4888-9999-aaaabbbbcccc · project: /tmp/quokka · last active 2026-08-01 10:01 UTC · via Gram",
		"## Original task",
		"Map the banyan scheduler jobs blocking the quokka-ledger cutover.",
		"## What has happened so far",
		"- → Edit ",
		"- → result: edited ok second line detail",
		"## Files touched",
		"- /tmp/quokka/scheduler.go",
		"## Where things stand",
		"hedgehog-sweep needs --lease-mode=advisory; README update remains.",
		"## Instruction",
	} {
		require.Contains(t, h.Markdown, want)
	}
	require.NotEmpty(t, h.Fidelity.Missing)
	require.Contains(t, h.Fidelity.Missing[0], "thinking")
}

func TestRenderBudgetTruncates(t *testing.T) {
	t.Parallel()
	tr := &sessionhandoff.Transcript{
		Session: sampleTranscript().Session,
		Turns:   nil,
	}
	tr.Turns = append(tr.Turns, sessionhandoff.Turn{Role: "user", Text: "the original ask about flotilla"})
	for i := range 40 {
		tr.Turns = append(tr.Turns, sessionhandoff.Turn{
			Role: "assistant", Text: fmt.Sprintf("filler turn %02d %s", i, strings.Repeat("pad ", 100)),
		})
	}
	tr.Turns = append(tr.Turns, sessionhandoff.Turn{Role: "assistant", Text: "final state: kelp-reader rename done"})

	h := sessionhandoff.Render(tr, sessionhandoff.Options{Budget: 2048, RedactToolPayloads: false})

	require.Contains(t, h.Markdown, "the original ask about flotilla", "the original task must survive any budget")
	require.Contains(t, h.Markdown, "final state: kelp-reader rename done", "the last assistant message must survive any budget")
	require.NotContains(t, h.Markdown, "filler turn 01", "early filler turns must fall outside a small budget")
	var truncated bool
	for _, m := range h.Fidelity.Missing {
		if strings.Contains(m, "beyond the handoff budget") {
			truncated = true
		}
	}
	require.True(t, truncated, "fidelity must report budget truncation, got %+v", h.Fidelity)
}

// A positive budget smaller than the section's empty-state marker is raised
// to the marker's length, so the documented bound holds whether the section
// carries the marker or a clipped newest turn.
func TestRenderTinyBudgetStaysBounded(t *testing.T) {
	t.Parallel()
	const marker = "(nothing recorded)\n"

	empty := &sessionhandoff.Transcript{Session: sampleTranscript().Session, Turns: nil}
	h := sessionhandoff.Render(empty, sessionhandoff.Options{Budget: 1, RedactToolPayloads: false})
	require.Contains(t, h.Markdown, marker, "the empty-state marker must render whole")

	h = sessionhandoff.Render(sampleTranscript(), sessionhandoff.Options{Budget: 1, RedactToolPayloads: false})
	body := section(t, h.Markdown, "What has happened so far")
	require.LessOrEqual(t, len(body), len(marker), "section exceeds the effective minimum budget:\n%s", body)
}

// The capture path emits content-empty turns (a tool result whose content
// renders empty, or any tool result under redaction). Those must not consume
// the "keep at least one turn" allowance and leave the section blank when the
// newest real turn overflows the budget.
func TestRenderKeepsNewestTurnBehindEmptyOnes(t *testing.T) {
	t.Parallel()
	big := "narwhal-index rebuild is " + strings.Repeat("still running ", 40)
	tr := &sessionhandoff.Transcript{
		Session: sampleTranscript().Session,
		Turns: []sessionhandoff.Turn{
			{Role: "user", Text: "start the narwhal-index rebuild"},
			{Role: "assistant", Text: big},
			{Role: "tool", ToolResult: ""},
			{Role: "tool", ToolResult: ""},
		},
	}

	const budget = 100
	h := sessionhandoff.Render(tr, sessionhandoff.Options{Budget: budget, RedactToolPayloads: false})
	body := section(t, h.Markdown, "What has happened so far")

	require.NotContains(t, body, "(nothing recorded)", "empty trailing turns suppressed the newest real turn")
	require.Contains(t, body, "narwhal-index rebuild is", "newest real turn missing from section")
	require.LessOrEqual(t, len(body), budget, "section is over the budget:\n%s", body)

	// Clipping that turn drops content, so the report has to admit it —
	// a FidelityReport is only worth anything if it never overstates.
	var warned bool
	for _, w := range h.Fidelity.Warnings {
		if strings.Contains(w, "clipped") {
			warned = true
		}
	}
	require.True(t, warned, "clipping the newest turn must be reported, got %+v", h.Fidelity)
}

// Tool payloads are cut to a byte bound; the cut must land on a rune boundary
// so a multi-byte payload cannot leave invalid UTF-8 in the document.
func TestSummarizeCutsOnRuneBoundary(t *testing.T) {
	t.Parallel()
	tr := &sessionhandoff.Transcript{
		Session: sampleTranscript().Session,
		Turns: []sessionhandoff.Turn{{
			Role: "assistant", ToolName: "Edit",
			ToolInput: strings.Repeat("ü", 300),
		}},
	}

	md := sessionhandoff.Render(tr, sessionhandoff.Options{Budget: 0, RedactToolPayloads: false}).Markdown
	require.True(t, utf8.ValidString(md), "handoff contains invalid UTF-8 after truncating a multi-byte payload")
	for line := range strings.SplitSeq(section(t, md, "What has happened so far"), "\n") {
		// maxSummaryLine (120) plus the "- → Edit " prefix.
		require.LessOrEqual(t, len(line), 120+len("- → Edit "), "summary line is over the bound: %q", line)
	}
}

func TestRenderRedactionFlag(t *testing.T) {
	t.Parallel()
	redacted := sessionhandoff.Render(sampleTranscript(), sessionhandoff.Options{Budget: 0, RedactToolPayloads: true})

	require.Contains(t, redacted.Markdown, "- → Edit\n", "redacted tool line must carry the name only")
	require.NotContains(t, redacted.Markdown, `"old":"a"`, "tool input must not survive redaction")
	require.NotContains(t, redacted.Markdown, "- → result:", "tool result lines must be dropped under redaction")
	require.NotContains(t, redacted.Markdown, "edited ok", "tool result content must not survive redaction")
	require.NotContains(t, redacted.Markdown, "/tmp/quokka/scheduler.go", "no tool-input-derived value may survive redaction")
	require.Contains(t, redacted.Fidelity.Missing, "tool inputs and outputs (redacted; tool names retained)")

	plain := sessionhandoff.Render(sampleTranscript(), sessionhandoff.Options{Budget: 0, RedactToolPayloads: false})

	require.Contains(t, plain.Markdown, `- → Edit {"file_path":"/tmp/quokka/scheduler.go","old":"a","new":"b"}`)
	require.Contains(t, plain.Markdown, "- → result: edited ok second line detail")
	require.NotContains(t, plain.Fidelity.Missing, "tool inputs and outputs (redacted; tool names retained)")
}

func TestRenderFidelitySeed(t *testing.T) {
	t.Parallel()
	h := sessionhandoff.Render(sampleTranscript(), sessionhandoff.Options{Budget: 0, RedactToolPayloads: false})

	require.Equal(t, []string{
		"assistant thinking (not captured)",
		"intermediate assistant messages within turns (capture records each turn's final message only)",
	}, h.Fidelity.Missing)
	require.Contains(t, h.Fidelity.Warnings,
		"rendered from Gram's captured transcript, not the harness's native session file; lower fidelity than a device-local move")

	redacted := sessionhandoff.Render(sampleTranscript(), sessionhandoff.Options{Budget: 0, RedactToolPayloads: true})
	require.Equal(t, []string{
		"assistant thinking (not captured)",
		"intermediate assistant messages within turns (capture records each turn's final message only)",
		"tool inputs and outputs (redacted; tool names retained)",
		"files touched (derived from tool inputs, which are redacted)",
	}, redacted.Fidelity.Missing)
}

func TestRenderBylineLineageMarkers(t *testing.T) {
	t.Parallel()
	tr := sampleTranscript()
	md := sessionhandoff.Render(tr, sessionhandoff.Options{Budget: 0, RedactToolPayloads: true}).Markdown

	require.Contains(t, md, "source session "+tr.Session.SessionID, "the lineage marker must ride in the byline verbatim")
	require.Contains(t, md, "gram chat "+tr.Session.ChatID)
	require.Contains(t, md, "· via Gram")
}

func TestRenderWarnsWhenCwdMissing(t *testing.T) {
	t.Parallel()
	tr := sampleTranscript()
	tr.Session.Cwd = ""

	h := sessionhandoff.Render(tr, sessionhandoff.Options{Budget: 0, RedactToolPayloads: true})
	require.Contains(t, h.Fidelity.Warnings, "working directory was not captured for this session")
	require.Contains(t, h.Markdown, "project: (unknown)")

	withCwd := sessionhandoff.Render(sampleTranscript(), sessionhandoff.Options{Budget: 0, RedactToolPayloads: true})
	require.NotContains(t, withCwd.Fidelity.Warnings, "working directory was not captured for this session")
}

func TestRenderFilesTouchedUnderRedaction(t *testing.T) {
	t.Parallel()
	h := sessionhandoff.Render(sampleTranscript(), sessionhandoff.Options{Budget: 0, RedactToolPayloads: true})

	// Paths are parsed from raw tool inputs, which redaction must not read —
	// the section is omitted and reported missing instead.
	require.NotContains(t, h.Markdown, "## Files touched")
	require.NotContains(t, h.Markdown, "/tmp/quokka/scheduler.go")
	require.Contains(t, h.Fidelity.Missing, "files touched (derived from tool inputs, which are redacted)")

	unredacted := sessionhandoff.Render(sampleTranscript(), sessionhandoff.Options{Budget: 0, RedactToolPayloads: false})
	require.Contains(t, section(t, unredacted.Markdown, "Files touched"), "- /tmp/quokka/scheduler.go")
	require.NotContains(t, unredacted.Fidelity.Missing, "files touched (derived from tool inputs, which are redacted)")
}
