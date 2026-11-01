package activities_test

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/background/activities"
	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// addIdleChat seeds another chat in the same project, optionally with one user
// message, and backdates it so the sweep sees it as idle.
func (c titleTestChat) addIdleChat(t *testing.T, content string) uuid.UUID {
	t.Helper()

	ctx := t.Context()
	chatID, err := c.repo.UpsertChat(ctx, chatrepo.UpsertChatParams{
		ID: uuid.New(), ProjectID: c.projectID, OrganizationID: c.orgID, Title: pgtype.Text{},
	})
	require.NoError(t, err)
	if content != "" {
		_, err = c.repo.CreateChatMessageReturningID(ctx, chatrepo.CreateChatMessageReturningIDParams{
			ChatID: chatID, ProjectID: uuid.NullUUID{UUID: c.projectID, Valid: true}, Role: "user", Content: content,
			ToolCalls: nil, ToolCallID: pgtype.Text{},
		})
		require.NoError(t, err)
	}
	c.backdate(t, chatID)
	return chatID
}

func (c titleTestChat) backdate(t *testing.T, chatID uuid.UUID) {
	t.Helper()
	_, err := c.conn.Exec(t.Context(), `UPDATE chats SET updated_at = now() - interval '2 hours' WHERE id = $1`, chatID)
	require.NoError(t, err)
}

func (c titleTestChat) chat(t *testing.T, chatID uuid.UUID) chatrepo.GetChatRow {
	t.Helper()
	row, err := c.repo.GetChat(t.Context(), chatrepo.GetChatParams{ID: chatID, ProjectID: c.projectID})
	require.NoError(t, err)
	return row
}

func sweepFlags(orgID string, enabled bool) *feature.InMemory {
	flags := &feature.InMemory{}
	flags.SetFlag(feature.FlagTeamMemory, orgID, enabled)
	return flags
}

func TestSummarizeIdleChats_SummarizesEligibleIdleChats(t *testing.T) {
	t.Parallel()

	tc := newTitleTestChat(t, "summarizeidle_eligible", "")
	token := "ghp_" + strings.Repeat("aB3dE5fG7h", 3) + "123456"

	idle := tc.addIdleChat(t, "We picked the durable queue for retries; token "+token)
	empty := tc.addIdleChat(t, "")
	excluded := tc.addIdleChat(t, "This one opted out of memory")
	_, err := tc.conn.Exec(t.Context(), `UPDATE chats SET memory_excluded_at = now(), updated_at = now() - interval '2 hours' WHERE id = $1`, excluded)
	require.NoError(t, err)
	// The seeded chat from newTitleTestChat stays fresh, so it is not idle yet.
	tc.addMessage(t, "user", "Still talking")

	stub := &titleCompletionStub{title: "The team chose the durable queue for retries."}
	act := activities.NewSummarizeIdleChats(testenv.NewLogger(t), tc.conn, sweepFlags(tc.orgID, true), stub)

	result, err := act.Do(t.Context(), activities.SummarizeIdleChatsArgs{})
	require.NoError(t, err)
	require.Equal(t, 1, result.Organizations)
	require.Equal(t, 1, result.Summarized)
	require.Equal(t, 1, result.Empty)
	require.Equal(t, 0, result.Failed)
	require.Equal(t, int32(1), stub.calls.Load())
	require.NotContains(t, *stub.prompt.Load(), token)

	summarized := tc.chat(t, idle)
	require.True(t, summarized.SummaryRedacted)
	require.Equal(t, "The team chose the durable queue for retries.", summarized.Summary.String)

	marked := tc.chat(t, empty)
	require.True(t, marked.SummaryGeneratedAt.Valid)
	require.False(t, marked.Summary.Valid)

	require.False(t, tc.chat(t, excluded).SummaryGeneratedAt.Valid)
	require.False(t, tc.get(t).SummaryGeneratedAt.Valid)

	// Nothing changed since, so a second run makes no model calls.
	again, err := act.Do(t.Context(), activities.SummarizeIdleChatsArgs{})
	require.NoError(t, err)
	require.Equal(t, 0, again.Summarized+again.Empty)
	require.Equal(t, int32(1), stub.calls.Load())

	// New activity after the summary makes the chat due again once idle.
	_, err = tc.repo.CreateChatMessageReturningID(t.Context(), chatrepo.CreateChatMessageReturningIDParams{
		ChatID: idle, ProjectID: uuid.NullUUID{UUID: tc.projectID, Valid: true}, Role: "user", Content: "Actually, switch to the stream",
		ToolCalls: nil, ToolCallID: pgtype.Text{},
	})
	require.NoError(t, err)
	// Replay the timeline: summary three hours ago, last message one hour ago.
	_, err = tc.conn.Exec(t.Context(), `UPDATE chats SET summary_generated_at = now() - interval '3 hours', updated_at = now() - interval '1 hour' WHERE id = $1`, idle)
	require.NoError(t, err)
	third, err := act.Do(t.Context(), activities.SummarizeIdleChatsArgs{})
	require.NoError(t, err)
	require.Equal(t, 1, third.Summarized)
}

func TestSummarizeIdleChats_SkipsOrganizationsWithFlagOff(t *testing.T) {
	t.Parallel()

	tc := newTitleTestChat(t, "summarizeidle_flagoff", "")
	idle := tc.addIdleChat(t, "Decided to keep the old auth flow")

	stub := &titleCompletionStub{title: "unused"}
	act := activities.NewSummarizeIdleChats(testenv.NewLogger(t), tc.conn, sweepFlags(tc.orgID, false), stub)

	result, err := act.Do(t.Context(), activities.SummarizeIdleChatsArgs{})
	require.NoError(t, err)
	require.Equal(t, 0, result.Organizations)
	require.Equal(t, int32(0), stub.calls.Load())
	require.False(t, tc.chat(t, idle).SummaryGeneratedAt.Valid)
}
