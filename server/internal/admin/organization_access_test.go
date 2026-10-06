package admin

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
)

func TestDashboardOrganizationAccessWritesRecordAdminActor(t *testing.T) {
	t.Parallel()
	ctx, service, db := newTestAdminService(t)
	ctx = contextvalues.SetAdminAuthContext(ctx, &contextvalues.AdminAuthContext{
		SessionID: "staff-session-placeholder", OIDCSubject: "staff-subject-placeholder",
		Name: "Test Operator", Email: "operator@example.test",
	})
	seedOrg(t, ctx, db, orgFixture{id: "org_access_actor_placeholder", name: "Access Test", slug: "access-test"})

	disabled, err := service.DisableOrganization(ctx, &gen.DisableOrganizationPayload{ID: "org_access_actor_placeholder"})
	require.NoError(t, err)
	require.NotNil(t, disabled.DisabledAt)
	entry, err := audittest.LatestAuditLogByAction(ctx, db, audit.ActionOrganizationDisabled)
	require.NoError(t, err)
	require.Equal(t, "org_access_actor_placeholder", entry.OrganizationID)
	require.Equal(t, "staff-subject-placeholder", entry.ActorID)
	require.Equal(t, "Test Operator", *entry.ActorDisplayName)

	enabled, err := service.EnableOrganization(ctx, &gen.EnableOrganizationPayload{ID: "org_access_actor_placeholder"})
	require.NoError(t, err)
	require.Nil(t, enabled.DisabledAt)
	entry, err = audittest.LatestAuditLogByAction(ctx, db, audit.ActionOrganizationEnabled)
	require.NoError(t, err)
	require.Equal(t, "org_access_actor_placeholder", entry.OrganizationID)
	require.Equal(t, "staff-subject-placeholder", entry.ActorID)
	require.Equal(t, "Test Operator", *entry.ActorDisplayName)
}
