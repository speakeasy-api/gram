package demoseed

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestConnectionHealthRowsTellTheSampleStories(t *testing.T) {
	t.Parallel()
	s := &seeder{spec: DefaultSpec(), now: time.Date(2026, 11, 20, 15, 30, 0, 0, time.UTC)}
	rows, served, err := s.connectionHealthRows()
	require.NoError(t, err)

	again, servedAgain, err := s.connectionHealthRows()
	require.NoError(t, err)
	require.Equal(t, rows, again, "the dataset is deterministic for one clock")
	require.Equal(t, served, servedAgain)
	require.NotEmpty(t, served)
	for _, r := range served {
		require.Equal(t, r.ServedAt, r.AnsweredAt, "every seeded served row is an answered request")
		require.False(t, r.ServedAt.After(s.now))
	}
	direct := 0
	for _, r := range served {
		if r.ServerKind == "mcp" {
			direct++
			require.Empty(t, r.MemberID, "a direct server has no gateway member")
		}
	}
	require.Positive(t, direct)
	// Renewals and sign-ins are not served requests: the killswitched person only renews, so has none.
	paused := urn.NewUserSubject(s.userID("amara")).String()
	renewed := false
	for _, r := range rows {
		renewed = renewed || (r.SubjectURN == paused && r.Outcome == string(mcpconnectionattempts.OutcomeConnected))
	}
	require.True(t, renewed)
	for _, r := range served {
		require.NotEqual(t, paused, r.SubjectURN, "no served row comes from a sign-in or renewal")
	}

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
