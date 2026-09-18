package anthropicinference

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func textMessage(role, text string) Message {
	return Message{Role: role, Content: json.RawMessage(fmt.Sprintf(`[{"type":"text","text":%q}]`, text))}
}

// chainIdentities hashes a stored conversation the way Save records it.
func chainIdentities(messages ...Message) []messageIdentity {
	stored := make([]messageIdentity, 0, len(messages))
	var prev []byte
	for _, msg := range messages {
		content := contentHash(msg)
		prev = chainHash(prev, content)
		stored = append(stored, messageIdentity{content: content, chain: prev})
	}
	return stored
}

func frameHashes(messages ...Message) [][]byte {
	hashes := make([][]byte, 0, len(messages))
	for _, msg := range messages {
		hashes = append(hashes, contentHash(msg))
	}
	return hashes
}

func TestAlignTranscriptContinuesStoredHistory(t *testing.T) {
	t.Parallel()
	prompt, reply, next := textMessage("user", "EXAMPLE prompt"), textMessage("assistant", "EXAMPLE reply"), textMessage("user", "EXAMPLE next")
	stored := chainIdentities(prompt, reply)

	start, prev, ok := alignTranscript(stored, frameHashes(prompt, reply, next))
	require.True(t, ok)
	require.Equal(t, 2, start)
	require.Equal(t, stored[1].chain, prev)

	// A redelivery of stored history adds nothing.
	start, _, ok = alignTranscript(stored, frameHashes(prompt, reply))
	require.True(t, ok)
	require.Equal(t, 2, start)
}

func TestAlignTranscriptSkipsCompactedHistory(t *testing.T) {
	t.Parallel()
	// Rolling compaction replaces early turns with a summary and keeps the
	// recent ones verbatim. Only the message after the kept history is new.
	var history []Message
	for index := range 20 {
		history = append(history, textMessage("user", fmt.Sprintf("turn %d", index)))
	}
	stored := chainIdentities(history...)
	summary := textMessage("user", "EXAMPLE summary of earlier turns")
	next := textMessage("user", "turn 20")
	frame := append([]Message{summary}, history[15:]...)
	frame = append(frame, next)

	start, prev, ok := alignTranscript(stored, frameHashes(frame...))
	require.True(t, ok)
	require.Equal(t, len(frame)-1, start)
	require.Equal(t, stored[len(stored)-1].chain, prev)
}

func TestAlignTranscriptDistinguishesRepeatedMessages(t *testing.T) {
	t.Parallel()
	first, again := textMessage("user", "start"), textMessage("user", "continue")
	stored := chainIdentities(first, again, again)

	start, _, ok := alignTranscript(stored, frameHashes(first, again, again, again))
	require.True(t, ok)
	require.Equal(t, 3, start)

	// Identical messages at different positions carry different identities.
	require.NotEqual(t, stored[1].chain, stored[2].chain)
}

func TestAlignTranscriptToleratesRewrittenNewestMessage(t *testing.T) {
	t.Parallel()
	prompt, reply, result := textMessage("user", "EXAMPLE prompt"), textMessage("assistant", "EXAMPLE reply"), textMessage("user", "tool output")
	stored := chainIdentities(prompt, reply, result)
	truncated := textMessage("user", "tool output [truncated]")
	next := textMessage("assistant", "EXAMPLE follow-up")

	start, prev, ok := alignTranscript(stored, frameHashes(prompt, reply, truncated, next))
	require.True(t, ok)
	require.Equal(t, 2, start)
	require.Equal(t, stored[1].chain, prev)
}

func TestAlignTranscriptReportsUnrelatedFrames(t *testing.T) {
	t.Parallel()
	stored := chainIdentities(textMessage("user", "EXAMPLE prompt"))
	_, _, ok := alignTranscript(stored, frameHashes(textMessage("user", "something else")))
	require.False(t, ok)
	_, _, ok = alignTranscript(nil, frameHashes(textMessage("user", "EXAMPLE prompt")))
	require.False(t, ok)
	// Rows stored before content hashing cannot anchor an alignment.
	_, _, ok = alignTranscript([]messageIdentity{{content: nil, chain: nil}}, frameHashes(textMessage("user", "EXAMPLE prompt")))
	require.False(t, ok)
}

func TestContentHashIgnoresEncodingWhitespace(t *testing.T) {
	t.Parallel()
	compact := Message{Role: "user", Content: json.RawMessage(`[{"type":"text","text":"EXAMPLE"}]`)}
	spaced := Message{Role: "user", Content: json.RawMessage("[ {\"type\": \"text\",\n  \"text\": \"EXAMPLE\"} ]")}
	require.Equal(t, contentHash(compact), contentHash(spaced))
	require.NotEqual(t, contentHash(compact), contentHash(Message{Role: "assistant", Content: compact.Content}))
}

func TestCurrentTurnStartsAfterLastAssistantMessage(t *testing.T) {
	t.Parallel()
	require.Equal(t, 0, currentTurnStart(nil))
	require.Equal(t, 0, currentTurnStart([]Message{textMessage("user", "a")}))
	require.Equal(t, 2, currentTurnStart([]Message{textMessage("user", "a"), textMessage("assistant", "b"), textMessage("user", "c"), textMessage("user", "d")}))
	require.Equal(t, 3, currentTurnStart([]Message{textMessage("user", "a"), textMessage("assistant", "b"), textMessage("assistant", "c")}))
}
