package transcriptstore

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/chat/transcriptcrypto"
	"github.com/speakeasy-api/gram/server/internal/chat/transcriptstore/fixturerepo"
)

func TestJoinedReadsServeShadowsInOneQuery(t *testing.T) {
	t.Parallel()
	f := setup(t, true)
	shadowed, unshadowed := uuid.New(), uuid.New()
	f.add(t, shadowed)
	f.add(t, unshadowed)
	f.capture(t, shadowed)

	transcripts := NewTranscripts(f.store, f.project)
	messages, err := transcripts.ListLatestGenerationChatMessages(t.Context(), f.db, f.project, f.chat)
	require.NoError(t, err)
	require.Len(t, messages, 2)
	for _, m := range messages {
		require.Equal(t, "sensitive example", m.Content)
	}

	// A project off the list reads plaintext and joins no shadow.
	plain, err := NewTranscripts(f.store, uuid.New()).ListLatestGenerationChatMessages(t.Context(), f.db, f.project, f.chat)
	require.NoError(t, err)
	require.Len(t, plain, 2)

	// Plaintext edited behind the shadow fails the read instead of serving it.
	_, err = fixturerepo.New(f.db).ChangeContent(t.Context(), shadowed, project(f.project))
	require.NoError(t, err)
	_, err = transcripts.ListLatestGenerationChatMessages(t.Context(), f.db, f.project, f.chat)
	require.ErrorIs(t, err, ErrStale)

	// The same read for a project off the list still serves the edited plaintext.
	plain, err = NewTranscripts(f.store, uuid.New()).ListLatestGenerationChatMessages(t.Context(), f.db, f.project, f.chat)
	require.NoError(t, err)
	contents := []string{plain[0].Content, plain[1].Content}
	require.Contains(t, contents, "changed")
}

func TestJoinedReadRefusesShadowOfAnotherMessage(t *testing.T) {
	t.Parallel()
	f := setup(t, true)
	first, second := uuid.New(), uuid.New()
	f.add(t, first)
	f.add(t, second)
	f.capture(t, first, second)

	// Swap one message's ciphertext for another's. The source hash still
	// matches, but the ciphertext is bound to the other message's identity.
	var other string
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT ciphertext FROM chat_message_transcripts WHERE message_id = $1`, second).Scan(&other)) //nolint:glint // notestingrawsql: reads a fixture ciphertext to tamper with it.
	_, err := fixturerepo.New(f.db).ReplaceCiphertext(t.Context(), other, first, f.project, f.org)
	require.NoError(t, err)

	_, err = NewTranscripts(f.store, f.project).ListLatestGenerationChatMessages(t.Context(), f.db, f.project, f.chat)
	require.Error(t, err)
}

func TestLeftRunesMatchesPostgresLeft(t *testing.T) {
	t.Parallel()
	require.Equal(t, "héll", leftRunes("héllo", 4))
	require.Equal(t, "héllo", leftRunes("héllo", 10))
	require.Equal(t, "", leftRunes("héllo", 0))
	require.Equal(t, strings.Repeat("界", 3), leftRunes(strings.Repeat("界", 5), 3))
}

func TestPlatformShapeBlanksOversizedFields(t *testing.T) {
	t.Parallel()
	big := strings.Repeat("x", platformBodyBytes+1)
	m := transcriptcrypto.Message{Content: big, ContentRaw: []byte(`"` + big + `"`), ToolCalls: []byte(`[]`)}
	platformShape(&m)
	require.Empty(t, m.Content)
	require.Nil(t, m.ContentRaw)
	require.Equal(t, []byte(`[]`), []byte(m.ToolCalls))
}

func TestJudgeToolCallsOf(t *testing.T) {
	t.Parallel()
	require.Equal(t, "[]", judgeToolCallsOf(nil))
	require.Equal(t, "[]", judgeToolCallsOf([]byte(`not json`)))
	require.JSONEq(t, `[{"function":{"name":"lookup","arguments":"{\"q\":1}"}}]`,
		judgeToolCallsOf([]byte(`[{"id":"a","function":{"name":"lookup","arguments":"{\"q\":1}"}}]`)))
	require.JSONEq(t, `[{"function":{"name":"lookup","arguments":null}}]`,
		judgeToolCallsOf([]byte(`"[{\"function\":{\"name\":\"lookup\"}}]"`)), "double-encoded arrays are read")

	calls := make([]string, 12)
	for i := range calls {
		calls[i] = `{"function":{"name":"` + strings.Repeat("n", 300) + `","arguments":"` + strings.Repeat("a", 5000) + `"}}`
	}
	var out []struct {
		Function struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		} `json:"function"`
	}
	require.NoError(t, jsonUnmarshal(judgeToolCallsOf([]byte("["+strings.Join(calls, ",")+"]")), &out))
	require.Len(t, out, judgeToolCalls)
	require.Len(t, out[0].Function.Name, judgeToolNameRunes)
	require.Len(t, out[0].Function.Arguments, judgeArgumentsRunes)
}

func jsonUnmarshal(s string, v any) error { return json.Unmarshal([]byte(s), v) }
