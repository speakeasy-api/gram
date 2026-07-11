package modelkeys_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/model_keys"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/modelkeys"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestDeleteKey_RemovesKey(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	enableCustomModelKeys(t, ctx, ti.conn)

	key, err := ti.service.UpsertKey(ctx, newUpsertPayload(modelkeys.SlotDefault, nil))
	require.NoError(t, err)

	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionModelProviderKeyDelete)
	require.NoError(t, err)

	err = ti.service.DeleteKey(ctx, &gen.DeleteKeyPayload{ID: key.ID, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)

	list, err := ti.service.ListKeys(ctx, &gen.ListKeysPayload{SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	require.Empty(t, list.Keys)

	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionModelProviderKeyDelete)
	require.NoError(t, err)
	require.Equal(t, before+1, after)
}

func TestDeleteKey_NotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	err := ti.service.DeleteKey(ctx, &gen.DeleteKeyPayload{ID: uuid.NewString(), SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestDeleteKey_RequiresProjectWriteScope(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	enableCustomModelKeys(t, ctx, ti.conn)

	key, err := ti.service.UpsertKey(ctx, newUpsertPayload(modelkeys.SlotDefault, nil))
	require.NoError(t, err)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	readOnlyCtx := withExactAccessGrants(t, ctx, ti.conn, authz.NewGrant(authz.ScopeProjectRead, authCtx.ProjectID.String()))

	err = ti.service.DeleteKey(readOnlyCtx, &gen.DeleteKeyPayload{ID: key.ID, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	requireOopsCode(t, err, oops.CodeForbidden)
}
