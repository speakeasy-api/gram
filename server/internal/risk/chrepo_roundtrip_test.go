package risk_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/risk/chrepo"
)

// TestInsertRiskFindings_RoundTrip exercises the only code in this package that
// talks to a real ClickHouse: the placeholder binding of nil *uuid.UUID /
// *time.Time into the Nullable columns, the []string tags array, and the insert
// column list vs the actual schema. A fake inserter can't catch a server-side
// parse error here — async_insert with wait_for_async_insert=0 does not surface
// one — so this drives InsertRiskFindings against the container and reads back.
func TestInsertRiskFindings_RoundTrip(t *testing.T) {
	t.Parallel()

	conn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)

	q := chrepo.New(conn)

	orgID := "org_" + uuid.NewString()
	// Relative date: rows older than the table's 90-day created_at TTL expire
	// at insert time, so a hardcoded date would silently break the round trip
	// once the calendar catches up.
	createdAt := time.Now().UTC().AddDate(0, 0, -1).Truncate(time.Hour)

	// A plain (non-excluded) row: excluded_at / exclusion_id bind as nil into
	// the Nullable columns.
	plain := chrepo.RiskFindingRow{
		ID:                       uuid.Must(uuid.NewV7()),
		CreatedAt:                createdAt,
		OrganizationID:           orgID,
		ProjectID:                "proj-1",
		RequestID:                "req-1",
		ChatMessageID:            "chat-1",
		ContentPartID:            "",
		RiskPolicyID:             "policy-1",
		RiskPolicyVersion:        7,
		RuleID:                   "pii.email_address",
		Description:              "an email",
		Source:                   "presidio",
		Confidence:               0.95,
		Tags:                     []string{"pii", "secret"},
		StartPos:                 3,
		EndPos:                   10,
		DeadLetterReason:         "",
		ChatID:                   uuid.NewString(),
		UserID:                   "user-1",
		ExternalUserID:           "user-1@example.com",
		Category:                 "pii",
		MatchLen:                 7,
		MatchRedacted:            "<redacted len=7 sha=deadbeef>",
		FingerprintPepperVersion: "v1",
		FingerprintGlobalHS256:   "global-fp",
		FingerprintTenantHS256:   "tenant-fp",
		ExcludedAt:               nil,
		ExclusionID:              nil,
		MessageCreatedAt:         createdAt.Add(-time.Minute),
		AssistantID:              "assistant-1",
		Surface:                  "json_path",
		Field:                    "tool.args",
		Path:                     "command.0",
		ToolCallID:               "call_abc123",
	}

	// An excluded row: excluded_at / exclusion_id are populated.
	excludedAt := createdAt.Add(time.Minute)
	exclusionID := uuid.Must(uuid.NewV7())
	excluded := plain
	excluded.ID = uuid.Must(uuid.NewV7())
	excluded.Tags = []string{}
	excluded.ExcludedAt = &excludedAt
	excluded.ExclusionID = &exclusionID

	require.NoError(t, q.InsertRiskFindings(t.Context(), []chrepo.RiskFindingRow{plain, excluded}))

	// async_insert=1, wait_for_async_insert=0: the rows land after the buffer
	// flushes, so poll until both are visible.
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		rows, err := conn.Query(t.Context(), `
			SELECT id, tags, match_redacted, chat_id, user_id, external_user_id, category, excluded_at, exclusion_id, message_created_at, assistant_id, surface, field, path, tool_call_id
			FROM risk_findings
			WHERE organization_id = ?
			ORDER BY created_at
		`, orgID)
		if !assert.NoError(c, err) {
			return
		}
		defer func() { _ = rows.Close() }()

		type foundRow struct {
			tags             []string
			redacted         string
			chatID           string
			userID           string
			externalUserID   string
			category         string
			excludedAt       *time.Time
			exclusionID      *uuid.UUID
			messageCreatedAt time.Time
			assistantID      string
			surface          string
			field            string
			path             string
			toolCallID       string
		}
		got := map[uuid.UUID]foundRow{}
		for rows.Next() {
			var (
				id  uuid.UUID
				row foundRow
			)
			if !assert.NoError(c, rows.Scan(&id, &row.tags, &row.redacted, &row.chatID, &row.userID, &row.externalUserID, &row.category, &row.excludedAt, &row.exclusionID, &row.messageCreatedAt, &row.assistantID, &row.surface, &row.field, &row.path, &row.toolCallID)) {
				return
			}
			got[id] = row
		}

		if !assert.Contains(c, got, plain.ID) || !assert.Contains(c, got, excluded.ID) {
			return
		}

		p := got[plain.ID]
		assert.Equal(c, []string{"pii", "secret"}, p.tags, "tags array round-trips")
		assert.Equal(c, plain.MatchRedacted, p.redacted)
		assert.Equal(c, plain.ChatID, p.chatID, "attribution chat_id round-trips")
		assert.Equal(c, plain.UserID, p.userID, "attribution user_id round-trips")
		assert.Equal(c, plain.ExternalUserID, p.externalUserID, "attribution external_user_id round-trips")
		assert.Equal(c, plain.Category, p.category, "category round-trips")
		assert.Nil(c, p.excludedAt, "non-excluded row stores NULL excluded_at")
		assert.Nil(c, p.exclusionID, "non-excluded row stores NULL exclusion_id")
		assert.True(c, plain.MessageCreatedAt.Equal(p.messageCreatedAt), "message_created_at round-trips")
		assert.Equal(c, plain.AssistantID, p.assistantID, "assistant_id round-trips")
		assert.Equal(c, plain.Surface, p.surface, "surface round-trips")
		assert.Equal(c, plain.Field, p.field, "field round-trips")
		assert.Equal(c, plain.Path, p.path, "path round-trips")
		assert.Equal(c, plain.ToolCallID, p.toolCallID, "tool_call_id round-trips")

		e := got[excluded.ID]
		if assert.NotNil(c, e.excludedAt, "excluded row stores excluded_at") {
			assert.True(c, excludedAt.Equal(*e.excludedAt))
		}
		if assert.NotNil(c, e.exclusionID, "excluded row stores exclusion_id") {
			assert.Equal(c, exclusionID, *e.exclusionID)
		}
	}, 5*time.Second, 100*time.Millisecond)
}
