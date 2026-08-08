package chat_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/chat/repo"
)

// GetChat is the lookup every per-chat endpoint funnels through. Scoping it in
// SQL is what keeps a forgotten Go-side comparison from becoming a cross-tenant
// read, so assert the predicate directly rather than only through a handler.
func TestQueries_GetChat_IsProjectScoped(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	r := repo.New(ti.conn)

	otherProject := createProjectInSameOrg(t, ti)
	chatID := seedChatInProject(t, ti, otherProject, "other project session")

	_, err := r.GetChat(t.Context(), repo.GetChatParams{ID: chatID, ProjectID: ti.projectID})
	require.ErrorIs(t, err, pgx.ErrNoRows, "a chat in another project must not be readable")

	got, err := r.GetChat(t.Context(), repo.GetChatParams{ID: chatID, ProjectID: otherProject})
	require.NoError(t, err)
	require.Equal(t, chatID, got.ID)
}

// UpsertChat conflicts on the bare primary key, so without the project fence a
// caller supplying another project's chat id lands on that row and gets its id
// back. /chat/completions takes that id straight from a client header.
func TestQueries_UpsertChat_RejectsForeignChatID(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	r := repo.New(ti.conn)

	otherProject := createProjectInSameOrg(t, ti)
	victim := seedChatInProject(t, ti, otherProject, "victim session")

	_, err := r.UpsertChat(t.Context(), repo.UpsertChatParams{
		ID:             victim,
		ProjectID:      ti.projectID,
		OrganizationID: ti.orgID,
		UserID:         pgtype.Text{String: "attacker", Valid: true},
		ExternalUserID: pgtype.Text{},
		Title:          pgtype.Text{String: "hijacked", Valid: true},
	})
	require.ErrorIs(t, err, pgx.ErrNoRows, "upserting onto another project's chat must not resolve")

	// The victim's row is untouched.
	got, err := r.GetChat(t.Context(), repo.GetChatParams{ID: victim, ProjectID: otherProject})
	require.NoError(t, err)
	require.Equal(t, "victim session", got.Title.String)
	require.False(t, got.UserID.Valid)
}

// The project/chat pairing on a feedback row is not enforced by any schema
// constraint, so the query verifies it: a mismatched pair must insert nothing
// rather than write a row whose project_id contradicts its chat_id.
func TestQueries_InsertUserFeedback_RejectsMismatchedProject(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	r := repo.New(ti.conn)

	otherProject := createProjectInSameOrg(t, ti)
	foreignChat := seedChatInProject(t, ti, otherProject, "other project session")

	ctx := initSessionCtx(t, ti)
	ownChat := seedChat(t, ctx, ti, "", "ext-user", "own session")
	msgID := seedNMessages(t, ctx, ti, ownChat, 1)[0]

	_, err := r.InsertUserFeedback(t.Context(), repo.InsertUserFeedbackParams{
		ProjectID:           ti.projectID,
		ChatID:              foreignChat,
		MessageID:           msgID,
		UserResolution:      "failure",
		UserResolutionNotes: pgtype.Text{},
		ChatResolutionID:    uuid.NullUUID{},
	})
	require.ErrorIs(t, err, pgx.ErrNoRows)

	rows, err := r.ListUserFeedbackForChat(t.Context(), repo.ListUserFeedbackForChatParams{
		ChatID:    foreignChat,
		ProjectID: ti.projectID,
	})
	require.NoError(t, err)
	require.Empty(t, rows)
}

// A message belonging to another project must not count toward a chat's
// generation. The generation drives which rows chat.load returns, so an
// unscoped max is how a foreign write blanks an owner's transcript.
func TestQueries_GetMaxGenerationForChat_IsProjectScoped(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	r := repo.New(ti.conn)

	ctx := initSessionCtx(t, ti)
	chatID := seedChat(t, ctx, ti, "", "ext-user", "session")

	// Seed above generation 0 deliberately. COALESCE(MAX(...), 0) returns 0 both
	// for "no rows matched" and for "matched a generation-0 row", so a chat whose
	// only message sits at generation 0 cannot distinguish a scoped query from an
	// unscoped one — the assertion below would hold either way.
	seedTypedMessage(t, ctx, ti, chatID, "user", 3, nil)

	gen, err := r.GetMaxGenerationForChat(t.Context(), repo.GetMaxGenerationForChatParams{
		ChatID:    chatID,
		ProjectID: ti.projectID,
	})
	require.NoError(t, err)
	require.Equal(t, int32(3), gen)

	otherProject := createProjectInSameOrg(t, ti)
	genElsewhere, err := r.GetMaxGenerationForChat(t.Context(), repo.GetMaxGenerationForChatParams{
		ChatID:    chatID,
		ProjectID: otherProject,
	})
	require.NoError(t, err)
	require.Equal(t, int32(0), genElsewhere,
		"another project sees none of this chat's messages; dropping the project_id predicate would report 3")
}
