package chat_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	mockidp "github.com/speakeasy-api/gram/dev-idp/pkg/testidp"
	gen "github.com/speakeasy-api/gram/server/gen/chat"
	"github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	userRepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

// defaultPayload returns a ListChatsPayload with required non-pointer fields
// set to their design defaults.
func defaultPayload() *gen.ListChatsPayload {
	return &gen.ListChatsPayload{
		Limit:     50,
		Offset:    0,
		SortBy:    "created_at",
		SortOrder: "desc",
	}
}

// requireOopsCode asserts that err is an oops.ShareableError with the given code.
func requireOopsCode(t *testing.T, err error, code oops.Code) {
	t.Helper()
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, code, oopsErr.Code)
}

// seedChat inserts a chat owned by the given userID or externalUserID.
func seedChat(t *testing.T, ctx context.Context, ti *chatTestInstance, userID, externalUserID, title string) uuid.UUID {
	t.Helper()
	id, err := repo.New(ti.conn).UpsertChat(ctx, repo.UpsertChatParams{
		ID:             uuid.New(),
		ProjectID:      ti.projectID,
		OrganizationID: ti.orgID,
		UserID:         pgtype.Text{String: userID, Valid: userID != ""},
		ExternalUserID: pgtype.Text{String: externalUserID, Valid: externalUserID != ""},
		Title:          pgtype.Text{String: title, Valid: title != ""},
	})
	require.NoError(t, err)
	return id
}

// seedChatAtTime inserts a chat at a specific timestamp for date-range tests.
func seedChatAtTime(t *testing.T, ctx context.Context, ti *chatTestInstance, externalUserID string, at time.Time) uuid.UUID {
	t.Helper()
	id, err := repo.New(ti.conn).SeedChatAtTime(ctx, repo.SeedChatAtTimeParams{
		ID:             uuid.New(),
		ProjectID:      ti.projectID,
		OrganizationID: ti.orgID,
		UserID:         pgtype.Text{},
		ExternalUserID: pgtype.Text{String: externalUserID, Valid: externalUserID != ""},
		Title:          pgtype.Text{},
		CreatedAt:      pgtype.Timestamptz{Time: at, InfinityModifier: pgtype.Finite, Valid: true},
	})
	require.NoError(t, err)
	return id
}

// seedRiskOnChat creates a chat message and attaches a risk result to it.
func seedRiskOnChat(t *testing.T, ctx context.Context, ti *chatTestInstance, chatID uuid.UUID, found bool) {
	t.Helper()
	r := repo.New(ti.conn)
	msgID, err := r.SeedChatMessage(ctx, repo.SeedChatMessageParams{
		ChatID:    chatID,
		ProjectID: uuid.NullUUID{UUID: ti.projectID, Valid: true},
	})
	require.NoError(t, err)
	policyID, err := r.SeedRiskPolicy(ctx, repo.SeedRiskPolicyParams{
		ProjectID:      ti.projectID,
		OrganizationID: ti.orgID,
	})
	require.NoError(t, err)
	err = r.SeedRiskResult(ctx, repo.SeedRiskResultParams{
		ProjectID:      ti.projectID,
		OrganizationID: ti.orgID,
		RiskPolicyID:   policyID,
		ChatMessageID:  msgID,
		Found:          found,
	})
	require.NoError(t, err)
}

// initSessionCtx creates a session-authenticated context and overrides ProjectID
// to ti.projectID so that ListChats scopes to the same project as seeded chats.
func initSessionCtx(t *testing.T, ti *chatTestInstance) context.Context {
	t.Helper()
	ctx := testenv.InitAuthContext(t, t.Context(), ti.conn, ti.sessions)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	authCtx.ProjectID = &ti.projectID
	return ctx
}

// makeAdmin upgrades the session user to admin and invalidates the sessions
// cache so GetUserInfo returns Admin: true on the next call.
func makeAdmin(t *testing.T, ctx context.Context, ti *chatTestInstance) {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	_, err := userRepo.New(ti.conn).UpsertUser(ctx, userRepo.UpsertUserParams{
		ID:          authCtx.UserID,
		Email:       mockidp.MockUserEmail,
		DisplayName: "Dev User",
		PhotoUrl:    pgtype.Text{},
		Admin:       true,
	})
	require.NoError(t, err)
	require.NoError(t, ti.sessions.InvalidateUserInfoCache(ctx, authCtx.UserID))
}

// externalUserCtx builds a context carrying an external-user AuthContext
// scoped to ti.projectID.
func externalUserCtx(t *testing.T, ti *chatTestInstance, externalUserID string) context.Context {
	t.Helper()
	authCtx := &contextvalues.AuthContext{
		ExternalUserID:       externalUserID,
		ProjectID:            &ti.projectID,
		ActiveOrganizationID: ti.orgID,
	}
	return contextvalues.SetAuthContext(t.Context(), authCtx)
}

// --- Auth / scoping ---

// TestListChats_NoAuthContext verifies that a request with no auth context at all is rejected.
func TestListChats_NoAuthContext(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	_, err := ti.service.ListChats(t.Context(), defaultPayload())
	requireOopsCode(t, err, oops.CodeUnauthorized)
}

// TestListChats_NoProjectID verifies that an auth context without a project ID is rejected.
func TestListChats_NoProjectID(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	authCtx := &contextvalues.AuthContext{
		UserID: "some-user",
	}
	ctx := contextvalues.SetAuthContext(t.Context(), authCtx)
	_, err := ti.service.ListChats(ctx, defaultPayload())
	requireOopsCode(t, err, oops.CodeUnauthorized)
}

// TestListChats_ExternalUser_SeesOnlyOwnChats verifies that an external user only receives
// chats they own and cannot see chats belonging to other external users.
func TestListChats_ExternalUser_SeesOnlyOwnChats(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := externalUserCtx(t, ti, "ext-123")

	seedChat(t, ctx, ti, "", "ext-123", "chat for ext-123")
	seedChat(t, ctx, ti, "", "ext-456", "chat for ext-456")

	result, err := ti.service.ListChats(ctx, defaultPayload())
	require.NoError(t, err)
	require.Equal(t, 1, result.Total)
	require.Len(t, result.Chats, 1)
	require.Equal(t, "ext-123", conv.PtrValOr(result.Chats[0].ExternalUserID, ""))
}

// TestListChats_ExternalUser_PayloadExternalUserIDIsIgnored verifies that passing a different
// external user ID in the payload is silently ignored; the filter is always forced to the caller's
// own ID, preventing IDOR.
func TestListChats_ExternalUser_PayloadExternalUserIDIsIgnored(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := externalUserCtx(t, ti, "ext-123")

	seedChat(t, ctx, ti, "", "ext-123", "chat for ext-123")
	seedChat(t, ctx, ti, "", "ext-456", "chat for ext-456")

	payload := defaultPayload()
	extUser := "ext-456"
	payload.ExternalUserID = &extUser

	result, err := ti.service.ListChats(ctx, payload)
	require.NoError(t, err)
	require.Equal(t, 1, result.Total)
	require.Len(t, result.Chats, 1)
	require.Equal(t, "ext-123", conv.PtrValOr(result.Chats[0].ExternalUserID, ""))
}

// TestListChats_RegularUser_SeesOnlyOwnChats verifies that a non-admin session user only receives
// their own chats and cannot see chats belonging to other users.
func TestListChats_RegularUser_SeesOnlyOwnChats(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := initSessionCtx(t, ti)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	seedChat(t, ctx, ti, authCtx.UserID, "", "own chat")
	seedChat(t, ctx, ti, "other-user-id", "", "other users chat")

	result, err := ti.service.ListChats(ctx, defaultPayload())
	require.NoError(t, err)
	require.Equal(t, 1, result.Total)
	require.Len(t, result.Chats, 1)
	require.Equal(t, authCtx.UserID, conv.PtrValOr(result.Chats[0].UserID, ""))
}

// TestListChats_AdminUser_SeesAllChats verifies that an admin user can see all chats in the project,
// regardless of which user or external user owns them.
func TestListChats_AdminUser_SeesAllChats(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := initSessionCtx(t, ti)
	makeAdmin(t, ctx, ti)

	seedChat(t, ctx, ti, "", "ext-aaa", "chat A")
	seedChat(t, ctx, ti, "user-bbb", "", "chat B")

	result, err := ti.service.ListChats(ctx, defaultPayload())
	require.NoError(t, err)
	require.Equal(t, 2, result.Total)
	require.Len(t, result.Chats, 2)
}

// TestListChats_AdminUser_FilterByExternalUserID verifies that an admin can narrow results to a
// specific external user via the payload filter.
func TestListChats_AdminUser_FilterByExternalUserID(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := initSessionCtx(t, ti)
	makeAdmin(t, ctx, ti)

	seedChat(t, ctx, ti, "", "ext-123", "chat for ext-123")
	seedChat(t, ctx, ti, "", "ext-456", "chat for ext-456")

	extUser := "ext-123"
	payload := defaultPayload()
	payload.ExternalUserID = &extUser

	result, err := ti.service.ListChats(ctx, payload)
	require.NoError(t, err)
	require.Equal(t, 1, result.Total)
	require.Len(t, result.Chats, 1)
	require.Equal(t, "ext-123", conv.PtrValOr(result.Chats[0].ExternalUserID, ""))
}

// TestListChats_Filter_Search verifies that the search filter matches chats by title substring.
func TestListChats_Filter_Search(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := externalUserCtx(t, ti, "ext-search")

	seedChat(t, ctx, ti, "", "ext-search", "needle found here")
	seedChat(t, ctx, ti, "", "ext-search", "some other title")

	search := "needle"
	payload := defaultPayload()
	payload.Search = &search

	result, err := ti.service.ListChats(ctx, payload)
	require.NoError(t, err)
	require.Equal(t, 1, result.Total)
	require.Len(t, result.Chats, 1)
	require.Contains(t, result.Chats[0].Title, "needle")
}

// TestListChats_Filter_HasRisk_True verifies that has_risk=true returns only chats that have at
// least one risk finding attached.
func TestListChats_Filter_HasRisk_True(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := externalUserCtx(t, ti, "ext-risk")

	risky := seedChat(t, ctx, ti, "", "ext-risk", "risky chat")
	_ = seedChat(t, ctx, ti, "", "ext-risk", "safe chat")

	seedRiskOnChat(t, ctx, ti, risky, true)

	hasRisk := "true"
	payload := defaultPayload()
	payload.HasRisk = &hasRisk

	result, err := ti.service.ListChats(ctx, payload)
	require.NoError(t, err)
	require.Equal(t, 1, result.Total)
	require.Len(t, result.Chats, 1)
	require.Equal(t, risky.String(), result.Chats[0].ID)
}

// TestListChats_Filter_HasRisk_False verifies that has_risk=false returns only chats that have
// no risk findings attached.
func TestListChats_Filter_HasRisk_False(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := externalUserCtx(t, ti, "ext-risk2")

	risky := seedChat(t, ctx, ti, "", "ext-risk2", "risky chat")
	safe := seedChat(t, ctx, ti, "", "ext-risk2", "safe chat")

	seedRiskOnChat(t, ctx, ti, risky, true)

	hasRisk := "false"
	payload := defaultPayload()
	payload.HasRisk = &hasRisk

	result, err := ti.service.ListChats(ctx, payload)
	require.NoError(t, err)
	require.Equal(t, 1, result.Total)
	require.Len(t, result.Chats, 1)
	require.Equal(t, safe.String(), result.Chats[0].ID)
}

// TestListChats_Filter_DateRange verifies that from/to filters include only chats created within
// the specified window and exclude those outside it.
func TestListChats_Filter_DateRange(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := externalUserCtx(t, ti, "ext-time")

	base := time.Now().UTC().Truncate(time.Second)
	earlier := base.Add(-2 * time.Hour)
	later := base.Add(2 * time.Hour)

	oldChat := seedChatAtTime(t, ctx, ti, "ext-time", earlier)
	_ = seedChatAtTime(t, ctx, ti, "ext-time", later)

	from := earlier.Add(-time.Minute).Format(time.RFC3339)
	to := base.Format(time.RFC3339)
	payload := defaultPayload()
	payload.From = &from
	payload.To = &to

	result, err := ti.service.ListChats(ctx, payload)
	require.NoError(t, err)
	require.Equal(t, 1, result.Total)
	require.Len(t, result.Chats, 1)
	require.Equal(t, oldChat.String(), result.Chats[0].ID)
}

// TestListChats_Pagination verifies that limit/offset correctly pages through results and that
// the total count reflects all matching chats regardless of the page window.
func TestListChats_Pagination(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := externalUserCtx(t, ti, "ext-page")

	for range 5 {
		seedChat(t, ctx, ti, "", "ext-page", "chat")
	}

	payload := defaultPayload()
	payload.SortBy = "created_at"
	payload.SortOrder = "asc"
	payload.Limit = 2
	payload.Offset = 0

	page1, err := ti.service.ListChats(ctx, payload)
	require.NoError(t, err)
	require.Equal(t, 5, page1.Total)
	require.Len(t, page1.Chats, 2)

	payload.Offset = 2
	page2, err := ti.service.ListChats(ctx, payload)
	require.NoError(t, err)
	require.Equal(t, 5, page2.Total)
	require.Len(t, page2.Chats, 2)

	// Pages must not overlap.
	require.NotEqual(t, page1.Chats[0].ID, page2.Chats[0].ID)
	require.NotEqual(t, page1.Chats[1].ID, page2.Chats[1].ID)
}

// TestListChats_EmptyResult verifies that the response is well-formed when no chats exist.
func TestListChats_EmptyResult(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := externalUserCtx(t, ti, "ext-empty")

	result, err := ti.service.ListChats(ctx, defaultPayload())
	require.NoError(t, err)
	require.Equal(t, 0, result.Total)
	require.Empty(t, result.Chats)
}

// TestListChats_RiskFindingsCountInResult verifies that each chat in the result carries an accurate
// count of its associated risk findings.
func TestListChats_RiskFindingsCountInResult(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := externalUserCtx(t, ti, "ext-count")

	chatID := seedChat(t, ctx, ti, "", "ext-count", "chat with risk")
	for range 3 {
		seedRiskOnChat(t, ctx, ti, chatID, true)
	}

	result, err := ti.service.ListChats(ctx, defaultPayload())
	require.NoError(t, err)
	require.Len(t, result.Chats, 1)
	require.NotNil(t, result.Chats[0].RiskFindingsCount)
	require.Equal(t, 3, *result.Chats[0].RiskFindingsCount)
}

// TestListChats_InvalidFromTimestamp verifies that a malformed from timestamp returns a bad-request error.
func TestListChats_InvalidFromTimestamp(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := externalUserCtx(t, ti, "ext-bad")

	bad := "not-a-date"
	payload := defaultPayload()
	payload.From = &bad

	_, err := ti.service.ListChats(ctx, payload)
	requireOopsCode(t, err, oops.CodeBadRequest)
}
