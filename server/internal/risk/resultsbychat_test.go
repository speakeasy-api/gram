package risk_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/risk"
	"github.com/speakeasy-api/gram/server/internal/authz"
	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/risk/chrepo"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

func seedChatWithUser(t *testing.T, ti *testInstance, projectID uuid.UUID, orgID, externalUserID string) (uuid.UUID, uuid.UUID) {
	t.Helper()
	ctx := t.Context()

	chatID, err := uuid.NewV7()
	require.NoError(t, err)

	_, err = ti.chatRepo.UpsertChat(ctx, chatrepo.UpsertChatParams{
		ID:             chatID,
		ProjectID:      projectID,
		OrganizationID: orgID,
		UserID:         pgtype.Text{},
		ExternalUserID: pgtype.Text{String: externalUserID, Valid: externalUserID != ""},
		Title:          pgtype.Text{String: "test chat", Valid: true},
	})
	require.NoError(t, err)

	msgID, err := testrepo.New(ti.conn).InsertChatMessage(ctx, testrepo.InsertChatMessageParams{
		ChatID:    chatID,
		ProjectID: uuid.NullUUID{UUID: projectID, Valid: true},
		Role:      "user",
		Content:   "test message",
	})
	require.NoError(t, err)

	return chatID, msgID
}

func TestListRiskResultsByChat_GroupsFindings(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)},
	)
	projectID := *authCtx.ProjectID
	organizationID := authCtx.ActiveOrganizationID

	policy, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{Name: new("ByChat Test")})
	require.NoError(t, err)

	// One chat holds a message-anchored and an attachment-anchored finding;
	// a second chat holds only an attachment-anchored one. Both anchors must
	// resolve the chat, so the rollup sees two chats and three findings.
	mixedChat, mixedMsg := seedChatWithUser(t, ti, projectID, organizationID, "alice@example.com")
	attachmentChat, attachmentMsg := seedChatWithUser(t, ti, projectID, organizationID, "bob@example.com")
	at := time.Now().UTC()
	finding := func(chatID, messageID uuid.UUID) chrepo.RiskFindingRow {
		return chListFinding(t, projectID, organizationID, chatID, messageID, policy.ID, at, at, "gitleaks", "secret.generic", "", "", "", "")
	}
	attachment := func(chatID, messageID uuid.UUID) chrepo.RiskFindingRow {
		row := finding(chatID, messageID)
		row.ChatMessageID = ""
		row.ContentPartID = uuid.NewString()
		return row
	}
	require.NoError(t, chrepo.New(ti.chConn).InsertRiskFindings(ctx, []chrepo.RiskFindingRow{
		finding(mixedChat, mixedMsg),
		attachment(mixedChat, mixedMsg),
		attachment(attachmentChat, attachmentMsg),
	}))

	result, err := ti.service.ListRiskResultsByChat(ctx, &gen.ListRiskResultsByChatPayload{})
	require.NoError(t, err)
	require.Len(t, result.Chats, 2)
	counts := map[string]int64{}
	for _, chat := range result.Chats {
		counts[chat.ChatID] = chat.FindingsCount
	}
	require.Equal(t, map[string]int64{mixedChat.String(): 2, attachmentChat.String(): 1}, counts)
	require.Nil(t, result.NextCursor)
}

func TestListRiskResultsByChat_Unauthorized(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)
	ctx = withExactAccessGrants(t, ctx, ti.conn)

	_, err := ti.service.ListRiskResultsByChat(ctx, &gen.ListRiskResultsByChatPayload{})
	require.Error(t, err)
}

func TestListRiskResultsByChat_CountsOnlyLiveEnabledPolicyFindings(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)},
	)
	projectID := *authCtx.ProjectID
	organizationID := authCtx.ActiveOrganizationID

	enabled, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{Name: new("ByChat ClickHouse Enabled")})
	require.NoError(t, err)
	disabled, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{Name: new("ByChat ClickHouse Disabled"), Enabled: new(false)})
	require.NoError(t, err)
	deleted, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{Name: new("ByChat ClickHouse Deleted")})
	require.NoError(t, err)
	require.NoError(t, ti.service.DeleteRiskPolicy(ctx, &gen.DeleteRiskPolicyPayload{ID: deleted.ID}))

	chatID, messageID := seedChatWithUser(t, ti, projectID, organizationID, "alice@example.com")

	at := time.Now().UTC()
	finding := func(policyID string) chrepo.RiskFindingRow {
		return chListFinding(t, projectID, organizationID, chatID, messageID, policyID, at, at, "gitleaks", "secret.generic", "alice@example.com", "", "", "")
	}
	liveOne := finding(enabled.ID)
	liveTwo := finding(enabled.ID)
	dismissed := finding(enabled.ID)
	dismissed.FalsePositiveAt = new(at)
	excluded := finding(enabled.ID)
	excluded.ExcludedAt = new(at)
	shadow := finding(enabled.ID)
	shadow.Shadow = true
	deadLetter := finding(enabled.ID)
	deadLetter.DeadLetterReason = "scanner failed"
	disabledFinding := finding(disabled.ID)
	deletedFinding := finding(deleted.ID)

	require.NoError(t, chrepo.New(ti.chConn).InsertRiskFindings(ctx, []chrepo.RiskFindingRow{
		liveOne,
		liveTwo,
		dismissed,
		excluded,
		shadow,
		deadLetter,
		disabledFinding,
		deletedFinding,
	}))

	result, err := ti.service.ListRiskResultsByChat(ctx, &gen.ListRiskResultsByChatPayload{})
	require.NoError(t, err)
	require.Len(t, result.Chats, 1)
	require.Equal(t, chatID.String(), result.Chats[0].ChatID)
	require.Equal(t, int64(2), result.Chats[0].FindingsCount)
	require.Equal(t, "test chat", *result.Chats[0].ChatTitle)
	require.Equal(t, "alice@example.com", *result.Chats[0].UserID)
}
