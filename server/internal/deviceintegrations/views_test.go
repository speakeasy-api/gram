package deviceintegrations

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
)

func ts(t time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: t, InfinityModifier: pgtype.Finite, Valid: true}
}

func noTS() pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false}
}

func TestScheduleStatusDerivation(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	base := scheduleState{
		Schedule:            "testmdm_inventory",
		DisabledAt:          noTS(),
		NextPollAfter:       ts(now),
		LastPollSuccessAt:   noTS(),
		LastPollFailedAt:    noTS(),
		LastPollError:       pgtype.Text{String: "", Valid: false},
		ConsecutiveFailures: 0,
		AutoPausedAt:        noTS(),
	}

	pending := base
	require.Equal(t, "pending", scheduleStatus(pending))

	success := base
	success.LastPollSuccessAt = ts(now)
	require.Equal(t, "success", scheduleStatus(success))

	failed := base
	failed.LastPollSuccessAt = ts(now.Add(-time.Hour))
	failed.LastPollFailedAt = ts(now)
	require.Equal(t, "failed", scheduleStatus(failed))

	recovered := base
	recovered.LastPollFailedAt = ts(now.Add(-time.Hour))
	recovered.LastPollSuccessAt = ts(now)
	require.Equal(t, "success", scheduleStatus(recovered))

	// User disable outranks everything, auto-pause outranks sync outcomes.
	disabled := base
	disabled.DisabledAt = ts(now)
	disabled.AutoPausedAt = ts(now)
	require.Equal(t, "disabled", scheduleStatus(disabled))

	autoPaused := base
	autoPaused.AutoPausedAt = ts(now)
	autoPaused.LastPollFailedAt = ts(now)
	require.Equal(t, "auto_paused", scheduleStatus(autoPaused))
}
