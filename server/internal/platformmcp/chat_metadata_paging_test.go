package platformmcp

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
)

// A cursor pins the interval the first page read. A later page issued after
// the clock has moved queries the same absolute bounds and reports them.
func TestListChatsCursorPinsTheWindow(t *testing.T) {
	t.Parallel()

	principal := registrationServicePrincipal()
	reader := &recordingChatReader{rows: chatListRows(3, 7), params: nil, err: nil, countParams: nil, count: 0}
	service, _ := newChatMetadataService(t, reader, allowBudget())
	clock := chatListTestNow
	service.now = func() time.Time { return clock }

	first, err := service.List(t.Context(), principal, ListChatsInput{
		ProjectID: "", ProjectSlug: "", Window: "24h", Risk: "", Source: "", AssistantID: "", UserReference: "", Limit: 2, Cursor: "",
	})
	require.NoError(t, err)
	require.NotEmpty(t, first.NextCursor)

	clock = chatListTestNow.Add(3 * time.Minute)
	second, err := service.List(t.Context(), principal, ListChatsInput{
		ProjectID: "", ProjectSlug: "", Window: "24h", Risk: "", Source: "", AssistantID: "", UserReference: "", Limit: 2, Cursor: first.NextCursor,
	})
	require.NoError(t, err)
	require.Equal(t, reader.params[0].FromTime.Time, reader.params[1].FromTime.Time, "the second page reads the interval the first page read")
	require.Equal(t, reader.params[0].ToTime.Time, reader.params[1].ToTime.Time)
	require.Equal(t, first.Envelope.ResolvedWindow.From, second.Envelope.ResolvedWindow.From)
	require.Equal(t, first.Envelope.ResolvedWindow.To, second.Envelope.ResolvedWindow.To)
	require.Equal(t, DiagnosticWindowLastDay, second.Envelope.ResolvedWindow.Window)
	require.Equal(t, clock.Format(time.RFC3339), second.Envelope.QueriedAt, "queried_at still reports when this page ran")

	// A fresh listing without a cursor follows the clock.
	fresh, err := service.List(t.Context(), principal, ListChatsInput{
		ProjectID: "", ProjectSlug: "", Window: "24h", Risk: "", Source: "", AssistantID: "", UserReference: "", Limit: 2, Cursor: "",
	})
	require.NoError(t, err)
	require.Equal(t, clock, reader.params[2].ToTime.Time)
	require.NotEqual(t, first.Envelope.ResolvedWindow.To, fresh.Envelope.ResolvedWindow.To)

	// The pinned interval is sealed: a sealed cursor whose interval is
	// malformed is refused rather than trusted.
	projects, ok := service.projects.(*findingProjects)
	require.True(t, ok)
	for _, value := range []string{"o:2", "o:2:0:10", "o:2:10:10", "o:2:10:5", "o:2:x:y"} {
		list, err := normalizeChatList(ListChatsInput{ProjectID: "", ProjectSlug: "", Window: "24h", Risk: "", Source: "", AssistantID: "", UserReference: "", Limit: 2, Cursor: ""})
		require.NoError(t, err)
		list.window, err = resolveWindow("24h", clock, chatListWindowSpec)
		require.NoError(t, err)
		list.projectID = projects.project.ID
		forged, err := service.references.EncodeScoped(principal, subjectKindCursor, list.cursorScope(), value, clock)
		require.NoError(t, err)
		_, err = service.List(t.Context(), principal, ListChatsInput{
			ProjectID: "", ProjectSlug: "", Window: "24h", Risk: "", Source: "", AssistantID: "", UserReference: "", Limit: 2, Cursor: forged,
		})
		require.ErrorIs(t, err, ErrSubjectReferenceNotFound, value)
	}
}

// An empty page past the end of a set that shrank reports the count the
// database holds now, never the cursor's offset.
func TestListChatsEmptyPageCountsIndependently(t *testing.T) {
	t.Parallel()

	principal := registrationServicePrincipal()
	reader := &recordingChatReader{rows: chatListRows(3, 3), params: nil, err: nil, countParams: nil, count: 0}
	service, project := newChatMetadataService(t, reader, allowBudget())

	first, err := service.List(t.Context(), principal, ListChatsInput{
		ProjectID: "", ProjectSlug: "", Window: "7d", Risk: ChatRiskWithFindings, Source: "codex", AssistantID: "", UserReference: "", Limit: 2, Cursor: "",
	})
	require.NoError(t, err)
	require.NotEmpty(t, first.NextCursor)
	require.Empty(t, reader.countParams, "a page with rows carries its own total")

	// The set shrank to one chat before the second page was read.
	reader.rows = nil
	reader.count = 1
	second, err := service.List(t.Context(), principal, ListChatsInput{
		ProjectID: "", ProjectSlug: "", Window: "7d", Risk: ChatRiskWithFindings, Source: "codex", AssistantID: "", UserReference: "", Limit: 2, Cursor: first.NextCursor,
	})
	require.NoError(t, err)
	require.Empty(t, second.Chats)
	require.Equal(t, 1, second.TotalMatches)
	require.Len(t, reader.countParams, 1)
	count := reader.countParams[0]
	require.Equal(t, project.ID, count.ProjectID)
	require.Equal(t, "true", count.HasRiskFilter)
	require.Equal(t, []string{"codex"}, count.Sources)
	require.Equal(t, reader.params[1].FromTime.Time, count.FromTime.Time, "the count reads the pinned interval the page read")
	require.Equal(t, reader.params[1].ToTime.Time, count.ToTime.Time)
	require.Empty(t, count.Search)

	// An empty first page needs no count: nothing matched.
	_, err = service.List(t.Context(), principal, ListChatsInput{
		ProjectID: "", ProjectSlug: "", Window: "7d", Risk: "", Source: "", AssistantID: "", UserReference: "", Limit: 2, Cursor: "",
	})
	require.NoError(t, err)
	require.Len(t, reader.countParams, 1)
}

// The query yields one row per chat and picks the thread of the assistant the
// listing was narrowed to, so the projection reports that row's assistant as
// is, and the assistant filter reaches the query unchanged.
func TestListChatsReportsTheRowsAssistant(t *testing.T) {
	t.Parallel()

	principal := registrationServicePrincipal()
	chatID := uuid.New()
	assistantID := uuid.New()
	row := chatListRow(chatID, chatListTestNow.Add(-time.Hour), "user_1", "", "", 1)
	row.AssistantID = uuid.NullUUID{UUID: assistantID, Valid: true}
	row.AssistantName = pgtype.Text{String: "Release", Valid: true}
	reader := &recordingChatReader{rows: []chatrepo.ListChatsRow{row}, params: nil, err: nil, countParams: nil, count: 0}
	service, _ := newChatMetadataService(t, reader, allowBudget())

	output, err := service.List(t.Context(), principal, ListChatsInput{
		ProjectID: "", ProjectSlug: "", Window: "", Risk: "", Source: "", AssistantID: assistantID.String(), UserReference: "", Limit: 0, Cursor: "",
	})
	require.NoError(t, err)
	require.Len(t, output.Chats, 1)
	require.Equal(t, chatID.String(), output.Chats[0].ChatID)
	require.Equal(t, assistantID.String(), output.Chats[0].AssistantID)
	require.Equal(t, "Release", output.Chats[0].AssistantName)
	require.Equal(t, assistantID.String(), reader.params[0].AssistantID, "the filter reaches the query, which picks the matching thread")
	require.Equal(t, 1, output.TotalMatches)
}

// One row per chat is the query's guarantee and the unit every count relies
// on. A page that breaks it is refused rather than served with counts that no
// longer mean chats.
func TestListChatsRefusesADuplicateChatRow(t *testing.T) {
	t.Parallel()

	chatID := uuid.New()
	first := chatListRow(chatID, chatListTestNow.Add(-time.Hour), "user_1", "", "", 2)
	second := first
	second.AssistantID = uuid.NullUUID{UUID: uuid.New(), Valid: true}
	reader := &recordingChatReader{rows: []chatrepo.ListChatsRow{first, second}, params: nil, err: nil, countParams: nil, count: 0}
	service, _ := newChatMetadataService(t, reader, allowBudget())

	_, err := service.List(t.Context(), registrationServicePrincipal(), ListChatsInput{
		ProjectID: "", ProjectSlug: "", Window: "", Risk: "", Source: "", AssistantID: "", UserReference: "", Limit: 0, Cursor: "",
	})
	require.ErrorIs(t, err, errChatListDuplicateRow)
}
