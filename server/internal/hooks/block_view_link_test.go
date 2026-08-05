package hooks

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	riskRepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
)

// TestToolCallBlockSurvivesUnresolvableChatLink: the block URL is handed to the
// agent before the row is written, so losing the insert means the user opens a
// page that does not exist. Enforcement runs before the hook's chat row is
// persisted, so a block early in a session races its own chat and the chat_id
// FK rejects. The block must still land, with the unresolvable links dropped.
func TestToolCallBlockSurvivesUnresolvableChatLink(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	blockID, err := uuid.NewV7()
	require.NoError(t, err)

	// A session whose chat row does not exist, plus a policy id naming no
	// policy — two optional links pointing at absent rows, so the retry has to
	// clear more than one.
	ti.service.insertToolCallBlock(ctx, blockID, toolCallBlockParams{
		Provider:       "codex",
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      *authCtx.ProjectID,
		Reason:         "blocked for the test",
		ToolName:       "Bash",
		UserID:         authCtx.UserID,
		RiskPolicyID:   uuid.NullUUID{UUID: uuid.New(), Valid: true},
		RiskResultID:   uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ChatID:         chatIDForBlock("session-with-no-chat-row"),
		ChatMessageID:  uuid.NullUUID{UUID: uuid.Nil, Valid: false},
	})

	block, err := riskRepo.New(ti.conn).GetToolCallBlock(ctx, riskRepo.GetToolCallBlockParams{
		ID:           blockID,
		ViewerUserID: authCtx.UserID,
	})
	require.NoError(t, err, "the block must be readable even though its links were unresolvable")
	require.Equal(t, "blocked for the test", block.Reason)
	require.Equal(t, *authCtx.ProjectID, block.ProjectID)
}

// TestToolCallBlockRecordsWithoutLinks covers the ordinary path: a block
// carrying no optional links inserts on the first attempt.
func TestToolCallBlockRecordsWithoutLinks(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	blockID, err := uuid.NewV7()
	require.NoError(t, err)

	ti.service.insertToolCallBlock(ctx, blockID, toolCallBlockParams{
		Provider:       "codex",
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      *authCtx.ProjectID,
		Reason:         "blocked with no links",
		ToolName:       "Bash",
		UserID:         authCtx.UserID,
		RiskPolicyID:   uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		RiskResultID:   uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ChatID:         uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ChatMessageID:  uuid.NullUUID{UUID: uuid.Nil, Valid: false},
	})

	block, err := riskRepo.New(ti.conn).GetToolCallBlock(ctx, riskRepo.GetToolCallBlockParams{
		ID:           blockID,
		ViewerUserID: authCtx.UserID,
	})
	require.NoError(t, err)
	require.Equal(t, "blocked with no links", block.Reason)
}
