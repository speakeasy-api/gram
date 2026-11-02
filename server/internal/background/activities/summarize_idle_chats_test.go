package activities_test

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/background/activities"
	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

// addIdleChat seeds another chat in the same project, optionally with one
// message, and backdates it so the sweep sees it as idle.
func (c titleTestChat) addIdleChat(t *testing.T, role, content string) uuid.UUID {
	t.Helper()

	ctx := t.Context()
	chatID, err := c.repo.UpsertChat(ctx, chatrepo.UpsertChatParams{
		ID: uuid.New(), ProjectID: c.projectID, OrganizationID: c.orgID, Title: pgtype.Text{},
	})
	require.NoError(t, err)
	if content != "" {
		c.addChatMessage(t, chatID, role, content)
	}
	c.setSweepTimes(t, chatID, 2*time.Hour, 2*time.Hour, 0, false)
	return chatID
}

func (c titleTestChat) addChatMessage(t *testing.T, chatID uuid.UUID, role, content string) {
	t.Helper()
	_, err := c.repo.CreateChatMessageReturningID(t.Context(), chatrepo.CreateChatMessageReturningIDParams{
		ChatID: chatID, ProjectID: uuid.NullUUID{UUID: c.projectID, Valid: true}, Role: role, Content: content,
		ToolCalls: nil, ToolCallID: pgtype.Text{},
	})
	require.NoError(t, err)
}

// setSweepTimes backdates a chat's updated_at by updatedAgo and its messages by
// messagesAgo and, when given, sets summary_generated_at to summaryAgo before
// now and memory_excluded_at to now.
func (c titleTestChat) setSweepTimes(t *testing.T, chatID uuid.UUID, updatedAgo, messagesAgo, summaryAgo time.Duration, excluded bool) {
	t.Helper()
	now := time.Now()
	stamp := func(at time.Time, valid bool) pgtype.Timestamptz {
		return pgtype.Timestamptz{Time: at, InfinityModifier: pgtype.Finite, Valid: valid}
	}
	fixtures := testrepo.New(c.conn)
	err := fixtures.SetChatMemorySweepFixture(t.Context(), testrepo.SetChatMemorySweepFixtureParams{
		UpdatedAt:          stamp(now.Add(-updatedAgo), true),
		SummaryGeneratedAt: stamp(now.Add(-summaryAgo), summaryAgo > 0),
		MemoryExcludedAt:   stamp(now, excluded),
		ID:                 chatID,
	})
	require.NoError(t, err)
	err = fixtures.SetChatMessagesCreatedAtFixture(t.Context(), testrepo.SetChatMessagesCreatedAtFixtureParams{
		CreatedAt: stamp(now.Add(-messagesAgo), true),
		ChatID:    chatID,
	})
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

	idle := tc.addIdleChat(t, "user", "We picked the durable queue for retries; token "+token)
	// Only tool output, no prose: summarized as empty so it is not retried.
	empty := tc.addIdleChat(t, "tool", "{\"status\":\"ok\"}")
	// No messages at all: nothing to sweep.
	blank := tc.addIdleChat(t, "", "")
	excluded := tc.addIdleChat(t, "user", "This one opted out of memory")
	tc.setSweepTimes(t, excluded, 2*time.Hour, 2*time.Hour, 0, true)
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

	require.False(t, tc.chat(t, blank).SummaryGeneratedAt.Valid)
	require.False(t, tc.chat(t, excluded).SummaryGeneratedAt.Valid)
	require.False(t, tc.get(t).SummaryGeneratedAt.Valid)

	// Nothing changed since, so a second run makes no model calls.
	again, err := act.Do(t.Context(), activities.SummarizeIdleChatsArgs{})
	require.NoError(t, err)
	require.Equal(t, 0, again.Summarized+again.Empty)
	require.Equal(t, int32(1), stub.calls.Load())

	// A new message makes the chat due again once idle, even when the message
	// did not bump chats.updated_at, as on the gateway path. Replay the
	// timeline: chat row touched five hours ago, summary three hours ago, last
	// message one hour ago.
	tc.addChatMessage(t, idle, "user", "Actually, switch to the stream")
	tc.setSweepTimes(t, idle, 5*time.Hour, time.Hour, 3*time.Hour, false)
	third, err := act.Do(t.Context(), activities.SummarizeIdleChatsArgs{})
	require.NoError(t, err)
	require.Equal(t, 1, third.Summarized)

	// A message in the last few minutes means the chat is still active.
	tc.addChatMessage(t, idle, "user", "One more thing")
	tc.setSweepTimes(t, idle, 5*time.Hour, time.Minute, 3*time.Hour, false)
	fourth, err := act.Do(t.Context(), activities.SummarizeIdleChatsArgs{})
	require.NoError(t, err)
	require.Equal(t, 0, fourth.Summarized)
}

func TestSummarizeIdleChats_SkipsOrganizationsWithFlagOff(t *testing.T) {
	t.Parallel()

	tc := newTitleTestChat(t, "summarizeidle_flagoff", "")
	idle := tc.addIdleChat(t, "user", "Decided to keep the old auth flow")

	stub := &titleCompletionStub{title: "unused"}
	act := activities.NewSummarizeIdleChats(testenv.NewLogger(t), tc.conn, sweepFlags(tc.orgID, false), stub)

	result, err := act.Do(t.Context(), activities.SummarizeIdleChatsArgs{})
	require.NoError(t, err)
	require.Equal(t, 0, result.Organizations)
	require.Equal(t, int32(0), stub.calls.Load())
	require.False(t, tc.chat(t, idle).SummaryGeneratedAt.Valid)
}
