package roleprovisioning_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/roleprovisioning/repo"
	"github.com/stretchr/testify/require"
)

func TestGlobalRoleLockSharesAcrossOrganizationsAndExcludesWriter(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	const otherOrg = "org_other_role_fixture"
	f.exec(`INSERT INTO organization_metadata (id,name,slug) VALUES ($1,'Other fixture','other-fixture')`, otherOrg)
	roleID := uuid.MustParse(strings.TrimPrefix(f.addRole("", "Shared role"), "role:global:"))
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()

	first, firstPID := f.beginBlocker(ctx)
	second, secondPID := f.beginBlocker(ctx)
	// Match Configure and Reconcile: organization first, then role. Different
	// organizations must retain their role-name snapshots concurrently.
	_, err := repo.New(first).LockOrganization(ctx, f.org)
	require.NoError(t, err)
	_, err = repo.New(second).LockOrganization(ctx, otherOrg)
	require.NoError(t, err)
	name, err := repo.New(first).LockGlobalRole(ctx, roleID)
	require.NoError(t, err)
	require.Equal(t, "Shared role", name)
	_, err = second.Exec(ctx, `SET LOCAL lock_timeout = '1s'`) //nolint:glint // notestingrawsql: bound the regression's incompatible reader lock
	require.NoError(t, err)
	name, err = repo.New(second).LockGlobalRole(ctx, roleID)
	require.NoError(t, err, "a different organization must read the same global role while the first transaction remains open")
	require.Equal(t, "Shared role", name)

	writer, writerPID := f.beginBlocker(ctx)
	written := make(chan error, 1)
	go func() {
		_, err := writer.Exec(ctx, `UPDATE global_roles SET workos_name = 'Renamed role' WHERE id = $1`, roleID) //nolint:glint // notestingrawsql: simulate the IdP's non-key role-name update
		written <- err
	}()
	// Observe actual database wait edges rather than assuming a goroutine has
	// reached its UPDATE after a sleep. FOR KEY SHARE would fail this assertion.
	blockedBy := func(pid int) bool {
		var blocked bool
		err := f.db.QueryRow(ctx, `SELECT $1::integer = ANY(pg_blocking_pids($2::integer))`, pid, writerPID).Scan(&blocked) //nolint:glint // notestingrawsql: observe the writer's real row-lock dependency
		return err == nil && blocked
	}
	// PostgreSQL may wait on one member of a MultiXact at a time rather
	// than report every shared holder in pg_blocking_pids simultaneously.
	require.Eventually(t, func() bool { return blockedBy(firstPID) || blockedBy(secondPID) }, 5*time.Second, 10*time.Millisecond)
	require.NoError(t, first.Commit(ctx))
	require.Eventually(t, func() bool { return blockedBy(secondPID) }, 5*time.Second, 10*time.Millisecond)
	select {
	case err := <-written:
		t.Fatalf("writer finished while the second reader still held its lock: %v", err)
	default:
	}
	require.NoError(t, second.Commit(ctx))
	select {
	case err := <-written:
		require.NoError(t, err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	require.NoError(t, writer.Commit(ctx))
	name, err = repo.New(f.db).LockGlobalRole(ctx, roleID)
	require.NoError(t, err)
	require.Equal(t, "Renamed role", name)
}
