package authz

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	pluginsv1 "github.com/speakeasy-api/gram/infra/gen/gram/plugins/v1"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

func seededGlobalRoleHints(t *testing.T, ctx context.Context, db accessrepo.DBTX) []string {
	t.Helper()
	rows, err := testrepo.New(db).ListPublishOutboxRows(ctx)
	require.NoError(t, err)
	var urns []string
	for _, row := range rows {
		if row.Topic != "gram.plugins.v1.RoleProvisioningRequested" {
			continue
		}
		require.Empty(t, row.OrganizationID)
		hint := new(pluginsv1.RoleProvisioningRequested)
		require.NoError(t, proto.Unmarshal(row.Message, hint))
		require.Empty(t, hint.GetOrganizationId())
		require.Empty(t, hint.GetRoleUrn())
		urns = append(urns, hint.GetGlobalRoleUrn())
	}
	return urns
}

func TestSeedSystemRoleGrantsHintsOnlyForMutations(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	conn := newTestDB(t)
	seedOrganization(t, ctx, conn, "org_seed_hint_first")
	seedOrganization(t, ctx, conn, "org_seed_hint_second")
	require.NoError(t, SeedSystemRoleGrants(ctx, conn, "org_seed_hint_first"))
	expected := make([]string, 0, 2)
	for _, slug := range []string{SystemRoleAdmin, SystemRoleMember} {
		role, err := accessrepo.New(conn).GetGlobalRoleBySlug(ctx, slug)
		require.NoError(t, err)
		expected = append(expected, "role:global:"+role.ID.String())
	}
	require.ElementsMatch(t, expected, seededGlobalRoleHints(t, ctx, conn))
	// A different organization's routine grant initialization must not fan out.
	require.NoError(t, SeedSystemRoleGrants(ctx, conn, "org_seed_hint_second"))
	require.NoError(t, SeedSystemRoleGrants(ctx, conn, "org_seed_hint_first"))
	require.ElementsMatch(t, expected, seededGlobalRoleHints(t, ctx, conn))
}

func TestSeedSystemRoleGrantsHintsRollbackWithSourceInsert(t *testing.T) {
	t.Parallel()
	testSeedSystemRoleGrantsHintsRollbackWithSource(t, false)
}

func TestSeedSystemRoleGrantsHintsRollbackWithSourceReactivate(t *testing.T) {
	t.Parallel()
	testSeedSystemRoleGrantsHintsRollbackWithSource(t, true)
}

func testSeedSystemRoleGrantsHintsRollbackWithSource(t *testing.T, reactivate bool) {
	t.Helper()
	ctx := t.Context()
	conn := newTestDB(t)
	org := "org_seed_hint_rollback"
	seedOrganization(t, ctx, conn, org)
	var baseline []string
	var deletedURN string
	if reactivate {
		require.NoError(t, SeedSystemRoleGrants(ctx, conn, org))
		baseline = seededGlobalRoleHints(t, ctx, conn)
		role, err := accessrepo.New(conn).GetGlobalRoleBySlug(ctx, SystemRoleAdmin)
		require.NoError(t, err)
		deletedURN = "role:global:" + role.ID.String()
		_, err = accessrepo.New(conn).MarkGlobalRoleDeleted(ctx, accessrepo.MarkGlobalRoleDeletedParams{WorkosSlug: SystemRoleAdmin, WorkosDeletedAt: conv.ToPGTimestamptz(time.Now()), WorkosLastEventID: conv.ToPGText("event_seed_delete")})
		require.NoError(t, err)
	}
	tx, err := conn.Begin(ctx) //nolint:glint // notestingrawsql: Caller-owned transaction is required to prove source and hint rollback.
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(ctx) }()
	require.NoError(t, SeedSystemRoleGrantsTx(ctx, tx, org))
	pending := seededGlobalRoleHints(t, ctx, tx)
	if reactivate {
		require.Len(t, pending, len(baseline)+1)
		require.Equal(t, deletedURN, pending[len(pending)-1])
		role, err := accessrepo.New(tx).GetGlobalRoleBySlug(ctx, SystemRoleAdmin)
		require.NoError(t, err)
		require.False(t, role.Deleted)
		require.Equal(t, "event_seed_delete", role.WorkosLastEventID.String)
	} else {
		require.Len(t, pending, 2)
	}
	require.NoError(t, tx.Rollback(ctx))
	require.Equal(t, baseline, seededGlobalRoleHints(t, ctx, conn))
	role, err := accessrepo.New(conn).GetGlobalRoleBySlug(ctx, SystemRoleAdmin)
	if reactivate {
		require.NoError(t, err)
		require.True(t, role.Deleted)
		require.NoError(t, SeedSystemRoleGrants(ctx, conn, org))
		committed := seededGlobalRoleHints(t, ctx, conn)
		require.Len(t, committed, len(baseline)+1)
		require.Equal(t, deletedURN, committed[len(committed)-1])
	} else {
		require.ErrorIs(t, err, pgx.ErrNoRows)
	}
}

func TestSeedSystemRoleGrantsHintFailureRollsBack(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	conn := newTestDB(t)
	org := "org_seed_hint_failure"
	seedOrganization(t, ctx, conn, org)
	_, err := conn.Exec(ctx, `ALTER TABLE publish_outbox ADD CONSTRAINT reject_seed_hint CHECK (topic <> 'gram.plugins.v1.RoleProvisioningRequested') NOT VALID`) //nolint:glint // notestingrawsql: Test-only DDL injects an outbox write failure; SQLc cannot express this constraint.
	require.NoError(t, err)
	require.Error(t, SeedSystemRoleGrants(ctx, conn, org))
	require.Empty(t, seededGlobalRoleHints(t, ctx, conn))
	for _, slug := range []string{SystemRoleAdmin, SystemRoleMember} {
		_, err := accessrepo.New(conn).GetGlobalRoleBySlug(ctx, slug)
		require.ErrorIs(t, err, pgx.ErrNoRows)
	}
}

func TestSeedSystemRoleGrantsConcurrentHints(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	conn := newTestDB(t)
	orgs := []string{"org_seed_concurrent_first", "org_seed_concurrent_second"}
	for _, org := range orgs {
		seedOrganization(t, ctx, conn, org)
	}
	var wg sync.WaitGroup
	failures := make(chan error, len(orgs))
	for _, org := range orgs {
		wg.Go(func() { failures <- SeedSystemRoleGrants(ctx, conn, org) })
	}
	wg.Wait()
	close(failures)
	for err := range failures {
		require.NoError(t, err)
	}
	require.Len(t, seededGlobalRoleHints(t, ctx, conn), 2)
}
