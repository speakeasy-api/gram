// Package efficacy scores how well a skill served an agent session: it renders
// the session transcript, asks an LLM judge for a structured verdict, and
// normalizes that verdict for the skill_efficacy_scores ClickHouse sink.
package efficacy

import (
	"cmp"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	// maxTranscriptRunes bounds the rendered transcript. Whole messages are
	// dropped oldest-first until the payload fits, so the judge always keeps the
	// end of the session - where the skill's effect actually shows - instead of
	// losing it to a tail truncation.
	maxTranscriptRunes = 120000
	// maxMessageBodyRunes caps one message's content and one tool outcome note.
	maxMessageBodyRunes = 8000
	// maxToolCallArgumentRunes caps one tool call's argument blob. It is well
	// below the body cap because a message renders many calls but one body.
	maxToolCallArgumentRunes = 2000
	// maxMetadataRunes caps the short identifier fields - role, tool name, tool
	// call id, tool URN, outcome. Nothing legitimate approaches it, so it exists
	// purely to stop a hostile writer from smuggling a body-sized blob through a
	// field nobody budgets for.
	maxMetadataRunes = 512
	// maxRenderedToolCalls caps how many calls a single assistant message
	// renders; oversized call lists keep head and tail calls.
	maxRenderedToolCalls = 8
	// truncationMarker stands in for the runes truncateRunes dropped.
	truncationMarker = "\n…[%d characters truncated]…\n"
	// omittedMarker stands in for the messages the rendering dropped. The paged
	// loader restates it with the messages it never read folded into the count,
	// so a paged transcript and a fully-loaded one differ in the number, never
	// the shape.
	omittedMarker = "[%d earlier messages omitted]"
)

// renderCaps is the per-field budget one rendering pass applies. The default
// caps sum to roughly a third of maxTranscriptRunes, which leaves room for JSON
// escaping; fitMessage halves them when a message escapes worse than that.
type renderCaps struct {
	body      int
	arguments int
	metadata  int
	toolCalls int
}

var (
	defaultCaps = renderCaps{
		body:      maxMessageBodyRunes,
		arguments: maxToolCallArgumentRunes,
		metadata:  maxMetadataRunes,
		toolCalls: maxRenderedToolCalls,
	}
	// floorCaps is where halved() converges: no bodies, no arguments, one rune of
	// each identifier, one tool call. A message rendered at the floor is a few
	// hundred runes even fully escaped, so it always fits maxTranscriptRunes -
	// which is what makes the fit loop terminate.
	floorCaps = renderCaps{body: 0, arguments: 0, metadata: 1, toolCalls: 1}
)

func (c renderCaps) halved() renderCaps {
	return renderCaps{
		body:      c.body / 2,
		arguments: c.arguments / 2,
		metadata:  max(1, c.metadata/2),
		toolCalls: max(1, c.toolCalls/2),
	}
}

// Transcript is the judge-visible rendering of a chat. It is encoded as JSON in
// the judge's user turn, so hostile message text is always a quoted string in a
// known field and can never spoof a transcript heading. Omitted is declared
// first so the omission marker is prepended to the rendered messages.
type Transcript struct {
	Omitted  string              `json:"omitted,omitempty"`
	Messages []TranscriptMessage `json:"messages"`
}

// TranscriptMessage is one chat message as the judge sees it: who spoke, what
// they said, what tools they called and how those calls turned out.
type TranscriptMessage struct {
	Index                     int                  `json:"index"`
	CreatedAt                 string               `json:"created_at,omitempty"`
	SecondsSincePrevious      *float64             `json:"seconds_since_previous,omitempty"`
	Role                      string               `json:"role"`
	Content                   string               `json:"content,omitempty"`
	ContentTruncated          bool                 `json:"content_truncated,omitempty"`
	ToolCalls                 []TranscriptToolCall `json:"tool_calls,omitempty"`
	ToolCallsTruncated        bool                 `json:"tool_calls_truncated,omitempty"`
	ToolCallID                string               `json:"tool_call_id,omitempty"`
	ToolURN                   string               `json:"tool_urn,omitempty"`
	ToolOutcome               string               `json:"tool_outcome,omitempty"`
	ToolOutcomeNotes          string               `json:"tool_outcome_notes,omitempty"`
	ToolOutcomeNotesTruncated bool                 `json:"tool_outcome_notes_truncated,omitempty"`
}

// TranscriptToolCall is one invocation an assistant message requested.
type TranscriptToolCall struct {
	ID                 string `json:"id,omitempty"`
	Name               string `json:"name,omitempty"`
	Arguments          string `json:"arguments,omitempty"`
	ArgumentsTruncated bool   `json:"arguments_truncated,omitempty"`
}

// TranscriptInput is one stored chat message as the rendering reads it: the
// columns transcript rendering touches and nothing else. The loader projects
// page rows onto it, so a message body the rendering will not use never enters
// the shape at all.
type TranscriptInput struct {
	ID               uuid.UUID
	Seq              int64
	CreatedAt        pgtype.Timestamptz
	Role             string
	Content          string
	ToolCalls        []byte
	ToolCallID       pgtype.Text
	ToolURN          urn.Tool
	ToolOutcome      pgtype.Text
	ToolOutcomeNotes pgtype.Text
}

// storedToolCall is the persisted chat_messages.tool_calls shape: the OpenAI
// tool-call envelope every writer emits (server/internal/chat/message_capture_strategy.go:292,
// server/internal/hooks/codex_hooks.go:520-529).
type storedToolCall struct {
	ID       string `json:"id"`
	Function struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	} `json:"function"`
}

// RenderTranscript renders messages chronologically and trims whole messages
// oldest-first until the rendering fits maxTranscriptRunes, prepending a
// "[N earlier messages omitted]" marker when it drops any. The newest message is
// always kept: renderMessage guarantees every single message fits
// maxTranscriptRunes on its own, so keeping it cannot breach the budget. The
// judge's own instructions travel in SystemPrompt, not in the transcript, so
// they are never subject to this budget.
//
// Callers load messages with the project-scoped
// ListChatTranscriptMessagesPage, which already orders by (created_at, seq);
// the sort here makes the rendering deterministic for any input order.
func RenderTranscript(messages []TranscriptInput) Transcript {
	ordered := slices.Clone(messages)
	slices.SortStableFunc(ordered, func(a, b TranscriptInput) int {
		if c := a.CreatedAt.Time.Compare(b.CreatedAt.Time); c != 0 {
			return c
		}
		if c := cmp.Compare(a.Seq, b.Seq); c != 0 {
			return c
		}
		return strings.Compare(a.ID.String(), b.ID.String())
	})

	rendered := make([]TranscriptMessage, 0, len(ordered))
	sizes := make([]int, 0, len(ordered))
	total := 0
	for i, m := range ordered {
		msg, _ := renderMessage(m)
		msg.Index = i + 1
		if m.CreatedAt.Valid {
			msg.CreatedAt = m.CreatedAt.Time.UTC().Format(time.RFC3339Nano)
		}
		if i > 0 && m.CreatedAt.Valid && ordered[i-1].CreatedAt.Valid {
			seconds := max(0, m.CreatedAt.Time.Sub(ordered[i-1].CreatedAt.Time).Seconds())
			msg.SecondsSincePrevious = &seconds
		}
		size := renderedSize(msg)
		rendered = append(rendered, msg)
		sizes = append(sizes, size)
		total += size
	}

	dropped := 0
	for dropped < len(rendered)-1 && total > maxTranscriptRunes {
		total -= sizes[dropped]
		dropped++
	}

	omitted := ""
	if dropped > 0 {
		omitted = fmt.Sprintf(omittedMarker, dropped)
	}
	if dropped < len(rendered) {
		rendered[dropped].SecondsSincePrevious = nil
	}

	return Transcript{Omitted: omitted, Messages: rendered[dropped:]}
}

// renderMessage renders m at the default caps and, if the result still exceeds
// maxTranscriptRunes once JSON-encoded, re-renders it at successively halved
// caps until it fits. Re-rendering from the source message rather than shrinking
// an already-truncated rendering keeps the output a pure function of m, so the
// result is deterministic and never stacks truncation markers.
func renderMessage(m TranscriptInput) (TranscriptMessage, int) {
	caps := defaultCaps
	for {
		msg := renderMessageWith(m, caps)
		size := renderedSize(msg)
		if size <= maxTranscriptRunes || caps == floorCaps {
			return msg, size
		}
		caps = caps.halved()
	}
}

func renderMessageWith(m TranscriptInput, caps renderCaps) TranscriptMessage {
	content, contentTruncated := truncateRunes(m.Content, caps.body)
	notes, notesTruncated := truncateRunes(m.ToolOutcomeNotes.String, caps.body)

	toolURN := ""
	if !m.ToolURN.IsZero() {
		toolURN = m.ToolURN.String()
	}

	calls, callsTruncated := renderToolCalls(m.ToolCalls, caps)

	role, _ := truncateRunes(m.Role, caps.metadata)
	callID, _ := truncateRunes(m.ToolCallID.String, caps.metadata)
	urnText, _ := truncateRunes(toolURN, caps.metadata)
	outcome, _ := truncateRunes(m.ToolOutcome.String, caps.metadata)

	return TranscriptMessage{
		Index:                     0,
		CreatedAt:                 "",
		SecondsSincePrevious:      nil,
		Role:                      role,
		Content:                   content,
		ContentTruncated:          contentTruncated,
		ToolCalls:                 calls,
		ToolCallsTruncated:        callsTruncated,
		ToolCallID:                callID,
		ToolURN:                   urnText,
		ToolOutcome:               outcome,
		ToolOutcomeNotes:          notes,
		ToolOutcomeNotesTruncated: notesTruncated,
	}
}

func renderToolCalls(raw []byte, caps renderCaps) ([]TranscriptToolCall, bool) {
	if len(raw) == 0 {
		return nil, false
	}

	var stored []storedToolCall
	if err := json.Unmarshal(raw, &stored); err != nil {
		// An envelope this package cannot decode still carries what the assistant
		// asked for, so it is handed to the judge verbatim rather than dropped.
		args, truncated := truncateRunes(string(raw), caps.arguments)
		return []TranscriptToolCall{{ID: "", Name: "", Arguments: args, ArgumentsTruncated: truncated}}, false
	}

	truncatedCalls := false
	if len(stored) > caps.toolCalls {
		head := caps.toolCalls / 2
		tail := caps.toolCalls - head
		kept := make([]storedToolCall, 0, caps.toolCalls)
		kept = append(kept, stored[:head]...)
		kept = append(kept, stored[len(stored)-tail:]...)
		stored = kept
		truncatedCalls = true
	}

	calls := make([]TranscriptToolCall, 0, len(stored))
	for _, c := range stored {
		args, truncated := truncateRunes(decodeToolCallArguments(c.Function.Arguments), caps.arguments)
		id, _ := truncateRunes(c.ID, caps.metadata)
		name, _ := truncateRunes(c.Function.Name, caps.metadata)
		calls = append(calls, TranscriptToolCall{
			ID:                 id,
			Name:               name,
			Arguments:          args,
			ArgumentsTruncated: truncated,
		})
	}
	return calls, truncatedCalls
}

// decodeToolCallArguments unwraps the arguments field, which providers emit
// either as a JSON-encoded string or as a bare JSON object.
func decodeToolCallArguments(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	return string(raw)
}

// renderedSize measures a message the same way the judge payload encodes it, so
// the trim loop bounds what is actually sent.
func renderedSize(m TranscriptMessage) int {
	b, err := json.Marshal(m)
	if err != nil {
		// Unreachable: every field is a string, bool or slice of those. Fall back
		// to the fields that carry the bulk so a future field type that can fail
		// to marshal cannot make a message look free.
		return utf8.RuneCountInString(m.Content) + utf8.RuneCountInString(m.ToolOutcomeNotes)
	}
	return utf8.RuneCount(b)
}

// truncateRunes keeps the head and tail of s with an omission marker between
// them, and never returns more than maxRunes runes - the marker is charged to
// the same budget, because a caller that sizes a field to a limit gets no value
// from a result that overshoots it.
func truncateRunes(s string, maxRunes int) (string, bool) {
	if utf8.RuneCountInString(s) <= maxRunes {
		return s, false
	}
	runes := []rune(s)

	// The marker states how many runes it replaced, so its own width depends on
	// the count it prints. Budgeting against the widest possible count - every
	// rune omitted - breaks the cycle; a shorter real count only leaves slack.
	markerBudget := utf8.RuneCountInString(fmt.Sprintf(truncationMarker, len(runes)))
	if markerBudget >= maxRunes {
		return string(runes[:max(0, maxRunes)]), true
	}

	head := (maxRunes - markerBudget) * 3 / 5
	tail := maxRunes - markerBudget - head
	var b strings.Builder
	b.WriteString(string(runes[:head]))
	fmt.Fprintf(&b, truncationMarker, len(runes)-head-tail)
	b.WriteString(string(runes[len(runes)-tail:]))
	return b.String(), true
}
