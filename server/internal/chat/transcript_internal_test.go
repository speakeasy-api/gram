package chat

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/chat/repo"
)

func TestEntryKeysAcceptOnlyModelMintedToolIDs(t *testing.T) {
	t.Parallel()
	toolCalls := []byte(`[
		{"id":"toolu_01A","type":"function","function":{"name":"lookup","arguments":"{}"}},
		{"id":"cursor_synth_0123abcd","type":"function","function":{"name":"lookup","arguments":"{}"}},
		{"id":"12|session|Bash","type":"function","function":{"name":"Bash","arguments":"{}"}},
		{"id":"anthropic-inference:ab/tool:0","type":"function","function":{"name":"lookup","arguments":"{}"}},
		{"id":"Bash","type":"function","function":{"name":"Bash","arguments":"{}"}}
	]`)

	row := transcriptRow{sessionID: "session-1", toolCalls: toolCalls, toolCallID: "toolu_01A"}
	require.Equal(t, []string{"tool-call:toolu_01A", "tool-result:toolu_01A"}, entryKeys(row))

	require.Nil(t, entryKeys(transcriptRow{sessionID: "session-1", toolCallID: "cursor_synth_0123abcd"}))
	require.Nil(t, entryKeys(transcriptRow{sessionID: "session-1", toolCalls: []byte(`not json`)}))

	row.sessionID = ""
	require.Nil(t, entryKeys(row), "rows outside an agent session own no keys")
}

func TestBodyKeyNormalisesSurroundingWhitespaceOnly(t *testing.T) {
	t.Parallel()
	require.Equal(t, bodyKey("assistant", "Here is the answer."), bodyKey("assistant", "  Here is the answer.\n"))
	require.NotEqual(t, bodyKey("assistant", "Here is the answer."), bodyKey("user", "Here is the answer."))
	require.NotEqual(t, bodyKey("assistant", "Here is the answer."), bodyKey("assistant", "Here is  the answer."))
}

func TestSameUser(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name                                     string
		storedUserID, storedEmail, userID, email string
		want                                     bool
	}{
		{name: "same user id", storedUserID: "u1", storedEmail: "a@example.test", userID: "u1", email: "b@example.test", want: true},
		{name: "different user ids win over equal emails", storedUserID: "u1", storedEmail: "a@example.test", userID: "u2", email: "a@example.test", want: false},
		{name: "email fallback is case-insensitive", storedUserID: "", storedEmail: "A@Example.test", userID: "u1", email: "a@example.test", want: true},
		{name: "no identity on either side", storedUserID: "", storedEmail: "", userID: "", email: "", want: false},
		{name: "stored row has no identity", storedUserID: "", storedEmail: "", userID: "u1", email: "a@example.test", want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, sameUser(tc.storedUserID, tc.storedEmail, tc.userID, tc.email))
		})
	}
}

func TestPrimaryWritesDropsSecondaries(t *testing.T) {
	t.Parallel()
	first, second, third := uuid.New(), uuid.New(), uuid.New()
	writes := []MessageWrite{
		{Params: repo.CreateChatMessageParams{ID: first}},
		{Params: repo.CreateChatMessageParams{ID: second}},
		{Params: repo.CreateChatMessageParams{ID: third}},
	}

	require.Equal(t, writes, primaryWrites(writes, nil))

	kept := primaryWrites(writes, map[uuid.UUID]uuid.UUID{second: first})
	require.Len(t, kept, 2)
	require.Equal(t, first, kept[0].Params.ID)
	require.Equal(t, third, kept[1].Params.ID)
}
