package roledistribution_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	roledistributionv1 "github.com/speakeasy-api/gram/infra/gen/gram/role_distribution/v1"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/roledistribution"
	"github.com/speakeasy-api/gram/server/internal/roledistribution/requests"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// Exercise the same transactional boundary used by both staff surfaces. These
// processor-only tests have no Redis client and do not publish cache readback.
func rolloutMutator(t *testing.T, f pipelineFixture) *productfeatures.Mutator {
	t.Helper()
	return productfeatures.NewMutator(productfeatures.NewClient(testenv.NewLogger(t), testenv.NewTracerProvider(t), f.db, nil), audit.NewLogger())
}

func applyRollout(ctx context.Context, m *productfeatures.Mutator, tx pgx.Tx, org string, enabled bool) (bool, error) {
	changed, err := m.ApplyFeatureChangeTx(ctx, tx, org, productfeatures.FeatureAutomaticRoleDistribution, enabled, productfeatures.MutationActor{Principal: urn.NewPrincipal(urn.PrincipalTypeSystem, "rollout-test")})
	if err != nil {
		return false, fmt.Errorf("apply rollout: %w", err)
	}
	return changed, nil
}

func changeRollout(t *testing.T, f pipelineFixture, enabled bool) bool {
	t.Helper()
	ctx := t.Context()
	m := rolloutMutator(t, f)
	conn, release, err := m.LockFeatureChange(ctx, f.event.GetOrganizationId(), productfeatures.FeatureAutomaticRoleDistribution)
	require.NoError(t, err)
	defer release()
	tx, err := conn.Begin(ctx) //nolint:glint // notestingrawsql: Exercise the shared staff transaction boundary and atomic commit.
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	changed, err := applyRollout(ctx, m, tx, f.event.GetOrganizationId(), enabled)
	require.NoError(t, err)
	require.NoError(t, tx.Commit(ctx))
	return changed
}

func TestRollout_BoundedEnableAndResumeSkippedRoles(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	f := newPipelineFixture(t)
	q := testrepo.New(f.db)
	org := f.event.GetOrganizationId()
	require.True(t, changeRollout(t, f, false))
	require.NoError(t, f.handler.HandleRoleDistributionSetupRequested(ctx, f.event, gcp.MessageMetadata{ID: "disabled"}))
	count, err := q.PipelineCountPlugins(ctx, f.project)
	require.NoError(t, err)
	require.Zero(t, count, "disabled delivery must not create plugins")
	require.NoError(t, q.RolloutInsertGlobalRoles(ctx))
	require.NoError(t, q.RolloutInsertLocalRoles(ctx, org))
	before, err := q.PipelineCountRoleDistributionOutbox(ctx)
	require.NoError(t, err)
	require.True(t, changeRollout(t, f, true))
	after, err := q.PipelineCountRoleDistributionOutbox(ctx)
	require.NoError(t, err)
	require.Equal(t, before+1, after, "enable only queues one bounded expansion request")
	require.False(t, changeRollout(t, f, true), "repeated ON is not a reset/retry loop")
	repeated, err := q.PipelineCountRoleDistributionOutbox(ctx)
	require.NoError(t, err)
	require.Equal(t, after, repeated, "repeated ON does not enqueue")
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
		var skipped *roledistributionv1.RoleDistributionSetupRequestedV1
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
						skipped = event
					}
				}
				if event.GetBootstrapOrganizationId() == org {
					require.Greater(t, event.GetCursor(), cursor)
					cursor = event.GetCursor()
					continuation = true
				}
			}
			require.LessOrEqual(t, setups, 100, "each pass emits at most 100 setup requests")
			offset = len(rows)
			if !continuation {
				finished = true
				break
			}
		}
		require.True(t, finished, "bootstrap must finish within the safety bound")
		require.ElementsMatch(t, expected, emitted, "all active local and global identities exactly once, and no deleted roles")
		require.NotNil(t, skipped, "enable must enumerate the previously skipped role")
		return skipped
	}
	firstSkipped := drainPass()
	// Explicit OFF -> ON starts a fresh enumeration, including skipped roles.
	require.True(t, changeRollout(t, f, false))
	require.NoError(t, f.handler.HandleRoleDistributionSetupRequested(ctx, firstSkipped, gcp.MessageMetadata{ID: "disabled-again"}))
	require.True(t, changeRollout(t, f, true))
	skipped := drainPass()
	require.NoError(t, f.handler.HandleRoleDistributionSetupRequested(ctx, skipped, gcp.MessageMetadata{ID: "resumed"}))
	plugin, err := q.PipelineEngineeringPlugin(ctx, f.project)
	require.NoError(t, err)
	principal, err := q.PipelinePluginPrincipal(ctx, plugin)
	require.NoError(t, err)
	require.Equal(t, f.roleURN, principal)
}

func TestRollout_DisablePreservesPluginsAndReenableAssignmentsAreIdempotent(t *testing.T) {
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
	require.True(t, changeRollout(t, f, false))
	require.NoError(t, f.handler.HandleRoleDistributionSetupRequested(ctx, f.event, gcp.MessageMetadata{ID: "disabled"}))
	assertIntact()
	require.True(t, changeRollout(t, f, true))
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
		require.NoError(t, f.handler.HandleRoleDistributionSetupRequested(ctx, event, gcp.MessageMetadata{ID: "reenabled"}))
	}
	require.True(t, found, "fresh enumeration includes previously processed roles")
	require.NoError(t, f.handler.HandleRoleDistributionSetupRequested(ctx, f.event, gcp.MessageMetadata{ID: "duplicate"}))
	assertIntact()
}

func TestRollout_EnableOutboxFailureRollsBackFeatureAndRequest(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	f := newPipelineFixture(t)
	q := testrepo.New(f.db)
	org := f.event.GetOrganizationId()
	require.True(t, changeRollout(t, f, false))
	before, err := q.PipelineCountRoleDistributionOutbox(ctx)
	require.NoError(t, err)
	require.NoError(t, q.RolloutRejectOutbox(ctx))
	m := rolloutMutator(t, f)
	conn, release, err := m.LockFeatureChange(ctx, org, productfeatures.FeatureAutomaticRoleDistribution)
	require.NoError(t, err)
	defer release()
	tx, err := conn.Begin(ctx) //nolint:glint // notestingrawsql: Prove failed outbox publication rolls back the complete staff mutation.
	require.NoError(t, err)
	_, err = applyRollout(ctx, m, tx, org, true)
	var pgErr *pgconn.PgError
	require.ErrorAs(t, err, &pgErr)
	require.Equal(t, "23514", pgErr.Code)
	require.NoError(t, tx.Rollback(ctx))
	enabled, err := q.SourceRoleDistributionSetupEnabled(ctx, org)
	require.NoError(t, err)
	require.False(t, enabled)
	after, err := q.PipelineCountRoleDistributionOutbox(ctx)
	require.NoError(t, err)
	require.Equal(t, before, after)
}

func TestRollout_DisableSerializesWithAttemptBoundary(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	f := newPipelineFixture(t)
	org := f.event.GetOrganizationId()
	m := rolloutMutator(t, f)
	conn, release, err := m.LockFeatureChange(ctx, org, productfeatures.FeatureAutomaticRoleDistribution)
	require.NoError(t, err)
	defer release()
	tx, err := conn.Begin(ctx) //nolint:glint // notestingrawsql: Hold the staff change open to prove setup waits for its decision.
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	changed, err := applyRollout(ctx, m, tx, org, false)
	require.NoError(t, err)
	require.True(t, changed)
	result := make(chan error, 1)
	go func() {
		result <- f.handler.HandleRoleDistributionSetupRequested(ctx, f.event, gcp.MessageMetadata{ID: "concurrent"})
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
	count, err := testrepo.New(f.db).PipelineCountPlugins(ctx, f.project)
	require.NoError(t, err)
	require.Zero(t, count)
}

func TestRollout_EnableSerializesEvenWithoutExistingFlag(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	f := newPipelineFixture(t)
	require.True(t, changeRollout(t, f, false))
	org := f.event.GetOrganizationId()
	tx, err := f.db.Begin(ctx) //nolint:glint // notestingrawsql: Hold the exact attempt-boundary lock while a staff enable is submitted.
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	require.NoError(t, requests.LockOrganization(ctx, tx, org))
	result := make(chan error, 1)
	m := rolloutMutator(t, f)
	go func() {
		conn, release, err := m.LockFeatureChange(ctx, org, productfeatures.FeatureAutomaticRoleDistribution)
		if err != nil {
			result <- err
			return
		}
		defer release()
		write, err := conn.Begin(ctx) //nolint:glint // notestingrawsql: Concurrent staff mutation must wait for the attempt transaction.
		if err != nil {
			result <- err
			return
		}
		defer func() { _ = write.Rollback(context.WithoutCancel(ctx)) }()
		if _, err = applyRollout(ctx, m, write, org, true); err == nil {
			err = write.Commit(ctx)
		}
		result <- err
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
