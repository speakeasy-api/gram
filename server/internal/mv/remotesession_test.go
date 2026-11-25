package mv

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/stretchr/testify/require"
)

func TestBuildRemoteSessionViewAuthorizationDeadline(t *testing.T) {
	t.Parallel()
	deadline := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	row := repo.RemoteSession{AuthorizationExpiresAt: pgtype.Timestamptz{Time: deadline, Valid: true}}
	view := BuildRemoteSessionView(row, nil, nil)
	require.NotNil(t, view.AuthorizationExpiresAt)
	require.Equal(t, deadline.Format(time.RFC3339), *view.AuthorizationExpiresAt)
}

func TestBuildRemoteSessionViewAbsentAuthorizationDeadline(t *testing.T) {
	t.Parallel()
	view := BuildRemoteSessionView(repo.RemoteSession{}, nil, nil)
	require.Nil(t, view.AuthorizationExpiresAt)
}

func TestBuildRemoteSessionViewAccessExpiry(t *testing.T) {
	t.Parallel()
	expiry := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	row := repo.RemoteSession{AccessExpiresAt: pgtype.Timestamptz{Time: expiry, Valid: true}}
	view := BuildRemoteSessionView(row, nil, nil)
	require.NotNil(t, view.AccessExpiresAt)
	require.Equal(t, expiry.Format(time.RFC3339), *view.AccessExpiresAt)
}

// A non-expiring upstream token is stored as a NULL access expiry. The view
// must leave it out rather than send the zero time (year 0001), which the
// dashboard reads as a connection that ended long ago.
func TestBuildRemoteSessionViewNonExpiringAccessToken(t *testing.T) {
	t.Parallel()
	view := BuildRemoteSessionView(repo.RemoteSession{}, nil, nil)
	require.Nil(t, view.AccessExpiresAt)
}
