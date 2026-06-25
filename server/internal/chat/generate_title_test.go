package chat_test

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/chat"
	"github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
)

// Omitting title is the read path: it returns the current title and writes
// nothing. An untitled chat falls back to the default.
func TestService_GenerateTitle_ReadReturnsCurrentTitle(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := initSessionCtx(t, ti)

	titledID := seedChat(t, ctx, ti, "", "ext-user", "Existing Title")
	res, err := ti.service.GenerateTitle(ctx, &gen.GenerateTitlePayload{ID: titledID.String()})
	require.NoError(t, err)
	require.Equal(t, "Existing Title", res.Title)

	untitledID := seedChat(t, ctx, ti, "", "ext-user", "")
	res, err = ti.service.GenerateTitle(ctx, &gen.GenerateTitlePayload{ID: untitledID.String()})
	require.NoError(t, err)
	require.Equal(t, "New Chat", res.Title)

	// Read path must not flip the manual flag.
	chat, err := repo.New(ti.conn).GetChat(ctx, untitledID)
	require.NoError(t, err)
	require.False(t, chat.TitleManuallySet)
}

// A non-empty title is persisted and pins the manual flag, and surrounding
// whitespace is trimmed.
func TestService_GenerateTitle_SetManualTitle(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := initSessionCtx(t, ti)

	chatID := seedChat(t, ctx, ti, "", "ext-user", "")
	title := "  My Conversation  "
	res, err := ti.service.GenerateTitle(ctx, &gen.GenerateTitlePayload{ID: chatID.String(), Title: &title})
	require.NoError(t, err)
	require.Equal(t, "My Conversation", res.Title)

	chat, err := repo.New(ti.conn).GetChat(ctx, chatID)
	require.NoError(t, err)
	require.True(t, chat.Title.Valid)
	require.Equal(t, "My Conversation", chat.Title.String)
	require.True(t, chat.TitleManuallySet)
}

// An empty title clears the manual flag and nulls the title so auto-naming can
// take back over.
func TestService_GenerateTitle_ResetClearsManualFlag(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := initSessionCtx(t, ti)

	chatID := seedChat(t, ctx, ti, "", "ext-user", "")
	manual := "Pinned"
	_, err := ti.service.GenerateTitle(ctx, &gen.GenerateTitlePayload{ID: chatID.String(), Title: &manual})
	require.NoError(t, err)

	empty := ""
	res, err := ti.service.GenerateTitle(ctx, &gen.GenerateTitlePayload{ID: chatID.String(), Title: &empty})
	require.NoError(t, err)
	require.Empty(t, res.Title)

	chat, err := repo.New(ti.conn).GetChat(ctx, chatID)
	require.NoError(t, err)
	require.False(t, chat.Title.Valid)
	require.False(t, chat.TitleManuallySet)
}

func TestService_GenerateTitle_RejectsTooLongTitle(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := initSessionCtx(t, ti)

	chatID := seedChat(t, ctx, ti, "", "ext-user", "")
	tooLong := strings.Repeat("a", 201)
	_, err := ti.service.GenerateTitle(ctx, &gen.GenerateTitlePayload{ID: chatID.String(), Title: &tooLong})
	requireOopsCode(t, err, oops.CodeInvalid)
}

// The length bound counts runes, not bytes, matching the Goa MaxLength(200)
// transport validation. A 200-rune multi-byte title is 800 bytes but still
// valid — a byte-based check would wrongly reject it.
func TestService_GenerateTitle_AcceptsMaxLengthMultibyteTitle(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := initSessionCtx(t, ti)

	chatID := seedChat(t, ctx, ti, "", "ext-user", "")
	multibyte := strings.Repeat("世", 200) // 200 runes, 600 bytes
	res, err := ti.service.GenerateTitle(ctx, &gen.GenerateTitlePayload{ID: chatID.String(), Title: &multibyte})
	require.NoError(t, err)
	require.Equal(t, multibyte, res.Title)

	chat, err := repo.New(ti.conn).GetChat(ctx, chatID)
	require.NoError(t, err)
	require.True(t, chat.Title.Valid)
	require.Equal(t, multibyte, chat.Title.String)
	require.True(t, chat.TitleManuallySet)
}

func TestService_GenerateTitle_NotFound(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := initSessionCtx(t, ti)

	title := "Whatever"
	_, err := ti.service.GenerateTitle(ctx, &gen.GenerateTitlePayload{ID: uuid.NewString(), Title: &title})
	requireOopsCode(t, err, oops.CodeNotFound)
}

// A chat owned by another project must not be renamable through this caller's
// project-scoped context.
func TestService_GenerateTitle_CrossProjectUnauthorized(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := initSessionCtx(t, ti)

	otherProject, err := projectsrepo.New(ti.conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
		Name:           "Other Project",
		Slug:           "other-" + uuid.NewString()[:8],
		OrganizationID: ti.orgID,
	})
	require.NoError(t, err)

	otherChatID, err := repo.New(ti.conn).UpsertChat(ctx, repo.UpsertChatParams{
		ID:             uuid.New(),
		ProjectID:      otherProject.ID,
		OrganizationID: ti.orgID,
	})
	require.NoError(t, err)

	title := "Sneaky"
	_, err = ti.service.GenerateTitle(ctx, &gen.GenerateTitlePayload{ID: otherChatID.String(), Title: &title})
	requireOopsCode(t, err, oops.CodeUnauthorized)
}
