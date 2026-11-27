package transcriptstore

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
)

// A NULL text column's String is not part of its value, so leftover text in
// it must not make an update look like a change and reseal the message.
func TestSameRowIgnoresTheStringOfANullText(t *testing.T) {
	t.Parallel()
	before := chatrepo.ChatMessage{ToolOutcome: pgtype.Text{String: "", Valid: false}}
	after := before
	after.ToolOutcome = pgtype.Text{String: "leftover", Valid: false}
	require.True(t, sameRow(before, after))
	after.ToolOutcome = pgtype.Text{String: "leftover", Valid: true}
	require.False(t, sameRow(before, after))
	after.ToolOutcome = pgtype.Text{String: "", Valid: true}
	require.False(t, sameRow(before, after), "an empty string is not NULL")
}

// JSON columns compare by value, so a reformatted document is unchanged.
func TestSameRowComparesJSONByValue(t *testing.T) {
	t.Parallel()
	before := chatrepo.ChatMessage{ToolCalls: []byte(`{"a":1,"b":2}`)}
	after := before
	after.ToolCalls = []byte(`{ "b": 2, "a": 1 }`)
	require.True(t, sameRow(before, after))
	after.ToolCalls = []byte(`{"a":1,"b":3}`)
	require.False(t, sameRow(before, after))
}
