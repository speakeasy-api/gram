package chat_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/chat"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// defaultPayload returns a ListChatsPayload with required non-pointer fields
// set to their design defaults.
func defaultPayload() *gen.ListChatsPayload {
	return &gen.ListChatsPayload{
		Limit:     50,
		Offset:    0,
		SortBy:    "last_message_timestamp",
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

// seedChatWithSource inserts a chat owned by externalUserID with a single
// message carrying the given source, so the chat's inferred source (the latest
// non-null message source) is `source`.
func seedChatWithSource(t *testing.T, ctx context.Context, ti *chatTestInstance, externalUserID, source string) uuid.UUID {
	t.Helper()
	chatID := seedChat(t, ctx, ti, "", externalUserID, "chat for "+source)
	_, err := repo.New(ti.conn).SeedChatMessageWithSource(ctx, repo.SeedChatMessageWithSourceParams{
		ChatID:    chatID,
		ProjectID: uuid.NullUUID{UUID: ti.projectID, Valid: true},
		Source:    pgtype.Text{String: source, Valid: true},
	})
	require.NoError(t, err)
	return chatID
}

// seedRiskOnChatDisabledPolicy seeds a found risk result whose policy is
// disabled. The finding row is real, but every risk surface must treat it as
// absent because the policy is off (mirrors a policy disabled after detection).
func seedRiskOnChatDisabledPolicy(t *testing.T, ctx context.Context, ti *chatTestInstance, chatID uuid.UUID) {
	t.Helper()
	r := repo.New(ti.conn)
	msgID, err := r.SeedChatMessage(ctx, repo.SeedChatMessageParams{
		ChatID:    chatID,
		ProjectID: uuid.NullUUID{UUID: ti.projectID, Valid: true},
	})
	require.NoError(t, err)
	policyID, err := r.SeedDisabledRiskPolicy(ctx, repo.SeedDisabledRiskPolicyParams{
		ProjectID:      ti.projectID,
		OrganizationID: ti.orgID,
	})
	require.NoError(t, err)
	err = r.SeedRiskResult(ctx, repo.SeedRiskResultParams{
		ProjectID:      ti.projectID,
		OrganizationID: ti.orgID,
		RiskPolicyID:   policyID,
		ChatMessageID:  msgID,
		Found:          true,
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

// grantOrgAdmin returns a context carrying an org:admin RBAC grant for the
// caller's active organization, with RBAC enforcement active (enterprise).
// This is what a real customer org admin has — distinct from the platform-staff
// users.admin flag the visibility gate used to read.
func grantOrgAdmin(t *testing.T, ctx context.Context) context.Context {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	return authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID))
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

// managedAssistantCtx builds a context that mirrors what the assistant runtime
// installs when the managed assistant invokes a platform tool: a non-admin
// owner identity, no session, and a registered assistant principal. Chat svc
// treats that combination as admin-equivalent for project-wide reads.
func managedAssistantCtx(t *testing.T, ti *chatTestInstance, ownerUserID string) context.Context {
	t.Helper()
	authCtx := &contextvalues.AuthContext{
		UserID:               ownerUserID,
		ProjectID:            &ti.projectID,
		ActiveOrganizationID: ti.orgID,
	}
	ctx := contextvalues.SetAuthContext(t.Context(), authCtx)
	ctx = contextvalues.SetAssistantPrincipal(ctx, contextvalues.AssistantPrincipal{
		AssistantID: uuid.New(),
		ThreadID:    uuid.New(),
	})
	return ctx
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

// TestListChats_OrgAdmin_SeesAllChats verifies that a customer org admin (holding
// the org:admin RBAC scope, no platform-staff flag) can see all chats in the
// project, regardless of which user or external user owns them.
func TestListChats_OrgAdmin_SeesAllChats(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := grantOrgAdmin(t, initSessionCtx(t, ti))

	seedChat(t, ctx, ti, "", "ext-aaa", "chat A")
	seedChat(t, ctx, ti, "user-bbb", "", "chat B")

	result, err := ti.service.ListChats(ctx, defaultPayload())
	require.NoError(t, err)
	require.Equal(t, 2, result.Total)
	require.Len(t, result.Chats, 2)
}

// TestListChats_Member_SeesOnlyOwnChats verifies that a session user who holds
// no org:admin grant (a regular member) is scoped to their own chats even when
// RBAC is enforced for the org.
func TestListChats_Member_SeesOnlyOwnChats(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	// WithExactGrants marks the context enterprise (RBAC active) but grants
	// nothing, so the org:admin check is denied.
	ctx := authztest.WithExactGrants(t, initSessionCtx(t, ti))
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

// TestListChats_RBACDisabled_SeesOnlyOwnChats is the regression guard for the
// RBAC-disabled org: enforcement is off, so even a would-be admin must fall back
// to own-sessions-only rather than seeing every chat (Require short-circuits to
// allow when enforcement is off — the handler must not treat that as admin).
func TestListChats_RBACDisabled_SeesOnlyOwnChats(t *testing.T) {
	t.Parallel()
	ti := newTestChatServiceRBACDisabled(t)
	// Mark the context enterprise + grant org:admin; with the org's RBAC
	// feature flag off, ShouldEnforce still returns false and the grant is moot.
	ctx := grantOrgAdmin(t, initSessionCtx(t, ti))
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

// TestListChats_ManagedAssistant_SeesAllChats verifies that the managed
// assistant runtime (no session, non-admin owner, assistant principal
// installed) gets project-wide chat results — the sidebar contract.
func TestListChats_ManagedAssistant_SeesAllChats(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := managedAssistantCtx(t, ti, "owner-non-admin")

	seedChat(t, ctx, ti, "", "ext-aaa", "chat A")
	seedChat(t, ctx, ti, "user-bbb", "", "chat B")

	result, err := ti.service.ListChats(ctx, defaultPayload())
	require.NoError(t, err)
	require.Equal(t, 2, result.Total)
	require.Len(t, result.Chats, 2)
}

// TestLoadChat_ManagedAssistant_LoadsExternalUserChat verifies that the
// managed assistant runtime can load chats owned by an external user — the
// owner-scoped check that fires for non-session callers is skipped when an
// assistant principal is present.
func TestLoadChat_ManagedAssistant_LoadsExternalUserChat(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := managedAssistantCtx(t, ti, "owner-non-admin")

	chatID := seedChat(t, ctx, ti, "", "ext-other", "chat owned by external user")

	result, err := ti.service.LoadChat(ctx, &gen.LoadChatPayload{ID: chatID.String()})
	require.NoError(t, err)
	require.Equal(t, chatID.String(), result.ID)
}

// TestListChats_OrgAdmin_FilterByExternalUserID verifies that an org admin can narrow results to a
// specific external user via the payload filter.
func TestListChats_OrgAdmin_FilterByExternalUserID(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := grantOrgAdmin(t, initSessionCtx(t, ti))

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

// TestListChats_Filter_MinRiskScore verifies that min_risk_score keeps only chats whose active
// risk-finding count is at least the threshold (inclusive), and that the reported
// risk_findings_count matches.
func TestListChats_Filter_MinRiskScore(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := externalUserCtx(t, ti, "ext-minrisk")

	high := seedChat(t, ctx, ti, "", "ext-minrisk", "high risk chat")
	low := seedChat(t, ctx, ti, "", "ext-minrisk", "low risk chat")
	_ = seedChat(t, ctx, ti, "", "ext-minrisk", "safe chat")

	// high accrues 3 findings, low accrues 1, safe none.
	seedRiskOnChat(t, ctx, ti, high, true)
	seedRiskOnChat(t, ctx, ti, high, true)
	seedRiskOnChat(t, ctx, ti, high, true)
	seedRiskOnChat(t, ctx, ti, low, true)

	// Threshold 3 is inclusive: only the chat with exactly 3 findings qualifies.
	min3 := 3
	payload := defaultPayload()
	payload.MinRiskScore = &min3

	result, err := ti.service.ListChats(ctx, payload)
	require.NoError(t, err)
	require.Equal(t, 1, result.Total)
	require.Len(t, result.Chats, 1)
	require.Equal(t, high.String(), result.Chats[0].ID)
	require.NotNil(t, result.Chats[0].RiskFindingsCount)
	require.Equal(t, 3, *result.Chats[0].RiskFindingsCount)

	// Threshold 1 keeps both chats with findings but excludes the safe chat.
	min1 := 1
	payload = defaultPayload()
	payload.MinRiskScore = &min1

	result, err = ti.service.ListChats(ctx, payload)
	require.NoError(t, err)
	require.Equal(t, 2, result.Total)
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

func TestListChats_DateRangeAndSortUseLastMessageTimestamp(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := externalUserCtx(t, ti, "ext-activity")

	now := time.Now().UTC().Truncate(time.Second)
	oldCreatedAt := now.Add(-7 * 24 * time.Hour)
	newCreatedAt := now.Add(-2 * time.Hour)

	// Assign explicit, distinct message timestamps so the last_message_timestamp
	// ordering is deterministic without relying on a wall-clock gap. resumedOldChat
	// gets the more recent message, so it sorts first under desc.
	newerCreatedChat := seedChatAtTime(t, ctx, ti, "ext-activity", newCreatedAt)
	_, err := repo.New(ti.conn).SeedChatMessage(ctx, repo.SeedChatMessageParams{
		ChatID:    newerCreatedChat,
		ProjectID: uuid.NullUUID{UUID: ti.projectID, Valid: true},
		CreatedAt: pgtype.Timestamptz{Time: now.Add(-10 * time.Minute), InfinityModifier: pgtype.Finite, Valid: true},
	})
	require.NoError(t, err)

	resumedOldChat := seedChatAtTime(t, ctx, ti, "ext-activity", oldCreatedAt)
	_, err = repo.New(ti.conn).SeedChatMessage(ctx, repo.SeedChatMessageParams{
		ChatID:    resumedOldChat,
		ProjectID: uuid.NullUUID{UUID: ti.projectID, Valid: true},
		CreatedAt: pgtype.Timestamptz{Time: now.Add(-5 * time.Minute), InfinityModifier: pgtype.Finite, Valid: true},
	})
	require.NoError(t, err)

	from := now.Add(-24 * time.Hour).Format(time.RFC3339)
	to := now.Add(24 * time.Hour).Format(time.RFC3339)
	payload := defaultPayload()
	payload.From = &from
	payload.To = &to
	payload.SortBy = "last_message_timestamp"
	payload.SortOrder = "desc"

	result, err := ti.service.ListChats(ctx, payload)
	require.NoError(t, err)
	require.Equal(t, 2, result.Total)
	require.Len(t, result.Chats, 2)
	require.Equal(t, resumedOldChat.String(), result.Chats[0].ID)
	require.Equal(t, newerCreatedChat.String(), result.Chats[1].ID)
}

func TestListChats_SortByLastMessageTimestampAscending(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := externalUserCtx(t, ti, "ext-activity-asc")

	// Distinct message timestamps make the asc ordering deterministic without a
	// wall-clock gap: firstActiveChat's message precedes lastActiveChat's.
	now := time.Now().UTC()
	firstActiveChat := seedChat(t, ctx, ti, "", "ext-activity-asc", "first active")
	_, err := repo.New(ti.conn).SeedChatMessage(ctx, repo.SeedChatMessageParams{
		ChatID:    firstActiveChat,
		ProjectID: uuid.NullUUID{UUID: ti.projectID, Valid: true},
		CreatedAt: pgtype.Timestamptz{Time: now.Add(-10 * time.Minute), InfinityModifier: pgtype.Finite, Valid: true},
	})
	require.NoError(t, err)

	lastActiveChat := seedChat(t, ctx, ti, "", "ext-activity-asc", "last active")
	_, err = repo.New(ti.conn).SeedChatMessage(ctx, repo.SeedChatMessageParams{
		ChatID:    lastActiveChat,
		ProjectID: uuid.NullUUID{UUID: ti.projectID, Valid: true},
		CreatedAt: pgtype.Timestamptz{Time: now.Add(-5 * time.Minute), InfinityModifier: pgtype.Finite, Valid: true},
	})
	require.NoError(t, err)

	payload := defaultPayload()
	payload.SortBy = "last_message_timestamp"
	payload.SortOrder = "asc"

	result, err := ti.service.ListChats(ctx, payload)
	require.NoError(t, err)
	require.Len(t, result.Chats, 2)
	require.Equal(t, firstActiveChat.String(), result.Chats[0].ID)
	require.Equal(t, lastActiveChat.String(), result.Chats[1].ID)
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
	payload.SortBy = "last_message_timestamp"
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

// TestListChats_DisabledPolicyFinding_NotCounted verifies that a found risk
// result under a disabled policy is excluded from the per-chat count and from
// the has_risk filter — matching the risk.results.list detail view, so the
// chat-list "N risk" badge can't disagree with an empty detail panel.
func TestListChats_DisabledPolicyFinding_NotCounted(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := externalUserCtx(t, ti, "ext-disabled")

	chatID := seedChat(t, ctx, ti, "", "ext-disabled", "chat with disabled-policy finding")
	seedRiskOnChatDisabledPolicy(t, ctx, ti, chatID)

	result, err := ti.service.ListChats(ctx, defaultPayload())
	require.NoError(t, err)
	require.Len(t, result.Chats, 1)
	require.NotNil(t, result.Chats[0].RiskFindingsCount)
	require.Equal(t, 0, *result.Chats[0].RiskFindingsCount, "disabled-policy finding must not count")

	// has_risk=true must not surface the chat; has_risk=false must.
	hasRisk := "true"
	pTrue := defaultPayload()
	pTrue.HasRisk = &hasRisk
	resTrue, err := ti.service.ListChats(ctx, pTrue)
	require.NoError(t, err)
	require.Empty(t, resTrue.Chats, "disabled-policy finding must not match has_risk=true")

	noRisk := "false"
	pFalse := defaultPayload()
	pFalse.HasRisk = &noRisk
	resFalse, err := ti.service.ListChats(ctx, pFalse)
	require.NoError(t, err)
	require.Len(t, resFalse.Chats, 1, "chat with only a disabled-policy finding reads as no-risk")
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

// TestListChats_Filter_Source verifies the source filter matches chats by their
// inferred (latest non-null) message source and accepts a comma-separated list.
func TestListChats_Filter_Source(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := grantOrgAdmin(t, initSessionCtx(t, ti))

	claude := seedChatWithSource(t, ctx, ti, "ext-src", "claude-code")
	_ = seedChatWithSource(t, ctx, ti, "ext-src", "Codex")
	playground := seedChatWithSource(t, ctx, ti, "ext-src", "playground")

	source := "claude-code,playground"
	payload := defaultPayload()
	payload.Source = &source

	result, err := ti.service.ListChats(ctx, payload)
	require.NoError(t, err)
	require.Equal(t, 2, result.Total)
	got := map[string]bool{}
	for _, c := range result.Chats {
		got[c.ID] = true
	}
	require.True(t, got[claude.String()], "expected claude-code chat in results")
	require.True(t, got[playground.String()], "expected playground chat in results")
}

// TestListChats_Filter_Source_EmptyReturnsAll guards against the regression
// where an empty source filter sent SQL NULL and dropped every row.
func TestListChats_Filter_Source_EmptyReturnsAll(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := grantOrgAdmin(t, initSessionCtx(t, ti))

	seedChatWithSource(t, ctx, ti, "ext-src", "claude-code")
	seedChatWithSource(t, ctx, ti, "ext-src", "Codex")

	empty := ""
	payload := defaultPayload()
	payload.Source = &empty

	result, err := ti.service.ListChats(ctx, payload)
	require.NoError(t, err)
	require.Equal(t, 2, result.Total)
}

// TestListSources returns the distinct inferred sources present in the project.
func TestListSources(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := grantOrgAdmin(t, initSessionCtx(t, ti))

	seedChatWithSource(t, ctx, ti, "ext-src", "claude-code")
	seedChatWithSource(t, ctx, ti, "ext-src", "Codex")
	seedChatWithSource(t, ctx, ti, "ext-src", "claude-code") // duplicate collapses

	result, err := ti.service.ListSources(ctx, &gen.ListSourcesPayload{})
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"Codex", "claude-code"}, result.Sources)
}
