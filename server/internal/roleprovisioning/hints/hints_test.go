package hints

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	pluginsv1 "github.com/speakeasy-api/gram/infra/gen/gram/plugins/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

// Only outbox insertion is supported. A settings/GitHub query or transaction
// lifecycle call fails rather than silently making the emitter state-dependent.
type recordingTx struct {
	pgx.Tx
	args []any
	err  error
}

func (tx *recordingTx) QueryRow(_ context.Context, _ string, args ...any) pgx.Row {
	tx.args = args
	return resultRow{err: tx.err}
}

type resultRow struct{ err error }

func (r resultRow) Scan(...any) error { return r.err }

func TestEmit(t *testing.T) {
	t.Parallel()
	pluginID := uuid.New()
	for _, hint := range []Hint{
		{OrganizationID: "org_fixture"},
		{OrganizationID: "org_fixture", RoleURN: "urn:gram:role:fixture"},
		{OrganizationID: "org_fixture", PluginID: pluginID},
		{OrganizationID: "org_fixture", RoleURN: "urn:gram:role:fixture", PluginID: pluginID},
	} {
		tx := &recordingTx{}
		require.NoError(t, Emit(t.Context(), tx, hint))
		require.Len(t, tx.args, 5)
		require.Equal(t, hint.OrganizationID, tx.args[1])
		body, ok := tx.args[3].([]byte)
		require.True(t, ok)
		event := new(pluginsv1.RoleProvisioningRequested)
		require.NoError(t, proto.Unmarshal(body, event))
		require.Equal(t, hint.OrganizationID, event.GetOrganizationId())
		require.Equal(t, hint.RoleURN, event.GetRoleUrn())
		require.Equal(t, hint.RoleURN != "", event.HasRoleUrn())
		require.Equal(t, hint.PluginID != uuid.Nil, event.HasPluginId())
		if hint.PluginID != uuid.Nil {
			require.Equal(t, hint.PluginID.String(), event.GetPluginId())
		}
	}
}

func TestEmitValidationAndFailure(t *testing.T) {
	t.Parallel()
	require.ErrorContains(t, Emit(t.Context(), nil, Hint{OrganizationID: "org_fixture"}), "transaction")
	tx := &recordingTx{}
	require.ErrorContains(t, Emit(t.Context(), tx, Hint{}), "organization")
	require.Empty(t, tx.args)
	failure := errors.New("insert failed")
	tx.err = failure
	require.ErrorIs(t, Emit(t.Context(), tx, Hint{OrganizationID: "org_fixture"}), failure)
}
