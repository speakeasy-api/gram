package transcriptstore

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
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
	db := &countingDB{DBTX: f.db, queries: 0}
	messages, err := transcripts.ListLatestGenerationChatMessages(t.Context(), db, f.project, f.chat)
	require.NoError(t, err)
	require.Len(t, messages, 2)
	for _, m := range messages {
		require.Equal(t, "sensitive example", m.Content)
	}
	require.Equal(t, 2, db.queries, "a cold read adds the one organization lookup")

	// Warm, the rows and their shadows come back in one query.
	db.queries = 0
	_, err = transcripts.ListLatestGenerationChatMessages(t.Context(), db, f.project, f.chat)
	require.NoError(t, err)
	require.Equal(t, 1, db.queries)

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

// countingDB counts the statements a reader sends. Not safe for concurrent
// use; each test owns its own.
type countingDB struct {
	DBTX
	queries int
}

func (c *countingDB) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	c.queries++
	return c.DBTX.Exec(ctx, sql, args...) //nolint:glint,wrapcheck // notestingrawsql: forwards a generated query, unchanged, so the test can count it.
}

func (c *countingDB) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	c.queries++
	return c.DBTX.Query(ctx, sql, args...) //nolint:glint,wrapcheck // notestingrawsql: forwards a generated query, unchanged, so the test can count it.
}

func (c *countingDB) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	c.queries++
	return c.DBTX.QueryRow(ctx, sql, args...) //nolint:glint // notestingrawsql: forwards a generated query, unchanged, so the test can count it.
}

func TestEvaluationReadsServeShadowsAndBlankOverBudget(t *testing.T) {
	t.Parallel()
	f := setup(t, true)
	earlier, target := uuid.New(), uuid.New()
	f.add(t, earlier)
	f.add(t, target)
	f.capture(t, earlier, target)
	transcripts := NewTranscripts(f.store, f.project)

	messages, err := transcripts.LoadEvaluationMessages(t.Context(), f.db, LoadEvaluationMessagesParams{OpenShadows: false, ProjectID: f.project, OrganizationID: f.org, MessageIds: []uuid.UUID{target}})
	require.NoError(t, err)
	require.Len(t, messages, 1)
	require.Equal(t, "sensitive example", messages[0].Content)

	history := func(budget int64) ([]LoadEvaluationHistoryRow, error) {
		return transcripts.LoadEvaluationHistory(t.Context(), f.db, LoadEvaluationHistoryParams{
			OpenShadows: false, ProjectID: f.project, OrganizationID: f.org, ChatID: f.chat,
			Generation: messages[0].Generation, TargetID: target, TargetCreatedAt: messages[0].CreatedAt,
			TargetSeq: messages[0].Seq, HistoryLimit: 10, ContentBudget: budget,
		})
	}
	rows, err := history(1 << 20)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, earlier, rows[0].ID)
	require.False(t, rows[0].ContentTooLarge)
	require.Equal(t, "sensitive example", rows[0].Content)
	require.NotEmpty(t, rows[0].ToolCalls)

	// Over budget SQL blanks the body. The opened shadow is blanked the same
	// way, so it still matches instead of reading as stale.
	rows, err = history(1)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.True(t, rows[0].ContentTooLarge)
	require.Empty(t, rows[0].Content)
	require.Nil(t, rows[0].ContentRaw)
	require.Nil(t, rows[0].ToolCalls)

	// Plaintext edited behind a shadow fails both reads.
	_, err = fixturerepo.New(f.db).ChangeContent(t.Context(), earlier, project(f.project))
	require.NoError(t, err)
	_, err = history(1 << 20)
	require.ErrorIs(t, err, ErrStale)
	_, err = transcripts.LoadEvaluationMessages(t.Context(), f.db, LoadEvaluationMessagesParams{OpenShadows: false, ProjectID: f.project, OrganizationID: f.org, MessageIds: []uuid.UUID{earlier}})
	require.ErrorIs(t, err, ErrStale)
}

func TestPlatformReadBlanksJSONOverTheCapLikeSQL(t *testing.T) {
	t.Parallel()
	f := setup(t, true)
	id := uuid.New()
	f.add(t, id)
	// jsonb text adds a space after every ":" and ",", so this array is over
	// the cap as jsonb text and under it as the compact JSON a shadow holds.
	calls := "[" + strings.TrimSuffix(strings.Repeat(`{"a":1},`, 14000), ",") + "]"
	require.Less(t, len(calls), 131072)
	_, err := fixturerepo.New(f.db).SetToolCalls(t.Context(), []byte(calls), id, project(f.project))
	require.NoError(t, err)
	f.capture(t, id)

	rows, err := NewTranscripts(f.store, f.project).ReadPlatformChatMessages(t.Context(), f.db, ReadPlatformChatMessagesParams{
		ProjectID: f.project, OpenShadows: false, OrganizationID: f.org,
		MessageID: uuid.NullUUID{UUID: id, Valid: true}, ChatID: f.chat, ContextSize: 1, FollowingLimit: 2,
	})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.True(t, rows[0].Oversized)
	require.True(t, rows[0].ToolCallsOversized)
	require.Nil(t, rows[0].ToolCalls)
	require.Equal(t, "sensitive example", rows[0].Content)
}

func TestLeftRunesMatchesPostgresLeft(t *testing.T) {
	t.Parallel()
	require.Equal(t, "héll", leftRunes("héllo", 4))
	require.Equal(t, "héllo", leftRunes("héllo", 10))
	require.Empty(t, leftRunes("héllo", 0))
	require.Equal(t, strings.Repeat("界", 3), leftRunes(strings.Repeat("界", 5), 3))
}

func TestPlatformShapeBlanksFlaggedFields(t *testing.T) {
	t.Parallel()
	m := transcriptcrypto.Message{Content: "body", ContentRaw: []byte(`"raw"`), ToolCalls: []byte(`[]`)}
	platformShape(platformOversized{content: true, toolCalls: false, contentRaw: true})(&m)
	require.Empty(t, m.Content)
	require.Nil(t, m.ContentRaw)
	require.Equal(t, []byte(`[]`), []byte(m.ToolCalls))

	m = transcriptcrypto.Message{Content: "content", ContentRaw: []byte(`"raw"`), ToolCalls: []byte(`[{"id":"a"}]`)}
	platformShape(platformOversized{content: false, toolCalls: true, contentRaw: false})(&m)
	require.Equal(t, "content", m.Content)
	require.Equal(t, []byte(`"raw"`), []byte(m.ContentRaw))
	require.Nil(t, m.ToolCalls)
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
	require.NoError(t, json.Unmarshal([]byte(judgeToolCallsOf([]byte("["+strings.Join(calls, ",")+"]"))), &out))
	require.Len(t, out, judgeToolCalls)
	require.Len(t, out[0].Function.Name, judgeToolNameRunes)
	require.Len(t, out[0].Function.Arguments, judgeArgumentsRunes)
}
