package platformmcp

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/risk/chatfindings"
	"github.com/speakeasy-api/gram/server/internal/risk/chrepo"
)

func TestChatMetadataList_ClickHouseFindings(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_chat_findings")
	require.NoError(t, err)
	chConn, err := platformMCPInfra.NewClickhouseClient(t)
	require.NoError(t, err)
	principal, project := seedRegistrationLifecycle(t, ctx, conn)

	chatID := uuid.New()
	_, err = chatrepo.New(conn).UpsertChat(ctx, chatrepo.UpsertChatParams{
		ID:             chatID,
		ProjectID:      project.ID,
		OrganizationID: principal.OrganizationID,
		UserID:         pgtype.Text{String: principal.UserID, Valid: true},
		Title:          pgtype.Text{String: "private title", Valid: true},
	})
	require.NoError(t, err)
	messageID, err := chatrepo.New(conn).SeedChatMessage(ctx, chatrepo.SeedChatMessageParams{
		ChatID:    chatID,
		ProjectID: uuid.NullUUID{UUID: project.ID, Valid: true},
	})
	require.NoError(t, err)
	policyID, err := chatrepo.New(conn).SeedRiskPolicy(ctx, chatrepo.SeedRiskPolicyParams{
		ProjectID:      project.ID,
		OrganizationID: principal.OrganizationID,
	})
	require.NoError(t, err)
	now := time.Now().UTC()
	require.NoError(t, chrepo.New(chConn).InsertRiskFindings(ctx, []chrepo.RiskFindingRow{{
		ID:                uuid.New(),
		CreatedAt:         now,
		OrganizationID:    principal.OrganizationID,
		ProjectID:         project.ID.String(),
		ChatMessageID:     messageID.String(),
		RiskPolicyID:      policyID.String(),
		RiskPolicyVersion: 1,
		RuleID:            "secret.test",
		Source:            "gitleaks",
		ChatID:            chatID.String(),
		MessageCreatedAt:  now,
		EventKind:         chrepo.EventKindFinding,
	}}))

	service := NewChatMetadataService(conn, chatfindings.New(conn, chrepo.New(chConn)), allowBudget(), "chat-list-test-key")
	service.now = func() time.Time { return now.Add(time.Minute) }
	output, err := service.List(ctx, principal, ListChatsInput{
		ProjectID: project.ID.String(), Risk: ChatRiskWithFindings,
	})
	require.NoError(t, err)
	require.Equal(t, 1, output.TotalMatches)
	require.Len(t, output.Chats, 1)
	require.Equal(t, chatID.String(), output.Chats[0].ChatID)
	require.Equal(t, 1, output.Chats[0].RiskFindingsCount)
}
