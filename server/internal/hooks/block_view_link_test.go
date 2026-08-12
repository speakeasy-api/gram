package hooks

import (
	"context"
	"errors"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	chatRepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/hooks/repo"
	riskRepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

// blockLinkColumns reads the optional foreign keys straight off the row. The
// block page query does not expose them, and "the row survived" is only half
// the contract — a salvage that cleared the wrong column, or cleared more than
// the database named, would still insert and still pass a survival check.
func blockLinkColumns(t *testing.T, ctx context.Context, conn *pgxpool.Pool, blockID uuid.UUID) testrepo.GetToolCallBlockLinksFixtureRow {
	t.Helper()
	row, err := testrepo.New(conn).GetToolCallBlockLinksFixture(ctx, blockID)
	require.NoError(t, err)
	return row
}

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

	// The enrichment is reduced, not silently retained: both dangling links
	// must be null on the persisted row.
	links := blockLinkColumns(t, ctx, ti.conn, blockID)
	require.False(t, links.ChatID.Valid, "the unresolvable chat link must be cleared, not kept")
	require.False(t, links.RiskPolicyID.Valid, "the unresolvable policy link must be cleared, not kept")
	require.False(t, links.ChatMessageID.Valid)
	require.False(t, links.RiskResultID.Valid)
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

	// Read the columns directly too: the lookup above proves the message link
	// survived, this proves the salvage stopped there.
	links := blockLinkColumns(t, ctx, ti.conn, blockID)
	require.True(t, links.ChatMessageID.Valid, "a link the database never rejected must be kept")
	require.Equal(t, messageID, links.ChatMessageID.UUID)
	require.False(t, links.ChatID.Valid, "the dangling chat link must still be cleared")
	require.False(t, links.RiskPolicyID.Valid, "the dangling policy link must still be cleared")
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
		params := populated()
		links := optionalBlockLinks(&params)
		dropped, ok := clearRejectedBlockLink(links, &pgconn.PgError{Code: pgForeignKeyViolation, ConstraintName: tt.constraint})
		require.True(t, ok, tt.column)
		require.Equal(t, tt.column, dropped)

		for _, link := range links {
			if link.column == tt.column {
				require.False(t, link.value.Valid, "%s must be cleared when its constraint is named", link.column)
				continue
			}
			require.True(t, link.value.Valid, "%s must be left alone when %s is rejected", link.column, tt.column)
		}
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
		dropped, ok := clearRejectedBlockLink(links, tt.err)
		require.False(t, ok, tt.name)
		require.Empty(t, dropped, tt.name)
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

	// Match named, unnamed and composite table-level foreign keys. A form this
	// does not recognize would produce no match at all and silently shrink the
	// expected set, so the count is checked against every REFERENCES in the
	// definition — an inline column-level reference fails here rather than
	// passing as "no new foreign keys".
	matches := regexp.MustCompile(`(?:CONSTRAINT (\w+) )?FOREIGN KEY\s*\(([^)]+)\)`).FindAllStringSubmatch(body, -1)
	require.Len(t, matches, strings.Count(body, "REFERENCES"),
		"a foreign key in tool_call_blocks is written in a form this test cannot parse; widen the pattern rather than leaving it unchecked")

	// Optional links are the foreign keys whose referencing column is nullable.
	// The required tenancy keys (organization_id, project_id) are NOT NULL, so
	// their columns carry the marker and are skipped.
	var optional []string
	for _, fk := range matches {
		name, columns := fk[1], strings.Split(fk[2], ",")

		nullable := false
		for _, column := range columns {
			column = strings.TrimSpace(column)
			// One column per line, so match to end of line: truncating at the
			// first comma would misread a definition like
			// `DEFAULT coalesce(a, b) NOT NULL` as nullable.
			definition := regexp.MustCompile(`(?m)^\s+` + regexp.QuoteMeta(column) + `\s+.*$`).FindString(body)
			require.NotEmpty(t, definition, "column %s not found in the tool_call_blocks definition", column)
			// Drop a trailing comment first: "bar_id uuid, -- NOT NULL once
			// backfilled" is a nullable column, and reading the comment as the
			// constraint would quietly excuse it from the salvage list.
			if marker, _, found := strings.Cut(definition, "--"); found {
				definition = marker
			}
			if !regexp.MustCompile(`(?i)NOT NULL`).MatchString(definition) {
				nullable = true
			}
		}
		if !nullable {
			continue
		}

		// The salvage matches on the constraint name the database reports, so a
		// nullable foreign key without an explicit one cannot be cleared.
		require.NotEmpty(t, name,
			"nullable foreign key on %s has no explicit CONSTRAINT name, so the salvage cannot match it", strings.Join(columns, ","))
		require.Len(t, columns, 1,
			"composite nullable foreign key %s cannot be salvaged by clearing a single column", name)
		optional = append(optional, name)
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
