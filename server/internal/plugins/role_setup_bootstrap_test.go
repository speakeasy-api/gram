package plugins_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	roledistributionv1 "github.com/speakeasy-api/gram/infra/gen/gram/role_distribution/v1"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/roledistribution"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

func TestRoleBootstrapImmediateSkipsInactiveRoles(t *testing.T) {
	t.Parallel()
	constructors := map[string]func(context.Context, *orgrepo.Queries, string) error{
		"create": func(ctx context.Context, q *orgrepo.Queries, id string) error {
			return q.CreateOrganizationMetadata(ctx, orgrepo.CreateOrganizationMetadataParams{ID: id, Name: id, Slug: id})
		},
		"upsert": func(ctx context.Context, q *orgrepo.Queries, id string) error {
			_, err := q.UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{ID: id, Name: id, Slug: id})
			if err != nil {
				return fmt.Errorf("create organization fixture: %w", err)
			}
			return nil
		},
		"create-workos": func(ctx context.Context, q *orgrepo.Queries, id string) error {
			_, err := q.CreateOrganizationMetadataFromWorkOS(ctx, orgrepo.CreateOrganizationMetadataFromWorkOSParams{ID: id, Name: id, Slug: id, WorkosID: conv.ToPGText(id), VerifiedDomains: []string{}})
			if err != nil {
				return fmt.Errorf("create organization fixture: %w", err)
			}
			return nil
		},
		"upsert-workos": func(ctx context.Context, q *orgrepo.Queries, id string) error {
			_, err := q.UpsertOrganizationMetadataFromWorkOS(ctx, orgrepo.UpsertOrganizationMetadataFromWorkOSParams{ID: id, Name: id, Slug: id, WorkosID: conv.ToPGText(id)})
			if err != nil {
				return fmt.Errorf("create organization fixture: %w", err)
			}
			return nil
		},
	}
	for name, create := range constructors {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestPluginsService(t)
			roles := bootstrapGlobalRoles(t, ctx, ti)
			orgID := "org_" + uuid.NewString()
			require.NoError(t, create(ctx, orgrepo.New(ti.conn), orgID))
			afterID, err := testrepo.New(ti.conn).SourceLastOutboxID(ctx)
			require.NoError(t, err)
			require.NoError(t, roledistribution.ProcessOrganizationBootstrap(ctx, ti.conn, orgID, ""))
			assertBootstrapRoles(t, ctx, ti, orgID, roles, afterID)
		})
	}
}

func bootstrapGlobalRoles(t *testing.T, ctx context.Context, ti *testInstance) map[string]string {
	t.Helper()
	q := accessrepo.New(ti.conn)
	roles := map[string]string{}
	now := conv.ToPGTimestamptz(time.Now())
	for _, state := range []string{"active", "deleted", "workos-deleted"} {
		slug := "bootstrap-" + state
		require.NoError(t, q.UpsertGlobalRole(ctx, accessrepo.UpsertGlobalRoleParams{WorkosSlug: slug, WorkosName: slug, WorkosCreatedAt: now, WorkosUpdatedAt: now}))
		role, err := q.GetGlobalRoleBySlug(ctx, slug)
		require.NoError(t, err)
		roles[state] = "role:global:" + role.ID.String()
		if state != "active" {
			err := testrepo.New(ti.conn).BootstrapSetGlobalRoleInactive(ctx, testrepo.BootstrapSetGlobalRoleInactiveParams{
				ID: role.ID, WorkosDeleted: state == "workos-deleted",
			})
			require.NoError(t, err)
		}
	}
	return roles
}

func assertBootstrapRoles(t *testing.T, ctx context.Context, ti *testInstance, orgID string, roles map[string]string, afterID int64) {
	t.Helper()
	rows, err := testrepo.New(ti.conn).ListPublishOutboxRows(ctx)
	require.NoError(t, err)
	counts := map[string]int{}
	for _, row := range rows {
		if row.ID <= afterID || row.Topic != "gram.role_distribution.v1.RoleDistributionSetupRequestedV1" {
			continue
		}
		event := new(roledistributionv1.RoleDistributionSetupRequestedV1)
		require.NoError(t, proto.Unmarshal(row.Message, event))
		if event.GetOrganizationId() == orgID {
			counts[event.GetRoleUrn()]++
		}
	}
	for state, role := range roles {
		expected := 0
		if state == "active" {
			expected = 1
		}
		require.Equal(t, expected, counts[role], state)
	}
}

func TestRoleBootstrapDeferredSkipsInactiveRoles(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestPluginsService(t)
	ac, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	globals := bootstrapGlobalRoles(t, ctx, ti)
	locals := map[string]string{}
	for _, state := range []string{"active", "deleted", "workos-deleted"} {
		role := createTestRolePrincipal(t, ctx, ti, "Bootstrap "+state)
		locals[state] = role
		if state != "active" {
			err := testrepo.New(ti.conn).BootstrapSetOrganizationRoleInactive(ctx, testrepo.BootstrapSetOrganizationRoleInactiveParams{
				RoleUrn: role, WorkosDeleted: state == "workos-deleted",
			})
			require.NoError(t, err)
		}
	}
	// Inspect only expansion messages, not earlier role source events.
	afterID, err := testrepo.New(ti.conn).SourceLastOutboxID(ctx)
	require.NoError(t, err)
	require.NoError(t, roledistribution.ProcessOrganizationBootstrap(ctx, ti.conn, ac.ActiveOrganizationID, ""))
	assertBootstrapRoles(t, ctx, ti, ac.ActiveOrganizationID, globals, afterID)
	assertBootstrapRoles(t, ctx, ti, ac.ActiveOrganizationID, locals, afterID)
}

// Continue only from the cursor in emitted messages.
func TestRoleBootstrapBoundedOutboxContinuation(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestPluginsService(t)
	ac, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	for i := range 101 {
		err := testrepo.New(ti.conn).SourceInsertGlobalRoleWithoutDistribution(ctx, fmt.Sprintf("bounded-bootstrap-%03d", i))
		require.NoError(t, err)
	}
	cursor, err := testrepo.New(ti.conn).SourceLastOutboxID(ctx)
	require.NoError(t, err)
	require.NoError(t, roledistribution.ProcessOrganizationBootstrap(ctx, ti.conn, ac.ActiveOrganizationID, ""))
	roleURNs := map[string]bool{}
	previousCursor := ""
	continuations := 0
	// This fixture fits in two pages; a third drain observes no new messages.
	for page := range 3 {
		rows, err := testrepo.New(ti.conn).ListPublishOutboxRows(ctx)
		require.NoError(t, err)
		next := cursor
		var bootstrap *roledistributionv1.RoleDistributionSetupRequestedV1
		lastRoleURN := ""
		pageSetups := 0
		for _, row := range rows {
			if row.ID <= cursor {
				continue
			}
			if row.ID > next {
				next = row.ID
			}
			if row.Topic != "gram.role_distribution.v1.RoleDistributionSetupRequestedV1" {
				continue
			}
			event := new(roledistributionv1.RoleDistributionSetupRequestedV1)
			require.NoError(t, proto.Unmarshal(row.Message, event))
			if event.GetOrganizationId() == ac.ActiveOrganizationID && event.GetRoleUrn() != "" {
				require.False(t, roleURNs[event.GetRoleUrn()], "each role must occur on only one page")
				roleURNs[event.GetRoleUrn()] = true
				require.Greater(t, event.GetRoleUrn(), previousCursor)
				if event.GetRoleUrn() > lastRoleURN {
					lastRoleURN = event.GetRoleUrn()
				}
				pageSetups++
			}
			if event.GetBootstrapOrganizationId() == ac.ActiveOrganizationID {
				require.Nil(t, bootstrap)
				bootstrap = event
			}
		}
		require.LessOrEqual(t, pageSetups, 100)
		if page == 0 {
			require.Equal(t, 100, pageSetups)
		}
		cursor = next
		if bootstrap == nil {
			break
		}
		continuations++
		require.Equal(t, 100, pageSetups)
		require.Equal(t, lastRoleURN, bootstrap.GetCursor())
		require.Greater(t, bootstrap.GetCursor(), previousCursor)
		previousCursor = bootstrap.GetCursor()
		require.NoError(t, roledistribution.ProcessOrganizationBootstrap(ctx, ti.conn, bootstrap.GetBootstrapOrganizationId(), bootstrap.GetCursor()))
	}
	require.Equal(t, 1, continuations)
	require.GreaterOrEqual(t, len(roleURNs), 101)
}

func TestRoleGlobalFanoutBoundedOutboxContinuation(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestPluginsService(t)
	organizations := orgrepo.New(ti.conn)
	for i := range 101 {
		id := fmt.Sprintf("org-bounded-fanout-%03d", i)
		require.NoError(t, organizations.CreateOrganizationMetadata(ctx, orgrepo.CreateOrganizationMetadataParams{ID: id, Name: id, Slug: id}))
	}
	roles := accessrepo.New(ti.conn)
	now := conv.ToPGTimestamptz(time.Now())
	require.NoError(t, roles.UpsertGlobalRole(ctx, accessrepo.UpsertGlobalRoleParams{
		WorkosSlug: "bounded-fanout", WorkosName: "Bounded fanout", WorkosCreatedAt: now, WorkosUpdatedAt: now,
	}))
	role, err := roles.GetGlobalRoleBySlug(ctx, "bounded-fanout")
	require.NoError(t, err)
	roleURN := "role:global:" + role.ID.String()
	fixtures := testrepo.New(ti.conn)
	afterID, err := fixtures.SourceLastOutboxID(ctx)
	require.NoError(t, err)
	require.NoError(t, roledistribution.ProcessGlobalFanout(ctx, ti.conn, role.ID, ""))
	seen := map[string]bool{}
	previousCursor := ""
	continuations := 0
	for page := range 3 {
		rows, err := fixtures.ListPublishOutboxRows(ctx)
		require.NoError(t, err)
		nextID := afterID
		pageSetups := 0
		lastOrganization := ""
		var continuation *roledistributionv1.RoleDistributionSetupRequestedV1
		for _, row := range rows {
			if row.ID <= afterID {
				continue
			}
			if row.ID > nextID {
				nextID = row.ID
			}
			if row.Topic != "gram.role_distribution.v1.RoleDistributionSetupRequestedV1" {
				continue
			}
			event := new(roledistributionv1.RoleDistributionSetupRequestedV1)
			require.NoError(t, proto.Unmarshal(row.Message, event))
			if event.GetRoleUrn() == roleURN {
				org := event.GetOrganizationId()
				require.False(t, seen[org], "each organization must occur on only one page")
				require.Greater(t, org, previousCursor)
				seen[org] = true
				pageSetups++
				if org > lastOrganization {
					lastOrganization = org
				}
			}
			if event.GetGlobalRoleId() == role.ID.String() {
				require.Nil(t, continuation)
				continuation = event
			}
		}
		require.LessOrEqual(t, pageSetups, 100)
		if page == 0 {
			require.Equal(t, 100, pageSetups)
		}
		afterID = nextID
		if continuation == nil {
			break
		}
		continuations++
		require.Equal(t, 100, pageSetups)
		require.Equal(t, lastOrganization, continuation.GetCursor())
		require.Greater(t, continuation.GetCursor(), previousCursor)
		previousCursor = continuation.GetCursor()
		require.NoError(t, roledistribution.ProcessGlobalFanout(ctx, ti.conn, role.ID, continuation.GetCursor()))
	}
	require.Equal(t, 1, continuations)
	for i := range 101 {
		require.True(t, seen[fmt.Sprintf("org-bounded-fanout-%03d", i)])
	}
}
