// Package sessionhandoff renders a captured Gram chat transcript into a
// deterministic markdown handoff digest — the document served to a harness
// that wants to continue a previous session's work. No LLM sits in this path:
// the same transcript always renders the same digest, which is what makes the
// FidelityReport trustworthy.
//
// The renderer is a stdlib-only port of the device-agent handoff renderer
// (core/sessionport, ADR-0018 session portability), adapted to transcripts
// recalled from Gram's capture store rather than a harness's native session
// file.
package sessionhandoff

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// DefaultBudget bounds the "what has happened so far" section, in bytes of
// rendered output (a rough proxy for tokens). The task statement and the
// final assistant message are always included on top of it.
const DefaultBudget = 24 << 10

// emptyRecentMarker stands in for the recent-turns section when nothing fits
// or nothing was recorded. Its length is the effective minimum budget: a
// positive Options.Budget below it is raised so the section never exceeds its
// documented bound just to hold the marker.
const emptyRecentMarker = "(nothing recorded)\n"

// maxSummaryLine bounds one-line tool summaries inside the handoff.
const maxSummaryLine = 120

// maxTitleRunes bounds the rendered title's rune length.
const maxTitleRunes = 96

// maxContextSection bounds the Original-task and Where-things-stand sections
// in bytes. Both render outside the recent-turns budget, so without their own
// bound a single huge message would blow the document past every advertised
// limit.
const maxContextSection = 4 << 10

// maxMetaField bounds caller-supplied byline fields and extracted file paths
// in bytes — all of them render outside the recent-turns budget and none is
// trustworthy-length input.
const maxMetaField = 512

// SessionMeta identifies the captured session a transcript came from.
type SessionMeta struct {
	// Title is the session's display title (chats.title), resolved by the
	// caller. Empty falls back to SessionID in the rendered document.
	Title string

	// SessionID is the harness-native session identifier the capture pipeline
	// recorded (external_chat_id), falling back to the chat uuid.
	SessionID string

	// ChatID is the Gram chat uuid the transcript was recalled from.
	ChatID string

	// Source is the harness that produced the session (a capture surface slug
	// such as "claude-code").
	Source string

	// Cwd is the working directory the session ran in, when recorded.
	Cwd string

	// LastActivity is when the chat was last updated.
	LastActivity time.Time
}

// Turn is one step of the neutral transcript.
type Turn struct {
	// Role is "user", "assistant", or "tool" (a tool result).
	Role string

	// Text is the turn's prose, when it has any.
	Text string

	// ToolName and ToolInput describe a tool invocation requested by the
	// assistant. Both are verbatim strings — the renderer decides how to
	// compress or redact them.
	ToolName  string
	ToolInput string

	// ToolResult carries a tool turn's output, verbatim.
	ToolResult string

	// At is the turn's timestamp, zero when not recorded.
	At time.Time
}

// Transcript is a fully assembled session recall.
type Transcript struct {
	Session SessionMeta
	Turns   []Turn
}

// FidelityReport states what the handoff does not carry. It is surfaced as
// structured fields next to the digest: the product promise is "continue with
// your full context", never "identical session".
type FidelityReport struct {
	// Missing lists content categories absent from the handoff.
	Missing []string

	// Warnings lists degradations short of missing content.
	Warnings []string
}

// Handoff is a rendered handoff document plus the honesty report that goes
// with it.
type Handoff struct {
	Markdown string
	Fidelity FidelityReport

	// TurnsIncluded and TurnsDropped account for the recent-turns section
	// numerically — how many transcript turns fit the budget and how many fell
	// outside it — so callers can record content-free counters (audit) without
	// re-parsing the FidelityReport's prose.
	TurnsIncluded int
	TurnsDropped  int
}

// Options controls how Render compresses and redacts the transcript.
type Options struct {
	// Budget bounds the recent-turns section in bytes; <= 0 uses
	// DefaultBudget, and positive values are raised to the length of the
	// section's empty-state marker so the bound always holds.
	Budget int

	// RedactToolPayloads drops tool inputs and outputs from the digest,
	// keeping tool names only. The files-touched section is omitted too: it is
	// derived from tool inputs, which redaction must not read.
	RedactToolPayloads bool
}

// Render produces the handoff document for a recalled transcript.
func Render(t *Transcript, opts Options) Handoff {
	budget := opts.Budget
	if budget <= 0 {
		budget = DefaultBudget
	}
	if budget < len(emptyRecentMarker) {
		budget = len(emptyRecentMarker)
	}

	fidelity := FidelityReport{
		// The capture pipeline never records assistant thinking, and it
		// stores each turn's final assistant message only.
		Missing: []string{
			"assistant thinking (not captured)",
			"intermediate assistant messages within turns (capture records each turn's final message only)",
		},
		Warnings: []string{
			"rendered from Gram's captured transcript, not the harness's native session file; lower fidelity than a device-local move",
		},
	}
	if opts.RedactToolPayloads {
		fidelity.Missing = append(fidelity.Missing,
			"tool inputs and outputs (redacted; tool names retained)",
			"files touched (derived from tool inputs, which are redacted)",
		)
	}
	if t.Session.Cwd == "" {
		fidelity.Warnings = append(fidelity.Warnings, "working directory was not captured for this session")
	}
	if len(t.Turns) == 0 {
		fidelity.Warnings = append(fidelity.Warnings, "transcript is empty; handoff carries metadata only")
	}

	firstPrompt := firstUserText(t.Turns)
	if bounded := clip(firstPrompt, maxContextSection); bounded != firstPrompt {
		firstPrompt = bounded
		fidelity.Warnings = append(fidelity.Warnings, "the original task statement was too large and appears clipped")
	}
	lastAssistant := lastAssistantText(t.Turns)
	if bounded := clip(lastAssistant, maxContextSection); bounded != lastAssistant {
		lastAssistant = bounded
		fidelity.Warnings = append(fidelity.Warnings, "the final assistant message was too large and appears clipped")
	}

	recent, dropped, clipped := renderRecent(t.Turns, budget, opts.RedactToolPayloads)
	if dropped > 0 {
		fidelity.Missing = append(fidelity.Missing, fmt.Sprintf("%d earlier turns beyond the handoff budget", dropped))
	}
	if clipped {
		// The turn is present but shortened, so this is a degradation
		// rather than absent content — either way the report must say so.
		fidelity.Warnings = append(fidelity.Warnings, "the most recent turn was too large for the handoff budget and appears clipped")
	}

	// Under redaction the section is omitted entirely (and reported missing
	// above) rather than derived from inputs the caller asked us not to read.
	var files []string
	if !opts.RedactToolPayloads {
		files = filesTouched(t.Turns)
	}

	var b strings.Builder
	title := t.Session.Title
	if title == "" {
		title = t.Session.SessionID
	}
	fmt.Fprintf(&b, "# Session handoff — %s\n\n", truncateTitle(title))
	// The source session id is the lineage marker: it lands in the
	// continuation's own context/transcript, so a continuation whose new id
	// nobody could know at recall time can still be tied back to its origin
	// later. The gram chat uuid rides next to it for the same reason.
	fmt.Fprintf(&b, "Recalled from %s · source session %s · gram chat %s · project: %s · last active %s · via Gram\n\n",
		clip(t.Session.Source, maxMetaField), clip(t.Session.SessionID, maxMetaField), t.Session.ChatID,
		clip(valueOr(t.Session.Cwd, "(unknown)"), maxMetaField), t.Session.LastActivity.Format("2006-01-02 15:04 MST"))

	b.WriteString("## Original task\n\n")
	b.WriteString(valueOr(firstPrompt, "(no user prompt recorded)"))
	b.WriteString("\n\n")

	b.WriteString("## What has happened so far\n\n")
	if recent == "" {
		b.WriteString(emptyRecentMarker)
	} else {
		b.WriteString(recent)
	}
	b.WriteString("\n")

	if len(files) > 0 {
		b.WriteString("## Files touched\n\n")
		for _, f := range files {
			fmt.Fprintf(&b, "- %s\n", f)
		}
		b.WriteString("\n")
	}

	b.WriteString("## Where things stand\n\n")
	b.WriteString(valueOr(lastAssistant, "(no assistant message recorded)"))
	b.WriteString("\n\n")

	b.WriteString("## Instruction\n\n")
	b.WriteString("This context was recalled from a previous session. Treat it as background for the current conversation: confirm your understanding of where that work stood in one short paragraph, then continue with the user's current request.\n")

	return Handoff{
		Markdown:      b.String(),
		Fidelity:      fidelity,
		TurnsIncluded: len(t.Turns) - dropped,
		TurnsDropped:  dropped,
	}
}

// renderRecent walks turns newest-first, accumulating rendered entries until
// the byte budget is exhausted, then restores chronological order. It returns
// the section body, how many turns fell outside the budget, and whether the
// newest turn had to be clipped to fit — every way this drops content, so the
// FidelityReport can account for all of them.
func renderRecent(turns []Turn, budget int, redactToolPayloads bool) (body string, dropped int, clipped bool) {
	var entries []string
	used := 0
	included := 0
	for i := len(turns) - 1; i >= 0; i-- {
		entry := renderTurn(turns[i], redactToolPayloads)
		if entry == "" {
			included++ // skipped-by-content turns don't count as lost
			continue
		}
		if used+len(entry) > budget {
			// Guard on entries, not included: a leading run of content-empty
			// turns (empty or redacted tool results) must not suppress the
			// newest real turn.
			if len(entries) > 0 {
				return joinReversed(entries), len(turns) - included, clipped
			}
			// Keep that newest turn, but bound it — one oversized turn
			// must not blow the budget the section advertises.
			entry = strings.TrimRight(clip(entry, budget-1), "\n") + "\n"
			clipped = true
		}
		entries = append(entries, entry)
		used += len(entry)
		included++
	}
	return joinReversed(entries), 0, clipped
}

func joinReversed(entries []string) string {
	var b strings.Builder
	for i := len(entries) - 1; i >= 0; i-- {
		b.WriteString(entries[i])
	}
	return b.String()
}

// renderTurn renders one turn: prose verbatim, tool traffic as one-liners.
// Under redaction, tool invocations keep their name only and tool results are
// dropped entirely.
func renderTurn(t Turn, redactToolPayloads bool) string {
	switch {
	case t.ToolName != "":
		if redactToolPayloads {
			return fmt.Sprintf("- → %s\n", t.ToolName)
		}
		return fmt.Sprintf("- → %s %s\n", t.ToolName, summarize(t.ToolInput))
	case t.ToolResult != "":
		if redactToolPayloads {
			return ""
		}
		return fmt.Sprintf("- → result: %s\n", summarize(t.ToolResult))
	case strings.TrimSpace(t.Text) != "":
		role := "User"
		if t.Role == "assistant" {
			role = "Assistant"
		}
		return fmt.Sprintf("**%s:** %s\n\n", role, strings.TrimSpace(t.Text))
	default:
		return ""
	}
}

// summarize compresses a tool payload onto one bounded line.
func summarize(s string) string {
	return clip(strings.Join(strings.Fields(s), " "), maxSummaryLine)
}

// clip bounds s to maxBytes bytes — ellipsis included, so the result never
// exceeds maxBytes — cutting back to a rune boundary so a multi-byte payload
// can never leave invalid UTF-8 in the document.
func clip(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	const ellipsis = "…"
	keep := maxBytes - len(ellipsis)
	if keep <= 0 {
		return ""
	}
	for keep > 0 && !utf8.RuneStart(s[keep]) {
		keep--
	}
	return s[:keep] + ellipsis
}

// filesTouched extracts file paths from tool inputs — the JSON keys the
// known harness tool schemas use for their file arguments. Best-effort by
// design: it feeds a context list, not an audit.
func filesTouched(turns []Turn) []string {
	seen := make(map[string]bool)
	var files []string
	for _, t := range turns {
		if t.ToolInput == "" {
			continue
		}
		var input map[string]any
		if err := json.Unmarshal([]byte(t.ToolInput), &input); err != nil {
			continue
		}
		for _, key := range []string{"file_path", "path", "filename", "notebook_path"} {
			if v, ok := input[key].(string); ok && strings.TrimSpace(v) != "" {
				v = clip(v, maxMetaField)
				if !seen[v] {
					seen[v] = true
					files = append(files, v)
				}
			}
		}
	}
	const maxFiles = 20
	if len(files) > maxFiles {
		files = append(files[:maxFiles], fmt.Sprintf("… and %d more", len(files)-maxFiles))
	}
	return files
}

func firstUserText(turns []Turn) string {
	for _, t := range turns {
		if t.Role == "user" && strings.TrimSpace(t.Text) != "" && isSubstantivePrompt(t.Text) {
			return strings.TrimSpace(t.Text)
		}
	}
	return ""
}

func lastAssistantText(turns []Turn) string {
	for i := len(turns) - 1; i >= 0; i-- {
		if turns[i].Role == "assistant" && strings.TrimSpace(turns[i].Text) != "" {
			return strings.TrimSpace(turns[i].Text)
		}
	}
	return ""
}

func valueOr(s, fallback string) string {
	if strings.TrimSpace(s) == "" {
		return fallback
	}
	return s
}

// isSubstantivePrompt filters harness-generated user records (command
// wrappers, context envelopes) so the original-task section reflects what the
// human actually asked.
func isSubstantivePrompt(text string) bool {
	text = strings.TrimSpace(text)
	return text != "" && !isEnvelope(text)
}

// isEnvelope reports whether text opens with an XML-ish element that closes
// again inside the same prompt — the shape every harness envelope has
// (<command-name>…</command-name>, <local-command-stdout>…). Requiring the
// close tag keeps prose that merely starts with a bracket ("<div> renders
// wrong") as a usable title.
func isEnvelope(text string) bool {
	if !strings.HasPrefix(text, "<") {
		return false
	}
	name := text[1:]
	end := strings.IndexAny(name, " \t\r\n>")
	if end <= 0 {
		return false
	}
	name = name[:end]
	return strings.Contains(text, "</"+name+">")
}

// truncateTitle collapses whitespace and bounds the rune length of the
// rendered title.
func truncateTitle(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) <= maxTitleRunes {
		return s
	}
	runes := []rune(s)
	return string(runes[:maxTitleRunes-1]) + "…"
}
