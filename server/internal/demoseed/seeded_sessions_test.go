//go:build demoseed_safety

package demoseed

import (
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/demoseed/fixture/fixtureauth"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	remotesessionsrepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// The seeded Per-User OAuth Server sign-ins read as connected and hold MCP
// fixture tokens that refresh: status and the agent access read both use this
// rule.
func TestSeededSessionsHoldFixtureTokens(t *testing.T) {
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
		require.True(t, sess.RefreshTokenEncrypted.Valid, "%s holds a refresh token", key)
		access, err := enc.Decrypt(sess.AccessTokenEncrypted.String)
		require.NoError(t, err, key)
		_, err = fixtureauth.Parse(access, fixtureauth.KindAccess)
		require.NoError(t, err, "%s holds a fixture access token", key)
	}
}
