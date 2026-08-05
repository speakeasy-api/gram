package hooks

import (
	"errors"
	"os"
	"regexp"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	chatRepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/hooks/repo"
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

// TestToolCallBlockKeepsResolvableLink guards the other half of the contract:
// the salvage clears only the link the database names. A block that loses its
// resolvable links would still insert, but the dashboard would lose the
// enrichment the block page is built from.
func TestToolCallBlockKeepsResolvableLink(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	blockID, err := uuid.NewV7()
	require.NoError(t, err)

	// A chat message that does exist, so its link resolves, alongside a chat id
	// and a policy id naming nothing. Only the two dangling links may be
	// cleared.
	chatID, err := chatRepo.New(ti.conn).UpsertChat(ctx, chatRepo.UpsertChatParams{
		ID:             uuid.New(),
		ProjectID:      *authCtx.ProjectID,
		OrganizationID: authCtx.ActiveOrganizationID,
		UserID:         pgtype.Text{String: authCtx.UserID, Valid: true},
		ExternalUserID: pgtype.Text{String: "", Valid: false},
		Title:          pgtype.Text{String: "block link test", Valid: true},
	})
	require.NoError(t, err)
	messageID, err := chatRepo.New(ti.conn).SeedChatMessage(ctx, chatRepo.SeedChatMessageParams{
		ChatID:    chatID,
		ProjectID: uuid.NullUUID{UUID: *authCtx.ProjectID, Valid: true},
		CreatedAt: pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false},
	})
	require.NoError(t, err)

	ti.service.insertToolCallBlock(ctx, blockID, toolCallBlockParams{
		Provider:       "codex",
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      *authCtx.ProjectID,
		Reason:         "blocked with one resolvable link",
		ToolName:       "Bash",
		UserID:         authCtx.UserID,
		RiskPolicyID:   uuid.NullUUID{UUID: uuid.New(), Valid: true},
		RiskResultID:   uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ChatID:         chatIDForBlock("session-with-no-chat-row-" + blockID.String()),
		ChatMessageID:  uuid.NullUUID{UUID: messageID, Valid: true},
	})

	// Looking the block up *by* its message link is the observation: the row
	// only comes back while chat_message_id still points at the seeded message.
	blocks, err := riskRepo.New(ti.conn).ListLatestToolCallBlocksByMessageIDs(ctx, riskRepo.ListLatestToolCallBlocksByMessageIDsParams{
		ProjectID: *authCtx.ProjectID,
		Ids:       []uuid.UUID{messageID},
	})
	require.NoError(t, err)
	require.Len(t, blocks, 1, "the resolvable message link must survive the salvage")
	require.Equal(t, blockID, blocks[0].BlockID)
}

// TestClearRejectedBlockLinkClearsOnlyNamedLink states the salvage contract
// directly: the database names one constraint, and exactly the link behind it
// is dropped. Clearing more would silently strip enrichment the block page is
// built from; clearing less would loop without progress.
func TestClearRejectedBlockLinkClearsOnlyNamedLink(t *testing.T) {
	t.Parallel()

	populated := func() repo.InsertToolCallBlockParams {
		return repo.InsertToolCallBlockParams{
			ChatID:        uuid.NullUUID{UUID: uuid.New(), Valid: true},
			ChatMessageID: uuid.NullUUID{UUID: uuid.New(), Valid: true},
			RiskResultID:  uuid.NullUUID{UUID: uuid.New(), Valid: true},
			RiskPolicyID:  uuid.NullUUID{UUID: uuid.New(), Valid: true},
		}
	}

	for _, tt := range []struct {
		constraint string
		column     string
	}{
		{constraint: "tool_call_blocks_chat_id_fkey", column: "chat_id"},
		{constraint: "tool_call_blocks_chat_message_id_fkey", column: "chat_message_id"},
		{constraint: "tool_call_blocks_risk_result_id_fkey", column: "risk_result_id"},
		{constraint: "tool_call_blocks_risk_policy_id_fkey", column: "risk_policy_id"},
	} {
		t.Run(tt.column, func(t *testing.T) {
			t.Parallel()

			params := populated()
			links := optionalBlockLinks(&params)
			dropped, ok := clearRejectedBlockLink(links, &pgconn.PgError{Code: pgForeignKeyViolation, ConstraintName: tt.constraint})
			require.True(t, ok)
			require.Equal(t, tt.column, dropped)

			for _, link := range links {
				if link.column == tt.column {
					require.False(t, link.value.Valid, "the named link must be cleared")
					continue
				}
				require.True(t, link.value.Valid, "%s must be left alone when %s is rejected", link.column, tt.column)
			}
		})
	}
}

// TestClearRejectedBlockLinkRefusesUnsalvageable pins the loop's exit
// conditions: anything that is not an optional link pointing at a missing row
// must stop the retry rather than re-run the identical insert.
func TestClearRejectedBlockLinkRefusesUnsalvageable(t *testing.T) {
	t.Parallel()

	params := repo.InsertToolCallBlockParams{
		ChatID:        uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ChatMessageID: uuid.NullUUID{UUID: uuid.New(), Valid: true},
		RiskResultID:  uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		RiskPolicyID:  uuid.NullUUID{UUID: uuid.Nil, Valid: false},
	}
	links := optionalBlockLinks(&params)

	for _, tt := range []struct {
		name string
		err  error
	}{
		{name: "not a foreign key violation", err: &pgconn.PgError{Code: "23505", ConstraintName: "tool_call_blocks_pkey"}},
		{name: "not a pg error at all", err: errors.New("connection reset")},
		// project_id is required tenancy, not enrichment: there is no version
		// of this block worth keeping without it.
		{name: "required tenancy link", err: &pgconn.PgError{Code: pgForeignKeyViolation, ConstraintName: "tool_call_blocks_project_id_fkey"}},
		// A violation naming an already-null link means something else is
		// wrong; clearing it again would not change the row.
		{name: "link already null", err: &pgconn.PgError{Code: pgForeignKeyViolation, ConstraintName: "tool_call_blocks_chat_id_fkey"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dropped, ok := clearRejectedBlockLink(links, tt.err)
			require.False(t, ok)
			require.Empty(t, dropped)
		})
	}

	require.True(t, params.ChatMessageID.Valid, "no link may be cleared when the error is unsalvageable")
}

// TestOptionalBlockLinksCoverSchema pins the salvage list to the table
// definition. An optional foreign key that nobody adds to optionalBlockLinks is
// unsalvageable, so blocks racing it would be dropped again — the DNO-769
// failure. This fails at review time instead.
func TestOptionalBlockLinksCoverSchema(t *testing.T) {
	t.Parallel()

	schema, err := os.ReadFile("../../database/schema.sql")
	require.NoError(t, err)

	table := regexp.MustCompile(`(?s)CREATE TABLE IF NOT EXISTS tool_call_blocks \((.*?)\n\);`).FindSubmatch(schema)
	require.NotNil(t, table, "tool_call_blocks table not found in database/schema.sql")
	body := string(table[1])

	// Optional links are the foreign keys whose referencing column is nullable.
	// The required tenancy keys (organization_id, project_id) are NOT NULL, so
	// their columns carry the marker and are skipped.
	var optional []string
	for _, fk := range regexp.MustCompile(`CONSTRAINT (\w+) FOREIGN KEY \((\w+)\)`).FindAllStringSubmatch(body, -1) {
		column := regexp.MustCompile(`(?m)^\s+` + fk[2] + `\s+[^,]*`).FindString(body)
		require.NotEmpty(t, column, "column %s not found in the tool_call_blocks definition", fk[2])
		if !regexp.MustCompile(`(?i)NOT NULL`).MatchString(column) {
			optional = append(optional, fk[1])
		}
	}

	var params repo.InsertToolCallBlockParams
	links := optionalBlockLinks(&params)
	handled := make([]string, 0, len(links))
	for _, link := range links {
		handled = append(handled, link.constraint)
	}

	require.ElementsMatch(t, optional, handled,
		"optionalBlockLinks must list exactly the nullable foreign keys on tool_call_blocks; a new one has to be added there or blocks racing it get dropped")
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
