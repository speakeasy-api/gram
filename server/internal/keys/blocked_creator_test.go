package keys_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	keysrepo "github.com/speakeasy-api/gram/server/internal/keys/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/userblock"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

func TestKeyBasedAuth_RejectsKeyOfBlockedCreator(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestKeysService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	rawKey := "gram_local_" + uuid.NewString()
	keyHash, err := auth.GetAPIKeyHash(rawKey)
	require.NoError(t, err)
	_, err = keysrepo.New(ti.conn).CreateAPIKey(ctx, keysrepo.CreateAPIKeyParams{
		OrganizationID:  authCtx.ActiveOrganizationID,
		ProjectID:       uuid.NullUUID{UUID: *authCtx.ProjectID, Valid: true},
		CreatedByUserID: authCtx.UserID,
		Name:            "blocked-creator-key",
		KeyPrefix:       rawKey[:16],
		KeyHash:         keyHash,
		Scopes:          []string{"consumer"},
	})
	require.NoError(t, err)

	require.NoError(t, usersrepo.New(ti.conn).SetUserBlacklisted(ctx, usersrepo.SetUserBlacklistedParams{
		Blacklisted: true,
		ID:          authCtx.UserID,
	}))
	userblock.Forget(ti.conn)

	_, err = ti.keyAuth.KeyBasedAuth(ctx, rawKey, []string{"consumer"})
	require.Error(t, err)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeUnauthorized, oopsErr.Code)
}
