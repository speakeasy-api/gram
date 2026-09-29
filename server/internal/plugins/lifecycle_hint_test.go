package plugins_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/plugins"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

func TestDeletePluginDurableLifecycleHintRollsBack(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestPluginsService(t)
	created, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{SessionToken: nil, ProjectSlugInput: nil, Name: "Lifecycle fixture", Slug: nil, Description: nil})
	require.NoError(t, err)
	beforeRows, readErr := testrepo.New(ti.conn).ListPublishOutboxRows(ctx)
	require.NoError(t, readErr)
	var before int
	for _, row := range beforeRows {
		if row.Topic == "gram.plugins.v1.RoleProvisioningRequested" {
			before++
		}
	}
	_, err = ti.conn.Exec(ctx, `CREATE FUNCTION reject_lifecycle_hint() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.topic='gram.plugins.v1.RoleProvisioningRequested' THEN RAISE EXCEPTION 'injected hint failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER reject_lifecycle_hint BEFORE INSERT ON publish_outbox FOR EACH ROW EXECUTE FUNCTION reject_lifecycle_hint()`) //nolint:glint // notestingrawsql: failure injection
	require.NoError(t, err)
	err = ti.service.DeletePlugin(ctx, &gen.DeletePluginPayload{ID: created.ID, SessionToken: nil, ProjectSlugInput: nil})
	require.Error(t, err)
	_, err = ti.service.GetPlugin(ctx, &gen.GetPluginPayload{ID: created.ID, SessionToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	afterRows, readErr := testrepo.New(ti.conn).ListPublishOutboxRows(ctx)
	require.NoError(t, readErr)
	var after int
	for _, row := range afterRows {
		if row.Topic == "gram.plugins.v1.RoleProvisioningRequested" {
			after++
		}
	}
	require.Equal(t, before, after)
}
