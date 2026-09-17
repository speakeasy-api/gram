package remotesessions_test

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/stretchr/testify/require"
)

// Authorization uses request-scoped grant snapshots. Swapping an immutable
// snapshot models revocation without racing on grants or AuthContext fields.
// This tests post-wait evaluation, not persisted grant reload (Require does not
// reload grants from the database).
type preparationPostLockContext struct {
	context.Context //nolint:containedctx // Implements a context wrapper to atomically replace authorization values during a lock wait.
	current         atomic.Pointer[context.Context]
}

func (c *preparationPostLockContext) Value(key any) any {
	return (*c.current.Load()).Value(key)
}

func TestPreparationPostLockAuthorization_RevokedWhileBlocked(t *testing.T) {
	t.Parallel()
	for _, lock := range []string{"binding", "registration"} {
		for _, mutation := range []string{"prepare", "unlink"} {
			for _, revocation := range []string{"scope", "tenant"} {
				t.Run(lock+"/"+mutation+"/"+revocation, func(t *testing.T) {
					t.Parallel()
					ctx, ti, in := preparationFixture(t)
					authCtx, ok := contextvalues.GetAuthContext(ctx)
					require.True(t, ok)
					in.ConfirmGrants = []string{preparationJWTGrant}
					if mutation == "unlink" {
						prepared, err := ti.service.PrepareIdentityChaining(ctx, in)
						require.NoError(t, err)
						in.ExpectedGeneration = prepared.Generation
					}
					before, err := ti.service.ReadIdentityChaining(ctx, in)
					require.NoError(t, err)

					allowed := authz.GrantsToContext(ctx, []authz.Grant{{
						Scope: authz.ScopeProjectWrite, Selector: authz.NewSelector(authz.ScopeProjectWrite, authCtx.ProjectID.String()),
					}})
					denied := authz.GrantsToContext(ctx, nil)
					if revocation == "tenant" {
						other := *authCtx
						other.ActiveOrganizationID = "another-organization"
						denied = contextvalues.SetAuthContext(allowed, &other)
					}
					requestCtx := &preparationPostLockContext{Context: ctx, current: atomic.Pointer[context.Context]{}}
					requestCtx.current.Store(&allowed)

					holder, err := ti.conn.Acquire(ctx)
					require.NoError(t, err)
					defer holder.Release()
					key := strings.Join([]string{"ema-preparation", authCtx.ActiveOrganizationID, authCtx.ProjectID.String(), in.UserSessionIssuerID.String(), in.RemoteSessionIssuerID.String(), in.Resource}, "\n")
					if lock == "registration" {
						key = "ema-registration-issuer:" + in.RemoteSessionIssuerID.String()
					}
					q := repo.New(holder)
					require.NoError(t, q.LockPreparationSubmission(ctx, key))
					defer func() { _ = q.UnlockPreparationSubmission(context.Background(), key) }()
					done := make(chan error, 1)
					go func() {
						var err error
						if mutation == "unlink" {
							_, err = ti.service.UnlinkIdentityChaining(requestCtx, in)
						} else {
							_, err = ti.service.PrepareIdentityChaining(requestCtx, in)
						}
						done <- err
					}()
					// Observe a real lock waiter: the initial scope check has passed.
					require.Eventually(t, func() bool {
						var blocked bool
						err := ti.conn.QueryRow( //nolint:glint // notestingrawsql: pg_blocking_pids is a PostgreSQL test synchronization primitive unavailable to SQLc generation
							ctx, `SELECT EXISTS (SELECT 1 FROM pg_stat_activity WHERE datname = current_database() AND $1 = ANY(pg_blocking_pids(pid)))`, holder.Conn().PgConn().PID(),
						).Scan(&blocked)
						require.NoError(t, err)
						return blocked
					}, 5*time.Second, 10*time.Millisecond)
					requestCtx.current.Store(&denied)
					require.NoError(t, q.UnlockPreparationSubmission(ctx, key))
					select {
					case err := <-done:
						requireOopsCode(t, err, oops.CodeForbidden)
					case <-time.After(5 * time.Second):
						t.Fatal("preparation did not finish after releasing the lock")
					}
					after, err := ti.service.ReadIdentityChaining(ctx, in)
					require.NoError(t, err)
					require.Equal(t, before, after, "denied mutation must preserve the binding")
					if mutation == "prepare" {
						count, err := testrepo.New(ti.conn).CountPreparationFixtureBindings(ctx, *authCtx.ProjectID)
						require.NoError(t, err)
						require.Zero(t, count, "denial must not create a binding")
					}
				})
			}
		}
	}
}
