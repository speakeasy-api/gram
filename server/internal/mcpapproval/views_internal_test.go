package mcpapproval

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcpapproval/repo"
)

// The database can hand back a timestamptz in whatever location the
// connection negotiated. The API boundary must not leak that offset: the same
// instant has to serialize identically everywhere, so every view normalizes
// to UTC before formatting.
func TestDecisionView_NormalizesTimestampsToUTC(t *testing.T) {
	t.Parallel()

	zone := time.FixedZone("UTC+2", 2*60*60)
	instant := time.Date(2026, 8, 13, 14, 30, 0, 0, zone)

	view := decisionView(repo.McpApprovalDecision{
		Decision:  "approved",
		DecidedBy: "user-1",
		DecidedAt: pgtype.Timestamptz{Time: instant, Valid: true, InfinityModifier: pgtype.Finite},
	})

	require.Equal(t, "2026-08-13T12:30:00Z", view.DecidedAt)
}

func TestOptionalTime_NormalizesToUTC(t *testing.T) {
	t.Parallel()

	zone := time.FixedZone("UTC-5", -5*60*60)
	instant := time.Date(2026, 8, 13, 7, 0, 0, 0, zone)

	formatted := optionalTime(pgtype.Timestamptz{Time: instant, Valid: true, InfinityModifier: pgtype.Finite})
	require.NotNil(t, formatted)
	require.Equal(t, "2026-08-13T12:00:00Z", *formatted)

	require.Nil(t, optionalTime(pgtype.Timestamptz{Time: time.Time{}, Valid: false, InfinityModifier: pgtype.Finite}))
}
