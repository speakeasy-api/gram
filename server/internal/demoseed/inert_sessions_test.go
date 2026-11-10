//go:build demoseed_safety

package demoseed

import (
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	remotesessionsrepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// The inert demo sessions read as connected: status and the agent access read both use this rule.
func TestInertDemoSessionsReadAsConnected(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	db, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)
	spec := DefaultSpec()
	seedLocalPostgres(ctx, t, db, spec)

	s := &seeder{spec: spec}
	// Validated sessions 3 to 6: they share the Per-User OAuth Server's one
	// client, and each demo user can hold one session per client.
	keys := []string{"attachment-session"}
	for n := 3; n <= 6; n++ {
		keys = append(keys, "validated-session-"+strconv.Itoa(n))
	}
	enc := testenv.NewEncryptionClient(t)
	now := time.Now()
	for _, key := range keys {
		sess, err := remotesessionsrepo.New(db).GetRemoteSessionByIDIncludingDeleted(ctx, remotesessionsrepo.GetRemoteSessionByIDIncludingDeletedParams{ID: s.det(key), ProjectID: uuid.NullUUID{UUID: s.projectID(), Valid: true}})
		require.NoError(t, err, key)
		require.True(t, remotesessions.StoredSignInUsable(enc, sess, now), "%s reads as connected", key)
		require.False(t, sess.RefreshTokenEncrypted.Valid, "%s stays inert: no refresh token", key)
		require.False(t, sess.RefreshExpiresAt.Valid, "%s holds no refresh token, so it states no refresh deadline", key)
	}

	// The idle re-check never presents the dummy token to an upstream.
	due, err := remotesessionsrepo.New(db).ClaimDueRemoteSessionRecheckCandidates(ctx, remotesessionsrepo.ClaimDueRemoteSessionRecheckCandidatesParams{
		NowTs: conv.ToPGTimestamptz(now), RecheckCutoff: conv.ToPGTimestamptz(now.Add(time.Hour)),
		AttemptCutoff: conv.ToPGTimestamptz(now.Add(time.Hour)), ExcludedHosts: nil, LimitValue: 1000,
	})
	require.NoError(t, err)
	for _, key := range keys {
		require.False(t, slices.ContainsFunc(due, func(row remotesessionsrepo.ClaimDueRemoteSessionRecheckCandidatesRow) bool {
			return row.ID == s.det(key)
		}), "%s is never re-checked", key)
	}
}
