// The issuer migration re-points clients without touching their bindings, so
// only its own resync updates the servers behind them. It must read that set
// before taking the source's client-binding lock, which leaves a window a
// concurrent attach can commit inside. These tests drive the window rather than
// racing for it: the migration is parked on a lock the test holds while the
// test performs the attach, so the interleaving is fixed.

package remotesessions_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// migrationWindow is everything the two tests below share: a source issuer with
// one client bound to one user session issuer, a second user session issuer
// that is not bound yet, and a server behind each.
type migrationWindow struct {
	projectID      uuid.UUID
	organizationID string

	sourceID uuid.UUID
	targetID uuid.UUID

	earlyIssuerID uuid.UUID
	lateIssuerID  uuid.UUID
	earlyServerID uuid.UUID
	lateServerID  uuid.UUID

	clientID uuid.UUID
}

func newMigrationWindow(t *testing.T, ctx context.Context, ti *testInstance, prefix string) migrationWindow {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	w := migrationWindow{
		projectID:      *authCtx.ProjectID,
		organizationID: authCtx.ActiveOrganizationID,
		sourceID:       uuid.Nil,
		targetID:       uuid.Nil,
		earlyIssuerID:  uuid.Nil,
		lateIssuerID:   uuid.Nil,
		earlyServerID:  uuid.Nil,
		lateServerID:   uuid.Nil,
		clientID:       uuid.Nil,
	}

	// Both org-level and identically spelled, so the scope ladder and the
	// endpoint parity guard both pass and the window is the only thing under
	// test.
	w.sourceID = seedOrgLevelRemoteIssuer(t, ctx, ti.conn, w.organizationID, prefix+"-source")
	w.targetID = seedOrgLevelRemoteIssuer(t, ctx, ti.conn, w.organizationID, prefix+"-target")

	w.earlyIssuerID = createUserSessionIssuerInProject(t, ctx, ti.conn, w.projectID, "usi-"+prefix+"-early")
	w.lateIssuerID = createUserSessionIssuerInProject(t, ctx, ti.conn, w.projectID, "usi-"+prefix+"-late")
	w.earlyServerID = seedServerOnIssuer(t, ctx, ti.conn, w.projectID, w.earlyIssuerID, prefix+"-early")
	w.lateServerID = seedServerOnIssuer(t, ctx, ti.conn, w.projectID, w.lateIssuerID, prefix+"-late")

	w.clientID = seedOrgLevelRemoteClient(t, ctx, ti.conn, w.organizationID, w.sourceID, "cid-"+prefix, w.earlyIssuerID)
	resync(t, ctx, ti.conn, remotesessions.ProjectResyncScope(w.organizationID, w.projectID), w.earlyIssuerID)
	require.Equal(t, uuid.NullUUID{UUID: w.sourceID, Valid: true}, storedIssuer(t, ctx, ti.conn, w.projectID, w.earlyServerID))

	return w
}

// attachInsideWindow is the concurrent writer, replayed where the window opens:
// it binds the unmigrated client to an issuer the early read missed and
// recomputes it as its handler would — to the source, which is what the client
// points at until the re-point lands.
func (w migrationWindow) attachInsideWindow(t *testing.T, ctx context.Context, ti *testInstance) {
	t.Helper()

	require.NoError(t, repo.New(ti.conn).AttachRemoteSessionClientToUserSessionIssuer(ctx, repo.AttachRemoteSessionClientToUserSessionIssuerParams{
		RemoteSessionClientID: w.clientID,
		UserSessionIssuerID:   w.lateIssuerID,
	}))
	resync(t, ctx, ti.conn, remotesessions.ProjectResyncScope(w.organizationID, w.projectID), w.lateIssuerID)
	require.Equal(t, uuid.NullUUID{UUID: w.sourceID, Valid: true}, storedIssuer(t, ctx, ti.conn, w.projectID, w.lateServerID),
		"the racing writer stamps the source, which is still what its client points at")
}

// waitForDerivationLock blocks until userIssuerID's derivation lock is held
// elsewhere, meaning the migration finished its early read and is heading for
// the issuer lock the test holds. Probing with the non-blocking form makes that
// a fact; a sleep would only show the migration had not finished.
func waitForDerivationLock(t *testing.T, ctx context.Context, conn *pgxpool.Pool, userIssuerID uuid.UUID) {
	t.Helper()

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		probe := testenv.BeginTx(t, ctx, conn)
		acquired, err := repo.New(probe).TryLockUserSessionIssuerForRemoteIssuerDerivation(ctx, userIssuerID)
		// Released immediately either way: a probe that won the race would
		// otherwise be the thing blocking the migration.
		rollbackErr := probe.Rollback(ctx)

		assert.NoError(c, err)
		assert.NoError(c, rollbackErr)
		assert.False(c, acquired, "the derivation lock for user session issuer %s is still free", userIssuerID)
	}, 30*time.Second, 25*time.Millisecond)
}

func awaitMigration(t *testing.T, done <-chan error) error {
	t.Helper()

	select {
	case err := <-done:
		return err
	case <-time.After(30 * time.Second):
		t.Fatal("migration never returned; the derivation lock was most likely taken out of order")
		return nil
	}
}

// TestMigrateIssuer_ResyncsBindingsAddedBeforeTheIssuerLock is the regression
// test for the window. A binding committed inside it used to be resynced by
// nobody: its writer stamps the pre-re-point source and commits first, then the
// migration re-points without recomputing. The server was left naming an issuer
// the same transaction soft-deleted, so it stopped resolving.
func TestMigrateIssuer_ResyncsBindingsAddedBeforeTheIssuerLock(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	w := newMigrationWindow(t, ctx, ti, "mig-window")

	// Parks the migration at lockIssuersForMigration, which is strictly after
	// the early read and strictly before the re-point.
	blocker := testenv.BeginTx(t, ctx, ti.conn)
	require.NoError(t, repo.New(blocker).LockRemoteSessionIssuerForClientBinding(ctx, w.sourceID))

	done := make(chan error, 1)
	go func() {
		_, err := ti.service.MigrateIssuer(ctx, migratePayload(w.sourceID.String(), w.targetID.String()))
		done <- err
	}()

	waitForDerivationLock(t, ctx, ti.conn, w.earlyIssuerID)
	w.attachInsideWindow(t, ctx, ti)

	require.NoError(t, blocker.Rollback(ctx))
	require.NoError(t, awaitMigration(t, done))

	require.Equal(t, uuid.NullUUID{UUID: w.targetID, Valid: true}, storedIssuer(t, ctx, ti.conn, w.projectID, w.earlyServerID))
	require.Equal(t, uuid.NullUUID{UUID: w.targetID, Valid: true}, storedIssuer(t, ctx, ti.conn, w.projectID, w.lateServerID),
		"a binding added before the migration took the source's lock must not be left naming the soft-deleted source")
}

// TestMigrateIssuer_ConflictsWhenAWindowBindingIsLockedElsewhere covers the
// other half. The migration cannot wait for a derivation lock while holding the
// lock those holders wait on, so it takes the window's ids without blocking and
// gives up when one is held. That has to surface as a retryable conflict, and
// the retry has to work, or the fix trades a stale value for a stuck migration.
func TestMigrateIssuer_ConflictsWhenAWindowBindingIsLockedElsewhere(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	w := newMigrationWindow(t, ctx, ti, "mig-contended")

	blocker := testenv.BeginTx(t, ctx, ti.conn)
	require.NoError(t, repo.New(blocker).LockRemoteSessionIssuerForClientBinding(ctx, w.sourceID))

	done := make(chan error, 1)
	go func() {
		_, err := ti.service.MigrateIssuer(ctx, migratePayload(w.sourceID.String(), w.targetID.String()))
		done <- err
	}()

	waitForDerivationLock(t, ctx, ti.conn, w.earlyIssuerID)
	w.attachInsideWindow(t, ctx, ti)

	// A third writer holding the new binding's derivation lock: the state the
	// migration must not block on.
	holder := testenv.BeginTx(t, ctx, ti.conn)
	require.NoError(t, repo.New(holder).LockUserSessionIssuerForRemoteIssuerDerivation(ctx, w.lateIssuerID))

	require.NoError(t, blocker.Rollback(ctx))
	requireOopsCode(t, awaitMigration(t, done), oops.CodeConflict)

	require.Equal(t, uuid.NullUUID{UUID: w.sourceID, Valid: true}, storedIssuer(t, ctx, ti.conn, w.projectID, w.earlyServerID),
		"a refused migration must roll back whole")

	// Retrying once the contention clears reads a set that already contains the
	// new binding, so it needs no window handling at all.
	require.NoError(t, holder.Rollback(ctx))
	_, err := ti.service.MigrateIssuer(ctx, migratePayload(w.sourceID.String(), w.targetID.String()))
	require.NoError(t, err)

	require.Equal(t, uuid.NullUUID{UUID: w.targetID, Valid: true}, storedIssuer(t, ctx, ti.conn, w.projectID, w.earlyServerID))
	require.Equal(t, uuid.NullUUID{UUID: w.targetID, Valid: true}, storedIssuer(t, ctx, ti.conn, w.projectID, w.lateServerID))
}
