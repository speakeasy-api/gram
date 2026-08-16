package platformmcp

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
)

func TestConnectionAuthStateMapsPersistedReasons(t *testing.T) {
	t.Parallel()

	for internal, public := range map[string]string{
		"refresh_idle_expired":  "idle_expired",
		"authorization_expired": "authorization_expired",
		"refresh_reuse":         "refresh_invalidated",
		"authorization_lost":    "authorization_changed",
		"connection_revoked":    "revoked",
		"client_revoked":        "revoked",
		"security_reset":        "security_reset",
	} {
		t.Run(internal, func(t *testing.T) {
			t.Parallel()
			state, reason := connectionAuthState(platformrepo.GetPlatformMCPSubjectConnectionAuthStateRow{
				ReauthorizationReason: pgtype.Text{String: internal, Valid: true},
			}, time.Now())
			require.Equal(t, ConnectionAuthStateReauthorizationRequired, state)
			require.Equal(t, public, reason)
		})
	}
}

func TestConnectionAuthStateDerivesExactDeadlines(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 16, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name   string
		row    platformrepo.GetPlatformMCPSubjectConnectionAuthStateRow
		reason string
	}{
		{
			name: "absolute",
			row: platformrepo.GetPlatformMCPSubjectConnectionAuthStateRow{
				EffectiveAuthorizationExpiresAt: pgtype.Timestamptz{Time: now, Valid: true},
			},
			reason: "authorization_expired",
		},
		{
			name: "idle",
			row: platformrepo.GetPlatformMCPSubjectConnectionAuthStateRow{
				EffectiveAuthorizationExpiresAt: pgtype.Timestamptz{Time: now.Add(time.Hour), Valid: true},
				LatestRefreshExpiresAt:          pgtype.Timestamptz{Time: now, Valid: true},
			},
			reason: "idle_expired",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			state, reason := connectionAuthState(tc.row, now)
			require.Equal(t, ConnectionAuthStateReauthorizationRequired, state)
			require.Equal(t, tc.reason, reason)
		})
	}
}
