package keys_test

import (
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/keys"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	keysrepo "github.com/speakeasy-api/gram/server/internal/keys/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestRepairOrphanedAPIKeyCreators_RewritesPlaceholderCreator(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestKeysService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	rawKey := "gram_local_" + uuid.NewString()
	keyHash, err := auth.GetAPIKeyHash(rawKey)
	require.NoError(t, err)

	created, err := keysrepo.New(ti.conn).CreateAPIKey(ctx, keysrepo.CreateAPIKeyParams{
		OrganizationID:  authCtx.ActiveOrganizationID,
		ProjectID:       uuid.NullUUID{UUID: *authCtx.ProjectID, Valid: true},
		CreatedByUserID: "system",
		Name:            "plugins-hooks-20260708-120102-abc123",
		KeyPrefix:       rawKey[:16],
		KeyHash:         keyHash,
		Scopes:          []string{"hooks"},
	})
	require.NoError(t, err)
	require.Equal(t, "system", created.CreatedByUserID)

	_, err = keysrepo.New(ti.conn).GetAPIKeyByKeyHash(ctx, keyHash)
	require.ErrorIs(t, err, pgx.ErrNoRows)

	_, err = ti.keyAuth.KeyBasedAuth(ctx, rawKey, []string{"hooks"})
	require.Error(t, err)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeUnauthorized, oopsErr.Code)
	require.Contains(t, oopsErr.Error(), "api key not found")

	// The Keys page deliberately keeps listing the key while it is unusable --
	// hiding it would leave the owner with a row they can neither inspect nor
	// delete -- so it must be visible both before and after the repair.
	listed, err := ti.service.ListKeys(ctx, &gen.ListKeysPayload{SessionToken: nil})
	require.NoError(t, err)
	require.True(t, slices.ContainsFunc(listed.Keys, func(key *gen.Key) bool {
		return key.ID == created.ID.String()
	}), "unrepaired key must still appear on the Keys page")

	repaired, err := keysrepo.New(ti.conn).RepairOrphanedAPIKeyCreators(ctx)
	require.NoError(t, err)
	require.Equal(t, int64(1), repaired)

	resolved, err := keysrepo.New(ti.conn).GetAPIKeyByKeyHash(ctx, keyHash)
	require.NoError(t, err)
	require.Equal(t, created.ID, resolved.ID)
	require.Equal(t, authCtx.UserID, resolved.CreatedByUserID)

	_, err = ti.keyAuth.KeyBasedAuth(ctx, rawKey, []string{"hooks"})
	require.NoError(t, err)

	listed, err = ti.service.ListKeys(ctx, &gen.ListKeysPayload{SessionToken: nil})
	require.NoError(t, err)
	found := false
	for _, key := range listed.Keys {
		if key.ID == created.ID.String() {
			found = true
			require.Equal(t, authCtx.UserID, key.CreatedByUserID)
		}
	}
	require.True(t, found, "repaired key must appear on the Keys page")
}

func TestRepairOrphanedAPIKeyCreators_IsIdempotent(t *testing.T) {
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
		CreatedByUserID: "system",
		Name:            "plugins-mcp-20260708-120102-def456",
		KeyPrefix:       rawKey[:16],
		KeyHash:         keyHash,
		Scopes:          []string{"consumer"},
	})
	require.NoError(t, err)

	first, err := keysrepo.New(ti.conn).RepairOrphanedAPIKeyCreators(ctx)
	require.NoError(t, err)
	require.Equal(t, int64(1), first)

	second, err := keysrepo.New(ti.conn).RepairOrphanedAPIKeyCreators(ctx)
	require.NoError(t, err)
	require.Equal(t, int64(0), second)
}
