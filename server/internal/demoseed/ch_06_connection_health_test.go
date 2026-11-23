package demoseed

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
)

func TestConnectionHealthRowsTellTheSampleStories(t *testing.T) {
	t.Parallel()
	s := &seeder{spec: DefaultSpec(), now: time.Date(2026, 11, 20, 15, 30, 0, 0, time.UTC)}
	rows, err := s.connectionHealthRows()
	require.NoError(t, err)

	again, err := s.connectionHealthRows()
	require.NoError(t, err)
	require.Equal(t, rows, again, "the dataset is deterministic for one clock")

	finals := map[string]int{}
	attempts := map[string]bool{}
	byReason := map[string]int{}
	for _, row := range rows {
		attempts[row.AttemptID.String()] = true
		require.False(t, row.EventTime.After(s.now), "no row is in the future")
		require.False(t, row.EventTime.Before(s.now.Add(-30*24*time.Hour)), "every row is inside retention")
		if row.IsFinal {
			finals[row.Outcome]++
			byReason[row.Reason]++
		}
	}
	t.Logf("rows=%d attempts=%d finals=%v", len(rows), len(attempts), finals)
	require.Greater(t, len(attempts), 2000)
	require.Less(t, len(attempts), 5000)
	for _, outcome := range []string{"connected", "failed", "blocked", "declined"} {
		require.Positive(t, finals[outcome], outcome)
	}
	// The suspected problems' counts.
	require.Equal(t, 87, byReason[mcpconnectionattempts.CodeRefreshTokenUnknownOrAlreadyUsed.String()])
	require.Equal(t, 16, byReason[mcpconnectionattempts.CodeStaticKeyRejected.String()])
	require.Equal(t, 1, byReason[mcpconnectionattempts.CodeRemoteSessionUnavailable.String()])
}

func TestConnectionHealthSessionsCoverEveryState(t *testing.T) {
	t.Parallel()
	s := &seeder{spec: DefaultSpec(), now: time.Date(2026, 11, 20, 15, 30, 0, 0, time.UTC)}
	var live, ended, expired, unused int
	for _, session := range s.healthSessions() {
		switch {
		case session.deleted != nil:
			ended++
		case session.refreshExpires.Before(s.now):
			expired++
		case session.lastUsed.Before(s.now.Add(-7 * 24 * time.Hour)):
			unused++
		default:
			live++
		}
	}
	require.Equal(t, 2, expired, "two people need to sign in again")
	require.Equal(t, 2, unused, "two people have not used their sign-in for a week")
	require.Greater(t, live, 15)
	require.Greater(t, ended, 20)
}
