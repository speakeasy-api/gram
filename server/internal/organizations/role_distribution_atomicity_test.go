package organizations_test

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

// The source transaction + outbox seam must work for pool-backed onboarding as
// well as callers that already own a transaction (e.g. WorkOS reconciliation).
func TestRoleDistributionOrganizationOutboxFailureRollsBackSource(t *testing.T) {
	t.Parallel()
	for _, method := range []string{"create", "upsert", "workos-create", "workos-upsert"} {
		for _, callerTx := range []bool{false, true} {
			name := method + "/pool"
			if callerTx {
				name = method + "/transaction"
			}
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				ctx, ti := newTestOrganizationsService(t)
				fixture := testrepo.New(ti.conn)
				before, err := fixture.CountPublishOutboxRows(ctx)
				require.NoError(t, err)
				err = fixture.RejectPublishOutboxWritesFixture(ctx)
				require.NoError(t, err)
				q := orgrepo.New(ti.conn)
				var tx pgx.Tx
				if callerTx {
					tx, err = ti.conn.Begin(ctx) //nolint:glint // notestingrawsql: transaction boundary for source/outbox rollback assertions
					require.NoError(t, err)
					defer func() { _ = tx.Rollback(ctx) }()
					q = q.WithTx(tx)
				}
				const id = "org_distribution_atomicity"
				switch method {
				case "create":
					err = q.CreateOrganizationMetadata(ctx, orgrepo.CreateOrganizationMetadataParams{ID: id, Name: "Atomic organization", Slug: "atomic-organization"})
				case "upsert":
					_, err = q.UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{ID: id, Name: "Atomic organization", Slug: "atomic-organization"})
				case "workos-create":
					_, err = q.CreateOrganizationMetadataFromWorkOS(ctx, orgrepo.CreateOrganizationMetadataFromWorkOSParams{ID: id, Name: "Atomic organization", Slug: "atomic-organization", VerifiedDomains: []string{}})
				case "workos-upsert":
					_, err = q.UpsertOrganizationMetadataFromWorkOS(ctx, orgrepo.UpsertOrganizationMetadataFromWorkOSParams{ID: id, Name: "Atomic organization", Slug: "atomic-organization"})
				}
				require.ErrorContains(t, err, "reject_publish_outbox_writes_fixture")
				if callerTx {
					require.NoError(t, tx.Commit(ctx))
				}
				_, err = orgrepo.New(ti.conn).GetOrganizationMetadata(ctx, id)
				require.ErrorIs(t, err, pgx.ErrNoRows)
				after, err := fixture.CountPublishOutboxRows(ctx)
				require.NoError(t, err)
				require.Equal(t, before, after)
			})
		}
	}
}

func TestRoleDistributionOrganizationWithTxPreservesCallerAtomicity(t *testing.T) {
	t.Parallel()
	for _, method := range []string{"create", "upsert", "workos-create", "workos-upsert"} {
		for _, outcome := range []string{"commit", "rollback"} {
			t.Run(method+"/"+outcome, func(t *testing.T) {
				t.Parallel()
				ctx, ti := newTestOrganizationsService(t)
				now := conv.ToPGTimestamptz(time.Now())
				require.NoError(t, accessrepo.New(ti.conn).UpsertGlobalRole(ctx, accessrepo.UpsertGlobalRoleParams{WorkosSlug: "nested-bootstrap-role", WorkosName: "Bootstrap role", WorkosCreatedAt: now, WorkosUpdatedAt: now}))
				observer := testrepo.New(ti.conn)
				before, err := observer.CountPublishOutboxRows(ctx)
				require.NoError(t, err)
				tx, err := ti.conn.Begin(ctx) //nolint:glint // notestingrawsql: caller-owned transaction tests savepoint visibility and rollback
				require.NoError(t, err)
				defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })
				q := orgrepo.New(ti.conn).WithTx(tx)
				const id = "org_nested_atomicity"
				switch method {
				case "create":
					err = q.CreateOrganizationMetadata(ctx, orgrepo.CreateOrganizationMetadataParams{ID: id, Name: "Nested organization", Slug: "nested-organization"})
				case "upsert":
					_, err = q.UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{ID: id, Name: "Nested organization", Slug: "nested-organization"})
				case "workos-create":
					_, err = q.CreateOrganizationMetadataFromWorkOS(ctx, orgrepo.CreateOrganizationMetadataFromWorkOSParams{ID: id, Name: "Nested organization", Slug: "nested-organization", VerifiedDomains: []string{}})
				case "workos-upsert":
					_, err = q.UpsertOrganizationMetadataFromWorkOS(ctx, orgrepo.UpsertOrganizationMetadataFromWorkOSParams{ID: id, Name: "Nested organization", Slug: "nested-organization"})
				}
				require.NoError(t, err)
				_, err = q.GetOrganizationMetadata(ctx, id)
				require.NoError(t, err)
				inside := testrepo.New(tx)
				events, err := inside.CountPublishOutboxRows(ctx)
				require.NoError(t, err)
				require.Equal(t, before+1, events)
				assertVisible := func(visible bool) {
					t.Helper()
					_, err := orgrepo.New(ti.conn).GetOrganizationMetadata(ctx, id)
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
