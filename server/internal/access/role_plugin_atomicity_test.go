package access

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/stretchr/testify/require"
)

func TestRoleDistributionOutboxFailureAbortsSourceTransaction(t *testing.T) {
	t.Parallel()
	for _, scope := range []string{"organization-outbox", "organization-create-outbox", "global-outbox"} {
		t.Run(scope, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestAccessService(t)
			ac, ok := contextvalues.GetAuthContext(ctx)
			require.True(t, ok)
			// NOT VALID leaves fixture rows untouched while rejecting new writes.
			fixture := testrepo.New(ti.conn)
			before, err := fixture.CountPublishOutboxRows(ctx)
			require.NoError(t, err)
			err = fixture.RejectPublishOutboxWritesFixture(ctx)
			require.NoError(t, err)
			tx, err := ti.conn.Begin(ctx) //nolint:glint // notestingrawsql: transaction boundary verifies failed source savepoint rollback
			require.NoError(t, err)
			defer func() { _ = tx.Rollback(ctx) }()
			q := accessrepo.New(tx)
			now := conv.ToPGTimestamptz(time.Now())
			switch scope {
			case "organization-create-outbox":
				_, err = q.CreateOrganizationRole(ctx, accessrepo.CreateOrganizationRoleParams{OrganizationID: ac.ActiveOrganizationID, WorkosSlug: "atomic-source", WorkosName: "Atomic source", WorkosCreatedAt: now, WorkosUpdatedAt: now})
			case "organization-outbox":
				_, err = q.UpsertOrganizationRole(ctx, accessrepo.UpsertOrganizationRoleParams{OrganizationID: ac.ActiveOrganizationID, WorkosSlug: "atomic-source", WorkosName: "Atomic source", WorkosCreatedAt: now, WorkosUpdatedAt: now})
			default:
				err = q.UpsertGlobalRole(ctx, accessrepo.UpsertGlobalRoleParams{WorkosSlug: "atomic-source", WorkosName: "Atomic source", WorkosCreatedAt: now, WorkosUpdatedAt: now})
			}
			require.ErrorContains(t, err, "reject_publish_outbox_writes_fixture")
			require.NoError(t, tx.Commit(ctx), "failed source operation must roll back its savepoint")
			q = accessrepo.New(ti.conn)
			if scope == "organization-outbox" || scope == "organization-create-outbox" {
				_, err = q.GetOrganizationRoleBySlug(ctx, accessrepo.GetOrganizationRoleBySlugParams{OrganizationID: ac.ActiveOrganizationID, WorkosSlug: "atomic-source"})
			} else {
				_, err = q.GetGlobalRoleBySlug(ctx, "atomic-source")
			}
			require.ErrorIs(t, err, pgx.ErrNoRows, "source role must not survive failed outbox write")
			after, err := fixture.CountPublishOutboxRows(ctx)
			require.NoError(t, err)
			require.Equal(t, before, after)
		})
	}
}

func TestRoleDistributionWithTxPreservesCallerAtomicity(t *testing.T) {
	t.Parallel()
	for _, method := range []string{"create", "upsert"} {
		for _, outcome := range []string{"commit", "rollback"} {
			t.Run(method+"/"+outcome, func(t *testing.T) {
				t.Parallel()
				ctx, ti := newTestAccessService(t)
				ac, ok := contextvalues.GetAuthContext(ctx)
				require.True(t, ok)
				observer := testrepo.New(ti.conn)
				before, err := observer.CountPublishOutboxRows(ctx)
				require.NoError(t, err)
				tx, err := ti.conn.Begin(ctx) //nolint:glint // notestingrawsql: caller-owned transaction tests savepoint visibility and rollback
				require.NoError(t, err)
				defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })
				q := accessrepo.New(ti.conn).WithTx(tx)
				now := conv.ToPGTimestamptz(time.Now())
				const slug = "nested-atomic-role"
				if method == "create" {
					_, err = q.CreateOrganizationRole(ctx, accessrepo.CreateOrganizationRoleParams{OrganizationID: ac.ActiveOrganizationID, WorkosSlug: slug, WorkosName: "Nested role", WorkosCreatedAt: now, WorkosUpdatedAt: now})
				} else {
					_, err = q.UpsertOrganizationRole(ctx, accessrepo.UpsertOrganizationRoleParams{OrganizationID: ac.ActiveOrganizationID, WorkosSlug: slug, WorkosName: "Nested role", WorkosCreatedAt: now, WorkosUpdatedAt: now})
				}
				require.NoError(t, err)
				_, err = q.GetOrganizationRoleBySlug(ctx, accessrepo.GetOrganizationRoleBySlugParams{OrganizationID: ac.ActiveOrganizationID, WorkosSlug: slug})
				require.NoError(t, err)
				inside := testrepo.New(tx)
				events, err := inside.CountPublishOutboxRows(ctx)
				require.NoError(t, err)
				require.Equal(t, before+1, events)
				assertVisible := func(visible bool) {
					t.Helper()
					_, err := accessrepo.New(ti.conn).GetOrganizationRoleBySlug(ctx, accessrepo.GetOrganizationRoleBySlugParams{OrganizationID: ac.ActiveOrganizationID, WorkosSlug: slug})
					if visible {
						require.NoError(t, err)
					} else {
						require.ErrorIs(t, err, pgx.ErrNoRows)
					}
					count, err := observer.CountPublishOutboxRows(ctx)
					require.NoError(t, err)
					if visible {
						require.Equal(t, events, count)
					} else {
						require.Equal(t, before, count)
					}
				}
				assertVisible(false)
				if outcome == "commit" {
					require.NoError(t, tx.Commit(ctx))
				} else {
					require.NoError(t, tx.Rollback(ctx))
				}
				assertVisible(outcome == "commit")
			})
		}
	}
}
