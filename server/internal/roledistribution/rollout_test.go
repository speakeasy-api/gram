package roledistribution_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	roledistributionv1 "github.com/speakeasy-api/gram/infra/gen/gram/role_distribution/v1"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/roledistribution"
	"github.com/speakeasy-api/gram/server/internal/roledistribution/requests"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

func TestOrganizationBootstrap_WithoutFlagPaginatesAndReplays(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	f := newPipelineFixture(t)
	q := testrepo.New(f.db)
	org := f.event.GetOrganizationId()
	require.NoError(t, q.RolloutInsertGlobalRoles(ctx))
	require.NoError(t, q.RolloutInsertLocalRoles(ctx, org))
	expected, err := q.RolloutActiveRoles(ctx, org)
	require.NoError(t, err)
	require.Greater(t, len(expected), 100, "fixture must exercise pagination")
	drainPass := func() *roledistributionv1.RoleDistributionSetupRequestedV1 {
		t.Helper()
		baseline, err := q.ListPublishOutboxRows(ctx)
		require.NoError(t, err)
		offset := len(baseline)
		cursor := ""
		emitted := []string{}
		var roleEvent *roledistributionv1.RoleDistributionSetupRequestedV1
		finished := false
		for range 20 {
			require.NoError(t, roledistribution.ProcessOrganizationBootstrap(ctx, f.db, org, cursor))
			rows, err := q.ListPublishOutboxRows(ctx)
			require.NoError(t, err)
			setups, continuation := 0, false
			for _, row := range rows[offset:] {
				if row.Topic != "gram.role_distribution.v1.RoleDistributionSetupRequestedV1" {
					continue
				}
				event := &roledistributionv1.RoleDistributionSetupRequestedV1{}
				require.NoError(t, proto.Unmarshal(row.Message, event))
				if event.GetRoleUrn() != "" {
					setups++
					require.Equal(t, org, event.GetOrganizationId())
					emitted = append(emitted, event.GetRoleUrn())
					if event.GetRoleUrn() == f.roleURN {
						roleEvent = event
					}
				}
				if event.GetBootstrapOrganizationId() == org {
					require.Greater(t, event.GetCursor(), cursor)
					cursor = event.GetCursor()
					continuation = true
				}
			}
			require.LessOrEqual(t, setups, 100, "each pass emits at most 100 setup requests")
			if continuation {
				require.Equal(t, 100, setups, "continuation follows a full page")
			}
			offset = len(rows)
			if !continuation {
				finished = true
				break
			}
		}
		require.True(t, finished, "bootstrap must finish within the safety bound")
		require.ElementsMatch(t, expected, emitted, "all active local and global identities exactly once, and no deleted roles")
		require.NotNil(t, roleEvent, "bootstrap must enumerate the source role")
		return roleEvent
	}
	_ = drainPass()
	// A replay starts a fresh bounded enumeration without completion tracking.
	roleEvent := drainPass()
	require.NoError(t, f.handler.HandleRoleDistributionSetupRequested(ctx, roleEvent, gcp.MessageMetadata{ID: "bootstrap"}))
	plugin, err := q.PipelineEngineeringPlugin(ctx, f.project)
	require.NoError(t, err)
	principal, err := q.PipelinePluginPrincipal(ctx, plugin)
	require.NoError(t, err)
	require.Equal(t, f.roleURN, principal)
}

func TestOrganizationBootstrap_ReplayPreservesEditsAndAssignments(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	f := newPipelineFixture(t)
	q := testrepo.New(f.db)
	org := f.event.GetOrganizationId()
	require.NoError(t, f.handler.HandleRoleDistributionSetupRequested(ctx, f.event, gcp.MessageMetadata{ID: "complete"}))
	plugin, err := q.PipelineEngineeringPlugin(ctx, f.project)
	require.NoError(t, err)
	require.NoError(t, q.PipelineRenamePlugin(ctx, testrepo.PipelineRenamePluginParams{ID: plugin, ProjectID: f.project}))
	assertIntact := func() {
		t.Helper()
		edited, err := pluginsrepo.New(f.db).GetPlugin(ctx, pluginsrepo.GetPluginParams{ID: plugin, OrganizationID: org, ProjectID: f.project})
		require.NoError(t, err)
		require.Equal(t, "Administrator edit", edited.Name)
		count, err := q.PipelineCountPlugins(ctx, f.project)
		require.NoError(t, err)
		require.EqualValues(t, 1, count)
		assignments, err := q.PipelineCountPluginAssignments(ctx, plugin)
		require.NoError(t, err)
		require.EqualValues(t, 1, assignments)
	}
	baseline, err := q.ListPublishOutboxRows(ctx)
	require.NoError(t, err)
	require.NoError(t, roledistribution.ProcessOrganizationBootstrap(ctx, f.db, org, ""))
	rows, err := q.ListPublishOutboxRows(ctx)
	require.NoError(t, err)
	found := false
	for _, row := range rows[len(baseline):] {
		if row.Topic != "gram.role_distribution.v1.RoleDistributionSetupRequestedV1" {
			continue
		}
		event := &roledistributionv1.RoleDistributionSetupRequestedV1{}
		require.NoError(t, proto.Unmarshal(row.Message, event))
		if event.GetRoleUrn() != f.roleURN {
			continue
		}
		found = true
		require.NoError(t, f.handler.HandleRoleDistributionSetupRequested(ctx, event, gcp.MessageMetadata{ID: "replayed"}))
	}
	require.True(t, found, "fresh enumeration includes previously processed roles")
	require.NoError(t, f.handler.HandleRoleDistributionSetupRequested(ctx, f.event, gcp.MessageMetadata{ID: "duplicate"}))
	assertIntact()
}

func TestOrganizationBootstrap_OutboxFailureRollsBackPage(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	f := newPipelineFixture(t)
	q := testrepo.New(f.db)
	require.NoError(t, q.RolloutInsertGlobalRoles(ctx))
	before, err := q.PipelineCountRoleDistributionOutbox(ctx)
	require.NoError(t, err)
	require.NoError(t, q.RolloutRejectOutbox(ctx))
	err = roledistribution.ProcessOrganizationBootstrap(ctx, f.db, f.event.GetOrganizationId(), "")
	var pgErr *pgconn.PgError
	require.ErrorAs(t, err, &pgErr)
	require.Equal(t, "23514", pgErr.Code)
	require.ErrorContains(t, err, "injected bootstrap continuation failure")
	after, err := q.PipelineCountRoleDistributionOutbox(ctx)
	require.NoError(t, err)
	require.Equal(t, before, after, "failed continuation must roll back the entire page")
}

func TestOrganizationBootstrap_SerializesWithSetup(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	f := newPipelineFixture(t)
	org := f.event.GetOrganizationId()
	tx, err := f.db.Begin(ctx) //nolint:glint // notestingrawsql: Hold the organization lock to prove bootstrap waits for setup.
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	require.NoError(t, requests.LockOrganization(ctx, tx, org))
	result := make(chan error, 1)
	go func() {
		result <- roledistribution.ProcessOrganizationBootstrap(ctx, f.db, org, "")
	}()
	require.Eventually(t, func() bool {
		blocked, err := testrepo.New(f.db).RolloutBlockedBackends(ctx, int32(tx.Conn().PgConn().PID()))
		return err == nil && len(blocked) > 0
	}, 5*time.Second, 10*time.Millisecond, "operation must wait on the held transaction")
	require.NoError(t, tx.Commit(ctx))
	select {
	case err := <-result:
		require.NoError(t, err)
	case <-ctx.Done():
		t.Fatal("operation did not finish:", ctx.Err())
	}
}
