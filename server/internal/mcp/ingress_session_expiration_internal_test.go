package mcp

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

func TestIngressSessionExpiration(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 27, 12, 0, 0, 0, time.UTC)
	interval := func(d time.Duration) pgtype.Interval {
		return pgtype.Interval{Microseconds: d.Microseconds(), Valid: true}
	}
	timestamp := func(at time.Time) pgtype.Timestamptz {
		return pgtype.Timestamptz{Time: at, Valid: true}
	}

	tests := []struct {
		name       string
		session    usersessionsrepo.UserSession
		wantExpiry time.Time
		wantReason string
	}{
		{
			name: "active",
			session: usersessionsrepo.UserSession{
				CreatedAt:                timestamp(now.Add(-2 * time.Hour)),
				LastUsedAt:               timestamp(now.Add(-5 * time.Minute)),
				RefreshExpiresAt:         timestamp(now.Add(24 * time.Hour)),
				SessionAbsoluteExpiresAt: timestamp(now.Add(8 * time.Hour)),
				SessionInactivityTimeout: interval(30 * time.Minute),
			},
			wantExpiry: now.Add(25 * time.Minute),
		},
		{
			name: "inactive from last use",
			session: usersessionsrepo.UserSession{
				CreatedAt:                timestamp(now.Add(-2 * time.Hour)),
				LastUsedAt:               timestamp(now.Add(-31 * time.Minute)),
				RefreshExpiresAt:         timestamp(now.Add(24 * time.Hour)),
				SessionAbsoluteExpiresAt: timestamp(now.Add(8 * time.Hour)),
				SessionInactivityTimeout: interval(30 * time.Minute),
			},
			wantExpiry: now.Add(8 * time.Hour),
			wantReason: "session_inactivity_expired",
		},
		{
			name: "inactive before first use",
			session: usersessionsrepo.UserSession{
				CreatedAt:                timestamp(now.Add(-31 * time.Minute)),
				RefreshExpiresAt:         timestamp(now.Add(24 * time.Hour)),
				SessionAbsoluteExpiresAt: timestamp(now.Add(8 * time.Hour)),
				SessionInactivityTimeout: interval(30 * time.Minute),
			},
			wantExpiry: now.Add(8 * time.Hour),
			wantReason: "session_inactivity_expired",
		},
		{
			name: "absolute duration elapsed",
			session: usersessionsrepo.UserSession{
				CreatedAt:                timestamp(now.Add(-time.Hour)),
				LastUsedAt:               timestamp(now.Add(-time.Minute)),
				RefreshExpiresAt:         timestamp(now.Add(24 * time.Hour)),
				SessionAbsoluteExpiresAt: timestamp(now),
				SessionInactivityTimeout: interval(30 * time.Minute),
			},
			wantExpiry: now,
			wantReason: "refresh_token_expired",
		},
		{
			name: "legacy refresh deadline",
			session: usersessionsrepo.UserSession{
				CreatedAt:        timestamp(now.Add(-time.Hour)),
				RefreshExpiresAt: timestamp(now.Add(2 * time.Hour)),
			},
			wantExpiry: now.Add(2 * time.Hour),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			expiresAt, reason, description := ingressSessionExpiration(tt.session, now)
			require.Equal(t, tt.wantExpiry, expiresAt)
			require.Equal(t, tt.wantReason, reason)
			if tt.wantReason == "" {
				require.Empty(t, description)
			} else {
				require.NotEmpty(t, description)
			}
		})
	}
}
