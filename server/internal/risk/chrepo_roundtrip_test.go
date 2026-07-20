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
	createdAt := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)

	// A plain (non-excluded) row: excluded_at / exclusion_id bind as nil into
	// the Nullable columns.
	plain := chrepo.RiskFindingRow{
		ID:                       uuid.Must(uuid.NewV7()),
		CreatedAt:                createdAt,
		OrganizationID:           orgID,
		ProjectID:                "proj-1",
		RequestID:                "req-1",
		ChatMessageID:            "chat-1",
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
		MatchLen:                 7,
		MatchRedacted:            "<redacted len=7 sha=deadbeef>",
		FingerprintPepperVersion: "v1",
		FingerprintGlobalHS256:   "global-fp",
		FingerprintTenantHS256:   "tenant-fp",
		ExcludedAt:               nil,
		ExclusionID:              nil,
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
			SELECT id, tags, match_redacted, excluded_at, exclusion_id
			FROM risk_findings
			WHERE organization_id = ?
			ORDER BY created_at
		`, orgID)
		if !assert.NoError(c, err) {
			return
		}
		defer func() { _ = rows.Close() }()

		got := map[uuid.UUID]struct {
			tags        []string
			redacted    string
			excludedAt  *time.Time
			exclusionID *uuid.UUID
		}{}
		for rows.Next() {
			var (
				id       uuid.UUID
				tags     []string
				redacted string
				exAt     *time.Time
				exID     *uuid.UUID
			)
			if !assert.NoError(c, rows.Scan(&id, &tags, &redacted, &exAt, &exID)) {
				return
			}
			got[id] = struct {
				tags        []string
				redacted    string
				excludedAt  *time.Time
				exclusionID *uuid.UUID
			}{tags, redacted, exAt, exID}
		}

		if !assert.Contains(c, got, plain.ID) || !assert.Contains(c, got, excluded.ID) {
			return
		}

		p := got[plain.ID]
		assert.Equal(c, []string{"pii", "secret"}, p.tags, "tags array round-trips")
		assert.Equal(c, plain.MatchRedacted, p.redacted)
		assert.Nil(c, p.excludedAt, "non-excluded row stores NULL excluded_at")
		assert.Nil(c, p.exclusionID, "non-excluded row stores NULL exclusion_id")

		e := got[excluded.ID]
		if assert.NotNil(c, e.excludedAt, "excluded row stores excluded_at") {
			assert.True(c, excludedAt.Equal(*e.excludedAt))
		}
		if assert.NotNil(c, e.exclusionID, "excluded row stores exclusion_id") {
			assert.Equal(c, exclusionID, *e.exclusionID)
		}
	}, 5*time.Second, 100*time.Millisecond)
}
