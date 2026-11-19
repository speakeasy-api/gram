package hooks

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
)

// A Cowork reply event whose text is only NUL bytes and whitespace stores no
// row, and a real reply is stored without them.
func TestCoworkEventRow_ReplyTextIsCleaned(t *testing.T) {
	t.Parallel()

	surface := surfaceCowork
	reply := func(text string) *gen.IngestPayload {
		payload := canonicalIngestPayload("claude", "assistant.responded", "sess-1")
		payload.Session.Surface = &surface
		payload.Data = &gen.HookIngestData{Message: &gen.HookMessageData{Text: &text}}
		return payload
	}

	_, ok := coworkEventRow(reply("\x00 \x00"), time.Now())
	require.False(t, ok, "a reply that is blank once NUL bytes are removed stores nothing")

	row, ok := coworkEventRow(reply("Two\x00 files."), time.Now())
	require.True(t, ok)
	require.Equal(t, "assistant", row.role)
	require.Equal(t, "Two files.", row.content)
	require.Contains(t, row.externalID, claudeReplyKeyPrefix)
}
