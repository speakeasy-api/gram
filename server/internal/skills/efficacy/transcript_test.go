package efficacy

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/urn"
)

var transcriptBase = time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)

func transcriptMessage(seq int64, role, content string) TranscriptInput {
	return TranscriptInput{
		ID:        uuid.New(),
		Seq:       seq,
		Role:      role,
		Content:   content,
		CreatedAt: pgtype.Timestamptz{Time: transcriptBase.Add(time.Duration(seq) * time.Minute), Valid: true, InfinityModifier: pgtype.Finite},
	}
}

func TestRenderTranscriptIsChronologicalAndCarriesToolData(t *testing.T) {
	t.Parallel()

	toolURN := urn.NewTool(urn.ToolKindHTTP, "petstore", "list-pets")

	assistant := transcriptMessage(2, "assistant", "calling the tool")
	assistant.ToolCalls = []byte(`[{"id":"call_1","type":"function","function":{"name":"list_pets","arguments":"{\"limit\":3}"}}]`)

	toolResult := transcriptMessage(3, "tool", "three pets")
	toolResult.ToolCallID = pgtype.Text{String: "call_1", Valid: true}
	toolResult.ToolURN = toolURN
	toolResult.ToolOutcome = pgtype.Text{String: "success", Valid: true}
	toolResult.ToolOutcomeNotes = pgtype.Text{String: "returned 3 rows", Valid: true}

	// Deliberately out of chronological order on the way in.
	got := RenderTranscript([]TranscriptInput{
		toolResult,
		transcriptMessage(1, "user", "list the pets"),
		assistant,
	})

	require.Empty(t, got.Omitted)
	require.Len(t, got.Messages, 3)

	require.Equal(t, "user", got.Messages[0].Role)
	require.Equal(t, "list the pets", got.Messages[0].Content)
	require.Equal(t, 1, got.Messages[0].Index)
	require.Equal(t, transcriptBase.Add(time.Minute).Format(time.RFC3339Nano), got.Messages[0].CreatedAt)
	require.Nil(t, got.Messages[0].SecondsSincePrevious)

	require.Equal(t, "assistant", got.Messages[1].Role)
	require.Equal(t, 2, got.Messages[1].Index)
	require.InDelta(t, 60.0, *got.Messages[1].SecondsSincePrevious, 0)
	require.Len(t, got.Messages[1].ToolCalls, 1)
	require.Equal(t, "call_1", got.Messages[1].ToolCalls[0].ID)
	require.Equal(t, "list_pets", got.Messages[1].ToolCalls[0].Name)
	require.JSONEq(t, `{"limit":3}`, got.Messages[1].ToolCalls[0].Arguments)

	require.Equal(t, "tool", got.Messages[2].Role)
	require.Equal(t, 3, got.Messages[2].Index)
	require.InDelta(t, 60.0, *got.Messages[2].SecondsSincePrevious, 0)
	require.Equal(t, "call_1", got.Messages[2].ToolCallID)
	require.Equal(t, toolURN.String(), got.Messages[2].ToolURN)
	require.Equal(t, "success", got.Messages[2].ToolOutcome)
	require.Equal(t, "returned 3 rows", got.Messages[2].ToolOutcomeNotes)
}

func TestRenderTranscriptRendersObjectToolCallArguments(t *testing.T) {
	t.Parallel()

	assistant := transcriptMessage(1, "assistant", "")
	assistant.ToolCalls = []byte(`[{"id":"call_1","function":{"name":"list_pets","arguments":{"limit":3}}}]`)

	got := RenderTranscript([]TranscriptInput{assistant})

	require.Len(t, got.Messages, 1)
	require.Len(t, got.Messages[0].ToolCalls, 1)
	require.JSONEq(t, `{"limit":3}`, got.Messages[0].ToolCalls[0].Arguments)
}

func TestRenderTranscriptKeepsUndecodableToolCalls(t *testing.T) {
	t.Parallel()

	assistant := transcriptMessage(1, "assistant", "")
	assistant.ToolCalls = []byte(`{"not":"an array"}`)

	got := RenderTranscript([]TranscriptInput{assistant})

	require.Len(t, got.Messages[0].ToolCalls, 1)
	require.JSONEq(t, `{"not":"an array"}`, got.Messages[0].ToolCalls[0].Arguments)
}

func TestRenderTranscriptTruncatesOversizedFields(t *testing.T) {
	t.Parallel()

	msg := transcriptMessage(1, "user", strings.Repeat("é", maxMessageBodyRunes+500))

	got := RenderTranscript([]TranscriptInput{msg})

	require.True(t, got.Messages[0].ContentTruncated)
	// The omission marker is inside the budget, not on top of it.
	require.LessOrEqual(t, utf8.RuneCountInString(got.Messages[0].Content), maxMessageBodyRunes)
	require.True(t, utf8.ValidString(got.Messages[0].Content))
	require.Contains(t, got.Messages[0].Content, "characters truncated")
}

func TestTruncateRunesNeverExceedsRequestedMax(t *testing.T) {
	t.Parallel()

	// Sizes around and far below the marker's own width, where a naive split
	// would emit more than was asked for.
	for _, maxRunes := range []int{0, 1, 5, 30, 31, 40, 100, 8000} {
		input := strings.Repeat("é", maxRunes+1000)

		got, truncated := truncateRunes(input, maxRunes)

		require.True(t, truncated)
		require.LessOrEqual(t, utf8.RuneCountInString(got), maxRunes, "max=%d", maxRunes)
		require.True(t, utf8.ValidString(got))
	}
}

func TestTruncateRunesKeepsInputWithinBudget(t *testing.T) {
	t.Parallel()

	got, truncated := truncateRunes("short", 5)

	require.False(t, truncated)
	require.Equal(t, "short", got)
}

func TestRenderTranscriptDropsOldestWholeMessagesUnderBudget(t *testing.T) {
	t.Parallel()

	// Each message renders at roughly maxMessageBodyRunes, so 30 of them are far
	// past the transcript budget.
	const count = 30
	messages := make([]TranscriptInput, 0, count)
	for i := range count {
		seq := int64(i + 1)
		messages = append(messages, transcriptMessage(seq, "user", strings.Repeat("x", maxMessageBodyRunes)))
	}

	got := RenderTranscript(messages)

	require.NotEmpty(t, got.Omitted)
	dropped := count - len(got.Messages)
	require.Positive(t, dropped)
	require.Equal(t, "["+strconv.Itoa(dropped)+" earlier messages omitted]", got.Omitted)

	// Whole messages only: every kept message still carries its full rendered
	// body, and the newest message survives.
	for _, m := range got.Messages {
		require.Equal(t, strings.Repeat("x", maxMessageBodyRunes), m.Content)
		require.False(t, m.ContentTruncated)
	}

	total := 0
	for _, m := range got.Messages {
		total += renderedSize(m)
	}
	require.LessOrEqual(t, total, maxTranscriptRunes)
}

func TestRenderTranscriptAlwaysKeepsLatestMessage(t *testing.T) {
	t.Parallel()

	// One message whose rendering alone exceeds the transcript budget: per-field
	// truncation bounds it, and it must not be dropped.
	msg := transcriptMessage(1, "user", strings.Repeat("x", maxTranscriptRunes*2))

	got := RenderTranscript([]TranscriptInput{msg})

	require.Len(t, got.Messages, 1)
	require.Empty(t, got.Omitted)
	require.True(t, got.Messages[0].ContentTruncated)
}

func TestRenderTranscriptBoundsHostileNewestMessage(t *testing.T) {
	t.Parallel()

	// Every field at once, all of it control characters so JSON encoding expands
	// each rune six-fold - the shape that defeats a caps-only budget.
	blob := strings.Repeat("\x00", 20000)
	quoted, err := json.Marshal(blob)
	require.NoError(t, err)

	var envelope strings.Builder
	envelope.WriteString(`[`)
	for i := range 50 {
		if i > 0 {
			envelope.WriteString(`,`)
		}
		envelope.WriteString(`{"id":` + string(quoted) + `,"function":{"name":` + string(quoted) + `,"arguments":` + string(quoted) + `}}`)
	}
	envelope.WriteString(`]`)

	msg := transcriptMessage(2, strings.Repeat("r", 50000), blob)
	msg.ToolCalls = []byte(envelope.String())
	msg.ToolCallID = pgtype.Text{String: blob, Valid: true}
	msg.ToolOutcome = pgtype.Text{String: blob, Valid: true}
	msg.ToolOutcomeNotes = pgtype.Text{String: blob, Valid: true}

	got := RenderTranscript([]TranscriptInput{
		transcriptMessage(1, "user", "do the thing"),
		msg,
	})

	// The newest message is retained and bounded, not dropped.
	require.NotEmpty(t, got.Messages)
	latest := got.Messages[len(got.Messages)-1]
	require.LessOrEqual(t, renderedSize(latest), maxTranscriptRunes)

	require.True(t, latest.ContentTruncated)
	require.True(t, latest.ToolOutcomeNotesTruncated)
	require.True(t, latest.ToolCallsTruncated)
	require.NotEmpty(t, latest.ToolCalls, "tool information must not be silently dropped entirely")
	require.True(t, latest.ToolCalls[0].ArgumentsTruncated)

	require.True(t, utf8.ValidString(latest.Role))
	require.True(t, utf8.ValidString(latest.Content))
	require.True(t, utf8.ValidString(latest.ToolCallID))
	require.True(t, utf8.ValidString(latest.ToolOutcome))
	require.True(t, utf8.ValidString(latest.ToolOutcomeNotes))
	for _, c := range latest.ToolCalls {
		require.True(t, utf8.ValidString(c.ID))
		require.True(t, utf8.ValidString(c.Name))
		require.True(t, utf8.ValidString(c.Arguments))
	}

	require.Equal(t, got, RenderTranscript([]TranscriptInput{
		transcriptMessage(1, "user", "do the thing"),
		msg,
	}), "rendering must be deterministic")
}

func TestRenderToolCallsKeepsHeadAndTail(t *testing.T) {
	t.Parallel()

	var envelope strings.Builder
	envelope.WriteString(`[`)
	for i := range 50 {
		if i > 0 {
			envelope.WriteString(`,`)
		}
		fmt.Fprintf(&envelope, `{"id":"call-%02d","function":{"name":"tool-%02d","arguments":"{}"}}`, i, i)
	}
	envelope.WriteString(`]`)

	got, truncated := renderToolCalls([]byte(envelope.String()), defaultCaps)
	require.True(t, truncated)
	require.Len(t, got, maxRenderedToolCalls)

	ids := make([]string, 0, len(got))
	for _, call := range got {
		ids = append(ids, call.ID)
	}
	require.Equal(t, []string{"call-00", "call-01", "call-02", "call-03", "call-46", "call-47", "call-48", "call-49"}, ids)
}

func TestRenderTranscriptIsDeterministic(t *testing.T) {
	t.Parallel()

	messages := make([]TranscriptInput, 0, 40)
	for i := range 40 {
		seq := int64(i + 1)
		m := transcriptMessage(seq, "assistant", strings.Repeat("y", 5000))
		m.ToolCalls = []byte(`[{"id":"call_1","function":{"name":"do_thing","arguments":"{}"}}]`)
		messages = append(messages, m)
	}

	first := RenderTranscript(messages)
	second := RenderTranscript(messages)

	require.Equal(t, first, second)
}

func TestRenderTranscriptDoesNotMutateInput(t *testing.T) {
	t.Parallel()

	messages := []TranscriptInput{
		transcriptMessage(2, "assistant", "second"),
		transcriptMessage(1, "user", "first"),
	}

	RenderTranscript(messages)

	require.Equal(t, "second", messages[0].Content)
}
