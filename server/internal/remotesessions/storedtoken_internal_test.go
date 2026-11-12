package remotesessions

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/remotesessionmetrics"
	remotesessions_repo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// A renewal that finds the grant revoked or replaced reads as never linked, not as an unreadable sign-in.
func TestRenewedUnreadableAccess_SessionInactiveIsNeverLinked(t *testing.T) {
	t.Parallel()

	m := &ChallengeManager{refresher: &RefreshService{}}
	var sess remotesessions_repo.RemoteSession
	token, _, _, err := m.renewedUnreadableAccess(t.Context(), sess, RefreshResult{Outcome: remotesessionmetrics.RefreshOutcomeSessionInactive}, nil)
	require.NoError(t, err)
	require.Empty(t, token)
	require.False(t, StoredTokenUnreadable(err))
}

// A store that can't answer the active-row reread keeps its own failure, not an unreadable sign-in.
func TestRefresh_UnreadableSnapshotRereadStoreFailure(t *testing.T) {
	t.Parallel()

	pool, err := pgxpool.New(t.Context(), "postgres://speakeasy@127.0.0.1:1/unreachable?connect_timeout=1")
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	s := &RefreshService{enc: testenv.NewEncryptionClient(t)}
	sess := remotesessions_repo.RemoteSession{
		ID: uuid.New(), SubjectUrn: urn.NewUserSubject("store-down"), RemoteSessionClientID: uuid.New(),
		AccessTokenEncrypted: "not-ciphertext",
	}
	sess.RefreshTokenEncrypted.String, sess.RefreshTokenEncrypted.Valid = "not-ciphertext", true
	sess.UpdatedAt.Time, sess.UpdatedAt.Valid = time.Now(), true
	attempted := sess

	_, _, _, err = s.refresh(t.Context(), remotesessions_repo.New(pool), sess, "", &attempted)
	require.Error(t, err)
	require.False(t, StoredTokenUnreadable(err), "a store outage is not a decrypt failure")
	require.True(t, oops.IsInfrastructureFailure(err), "error: %v", err)
}
