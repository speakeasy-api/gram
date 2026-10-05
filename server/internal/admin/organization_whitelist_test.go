package admin

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
)

func TestDashboardOrganizationWhitelistRecordsActorAndPreservesOtherFields(t *testing.T) {
	t.Parallel()
	ctx, service, db := newTestAdminService(t)
	ctx = contextvalues.SetAdminAuthContext(ctx, &contextvalues.AdminAuthContext{SessionID: "test-session", OIDCSubject: "staff-subject", Name: "Test Operator", Email: "operator@example.test"})
	const id = "org_whitelist_placeholder"
	seedOrg(t, ctx, db, orgFixture{id: id, name: "Whitelist Test", slug: "whitelist-test", accountType: "pro"})
	q := orgrepo.New(db)
	before, err := q.GetOrganizationMetadata(ctx, id)
	require.NoError(t, err)
	result, err := service.UpdateOrganization(ctx, &gen.UpdateOrganizationPayload{ID: id, Whitelisted: new(true)})
	require.NoError(t, err)
	require.True(t, result.Whitelisted)
	after, err := q.GetOrganizationMetadata(ctx, id)
	require.NoError(t, err)
	after.Whitelisted, after.UpdatedAt = before.Whitelisted, before.UpdatedAt
	require.Equal(t, before, after)
	entry, err := audittest.LatestAuditLogByAction(ctx, db, audit.ActionOrganizationWhitelistUpdated)
	require.NoError(t, err)
	require.Equal(t, id, entry.OrganizationID)
	require.Equal(t, "staff-subject", entry.ActorID)
	require.Equal(t, "Test Operator", *entry.ActorDisplayName)
	require.JSONEq(t, `{"whitelisted":false}`, string(entry.BeforeSnapshot))
	require.JSONEq(t, `{"whitelisted":true}`, string(entry.AfterSnapshot))
	_, err = service.UpdateOrganization(ctx, &gen.UpdateOrganizationPayload{ID: id, Whitelisted: new(true)})
	require.NoError(t, err)
	count, err := audittest.AuditLogCountByAction(ctx, db, audit.ActionOrganizationWhitelistUpdated)
	require.NoError(t, err)
	require.Equal(t, int64(1), count, "a no-op must not emit another audit event")
}

func TestDashboardOrganizationWhitelistAuditFailureRollsBack(t *testing.T) {
	t.Parallel()
	ctx, service, db := newTestAdminService(t)
	const id = "org_whitelist_rollback_placeholder"
	seedOrg(t, ctx, db, orgFixture{id: id, name: "Rollback Test", slug: "rollback-test"})
	before, err := orgrepo.New(db).GetOrganizationMetadata(ctx, id)
	require.NoError(t, err)
	require.NoError(t, audittest.RejectAction(ctx, db, audit.ActionOrganizationWhitelistUpdated))
	_, err = service.UpdateOrganization(ctx, &gen.UpdateOrganizationPayload{ID: id, Whitelisted: new(true)})
	require.Error(t, err)
	after, err := orgrepo.New(db).GetOrganizationMetadata(ctx, id)
	require.NoError(t, err)
	require.Equal(t, before, after)
}

func TestDashboardOrganizationWhitelistRejectsMissingTarget(t *testing.T) {
	t.Parallel()
	ctx, service, _ := newTestAdminService(t)
	_, err := service.UpdateOrganization(ctx, &gen.UpdateOrganizationPayload{ID: "org_missing_placeholder", Whitelisted: new(true)})
	require.Error(t, err)
}
