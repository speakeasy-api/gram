package activities_test

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
)

// A chat whose title was set by a human must be skipped by auto title
// generation — its title stays put and no LLM call is made.
func TestGenerateChatTitle_SkipsManuallyTitledChat(t *testing.T) {
	t.Parallel()

	tc := newTitleTestChat(t, "generatetitle_skip", "")
	require.NoError(t, tc.repo.RenameChat(t.Context(), chatrepo.RenameChatParams{
		Title:            pgtype.Text{String: "Human Picked", Valid: true},
		TitleManuallySet: true,
		ID:               tc.chatID,
		ProjectID:        tc.projectID,
	}))

	stub := &titleCompletionStub{title: "Auto Generated"}
	tc.run(t, stub)

	row := tc.get(t)
	require.Equal(t, "Human Picked", row.Title.String)
	require.True(t, row.TitleManuallySet)
	require.Zero(t, stub.calls.Load())
}

// Race guard: a manual rename can land between the activity reading the chat
// and writing the generated title. UpdateChatTitle is guarded on
// title_manually_set, so the racing auto-title write must no-op and leave the
// human's title in place.
func TestGenerateChatTitle_WriteSkipsManuallyTitledChat(t *testing.T) {
	t.Parallel()

	tc := newTitleTestChat(t, "generatetitle_writeguard", "")
	require.NoError(t, tc.repo.RenameChat(t.Context(), chatrepo.RenameChatParams{
		Title:            pgtype.Text{String: "Human Picked", Valid: true},
		TitleManuallySet: true,
		ID:               tc.chatID,
		ProjectID:        tc.projectID,
	}))

	// The activity's generated-title write races in afterwards — and must no-op.
	require.NoError(t, tc.repo.UpdateChatTitle(t.Context(), chatrepo.UpdateChatTitleParams{
		ID:        tc.chatID,
		ProjectID: tc.projectID,
		Title:     pgtype.Text{String: "Auto Generated", Valid: true},
	}))

	row := tc.get(t)
	require.Equal(t, "Human Picked", row.Title.String)
	require.True(t, row.TitleManuallySet)
}
