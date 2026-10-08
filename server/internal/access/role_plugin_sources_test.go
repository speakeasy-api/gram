package access

import (
	"testing"
	"time"

	"github.com/google/uuid"
	roledistributionv1 "github.com/speakeasy-api/gram/infra/gen/gram/role_distribution/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestRoleDistributionSourcesEmitOnlyOnCreate(t *testing.T) {
	t.Parallel()
	for _, scope := range []string{"organization", "global"} {
		t.Run(scope, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestAccessService(t)
			ac, ok := contextvalues.GetAuthContext(ctx)
			require.True(t, ok)
			q := accessrepo.New(ti.conn)
			now := conv.ToPGTimestamptz(time.Now().UTC())
			upsert := func(event string) {
				t.Helper()
				if scope == "global" {
					require.NoError(t, q.UpsertGlobalRole(ctx, accessrepo.UpsertGlobalRoleParams{WorkosSlug: "source-role", WorkosName: "Source role", WorkosCreatedAt: now, WorkosUpdatedAt: now, WorkosLastEventID: conv.ToPGTextEmpty(event)}))
				} else {
					_, err := q.UpsertOrganizationRole(ctx, accessrepo.UpsertOrganizationRoleParams{OrganizationID: ac.ActiveOrganizationID, WorkosSlug: "source-role", WorkosName: "Source role", WorkosCreatedAt: now, WorkosUpdatedAt: now, WorkosLastEventID: conv.ToPGTextEmpty(event)})
					require.NoError(t, err)
				}
			}
			read := func() uuid.UUID {
				t.Helper()
				if scope == "global" {
					r, err := q.GetGlobalRoleBySlug(ctx, "source-role")
					require.NoError(t, err)
					return r.ID
				}
				r, err := q.GetOrganizationRoleBySlug(ctx, accessrepo.GetOrganizationRoleBySlugParams{OrganizationID: ac.ActiveOrganizationID, WorkosSlug: "source-role"})
				require.NoError(t, err)
				return r.ID
			}
			events := func() int64 {
				t.Helper()
				n, err := testrepo.New(ti.conn).CountPublishOutboxRows(ctx)
				require.NoError(t, err)
				return n
			}
			before := events()
			upsert("")
			afterCreate := events()
			require.Equal(t, before+1, afterCreate)
			id := read()
			outboxID, err := testrepo.New(ti.conn).SourceLastOutboxID(ctx)
			require.NoError(t, err)
			row, err := testrepo.New(ti.conn).GetPublishOutboxRow(ctx, outboxID)
			require.NoError(t, err)
			event := &roledistributionv1.RoleDistributionSetupRequestedV1{}
			require.NoError(t, proto.Unmarshal(row.Message, event))
			if scope == "global" {
				require.Equal(t, id.String(), event.GetGlobalRoleId())
			} else {
				roleURN := urn.NewPrincipal(urn.PrincipalTypeRole, "organization:"+id.String()).String()
				require.Equal(t, ac.ActiveOrganizationID, event.GetOrganizationId())
				require.Equal(t, roleURN, event.GetRoleUrn())
			}
			upsert("event_01UPDATE")
			require.Equal(t, afterCreate, events(), "ordinary updates must not emit events")
		})
	}
}

func TestRoleDistributionSourcesExistingGlobalRoleDoesNotEmit(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAccessService(t)
	fixture := testrepo.New(ti.conn)
	require.NoError(t, fixture.SourceInsertGlobalRoleWithoutDistribution(ctx, "existing-source-role"))
	before, err := fixture.CountPublishOutboxRows(ctx)
	require.NoError(t, err)
	now := conv.ToPGTimestamptz(time.Now().UTC())
	require.NoError(t, accessrepo.New(ti.conn).UpsertGlobalRole(ctx, accessrepo.UpsertGlobalRoleParams{WorkosSlug: "existing-source-role", WorkosName: "Updated source role", WorkosCreatedAt: now, WorkosUpdatedAt: now}))
	after, err := fixture.CountPublishOutboxRows(ctx)
	require.NoError(t, err)
	require.Equal(t, before, after, "updating a pre-existing role must not emit setup events")
}

func TestRoleDistributionSourcesNewOrganizationOnly(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAccessService(t)
	now := conv.ToPGTimestamptz(time.Now().UTC())
	require.NoError(t, accessrepo.New(ti.conn).UpsertGlobalRole(ctx, accessrepo.UpsertGlobalRoleParams{WorkosSlug: "bootstrap-role", WorkosName: "Bootstrap role", WorkosCreatedAt: now, WorkosUpdatedAt: now}))
	q := orgrepo.New(ti.conn)
	params := orgrepo.UpsertOrganizationMetadataParams{ID: "org_role_source_new", Name: "Role source", Slug: "role-source"}
	fixture := testrepo.New(ti.conn)
	before, err := fixture.CountPublishOutboxRows(ctx)
	require.NoError(t, err)
	_, err = q.UpsertOrganizationMetadata(ctx, params)
	require.NoError(t, err)
	afterCreate, err := fixture.CountPublishOutboxRows(ctx)
	require.NoError(t, err)
	require.Equal(t, before+1, afterCreate)
	outboxID, err := fixture.SourceLastOutboxID(ctx)
	require.NoError(t, err)
	row, err := fixture.GetPublishOutboxRow(ctx, outboxID)
	require.NoError(t, err)
	event := &roledistributionv1.RoleDistributionSetupRequestedV1{}
	require.NoError(t, proto.Unmarshal(row.Message, event))
	require.Equal(t, params.ID, event.GetBootstrapOrganizationId())
	_, err = q.UpsertOrganizationMetadata(ctx, params)
	require.NoError(t, err)
	afterUpdate, err := fixture.CountPublishOutboxRows(ctx)
	require.NoError(t, err)
	require.Equal(t, afterCreate, afterUpdate, "existing organization must not emit bootstrap events")
}

func TestRoleDistributionSourcesConcurrentOrganizationGlobalRole(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAccessService(t)
	observer := testrepo.New(ti.conn)
	before, err := observer.CountPublishOutboxRows(ctx)
	require.NoError(t, err)
	orgTx, err := ti.conn.Begin(ctx) //nolint:glint // notestingrawsql: transaction boundary for atomic source visibility assertions
	require.NoError(t, err)
	defer o11y.NoLogDefer(func() error { return orgTx.Rollback(ctx) })
	roleTx, err := ti.conn.Begin(ctx) //nolint:glint // notestingrawsql: transaction boundary for atomic source visibility assertions
	require.NoError(t, err)
	defer o11y.NoLogDefer(func() error { return roleTx.Rollback(ctx) })

	// Neither source statement can see the other's uncommitted insertion.
	const orgID = "org_role_source_concurrent"
	_, err = orgrepo.New(orgTx).UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{ID: orgID, Name: "Concurrent source", Slug: "concurrent-source"})
	require.NoError(t, err)
	now := conv.ToPGTimestamptz(time.Now().UTC())
	q := accessrepo.New(roleTx)
	require.NoError(t, q.UpsertGlobalRole(ctx, accessrepo.UpsertGlobalRoleParams{WorkosSlug: "concurrent-source", WorkosName: "Concurrent source", WorkosCreatedAt: now, WorkosUpdatedAt: now}))
	orgEvents, err := testrepo.New(orgTx).CountPublishOutboxRows(ctx)
	require.NoError(t, err)
	require.Equal(t, before+1, orgEvents)
	roleEvents, err := testrepo.New(roleTx).CountPublishOutboxRows(ctx)
	require.NoError(t, err)
	require.Equal(t, before+1, roleEvents)
	visible, err := observer.CountPublishOutboxRows(ctx)
	require.NoError(t, err)
	require.Equal(t, before, visible, "events cannot escape their source transactions")
	require.NoError(t, orgTx.Commit(ctx))
	require.NoError(t, roleTx.Commit(ctx))
	visible, err = observer.CountPublishOutboxRows(ctx)
	require.NoError(t, err)
	require.Equal(t, before+2, visible, "both concurrent sources retain events for deferred processing")
}
