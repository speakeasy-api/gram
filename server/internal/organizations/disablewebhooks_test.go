package organizations_test

import (
	"testing"

	mockidp "github.com/speakeasy-api/gram/dev-idp/pkg/testidp"
	gen "github.com/speakeasy-api/gram/server/gen/organizations"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/stretchr/testify/require"
)

func TestService_DisableWebhooks(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	// Seed a svix app with webhooks enabled.
	_, err := orgrepo.New(ti.conn).UpsertSvixAppID(ctx, orgrepo.UpsertSvixAppIDParams{
		ID:        authCtx.ActiveOrganizationID,
		SvixAppID: conv.ToPGText(testSvixAppID),
	})
	require.NoError(t, err)

	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionOrganizationWebhooksDisabled)
	require.NoError(t, err)

	err = ti.service.DisableWebhooks(ctx, &gen.DisableWebhooksPayload{})
	require.NoError(t, err)

	org, err := orgrepo.New(ti.conn).GetOrganizationMetadata(ctx, authCtx.ActiveOrganizationID)
	require.NoError(t, err)
	require.False(t, org.WebhooksEnabled.Bool)

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionOrganizationWebhooksDisabled)
	require.NoError(t, err)
	require.Equal(t, beforeCount+1, afterCount)

	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionOrganizationWebhooksDisabled)
	require.NoError(t, err)
	require.Equal(t, string(audit.ActionOrganizationWebhooksDisabled), record.Action)
	require.Equal(t, "organization", record.SubjectType)
	require.Equal(t, mockidp.MockOrgName, record.SubjectDisplay)
	require.Equal(t, mockidp.MockOrgSlug, record.SubjectSlug)
}

// TestService_DisableWebhooks_NoopWhenAlreadyDisabled verifies that DisableWebhooks on an org
// with no svix app (webhooks_enabled is null) returns no error and logs no audit event.
func TestService_DisableWebhooks_NoopWhenAlreadyDisabled(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)

	beforeCount, err := audittest.AuditLogCount(ctx, ti.conn)
	require.NoError(t, err)

	err = ti.service.DisableWebhooks(ctx, &gen.DisableWebhooksPayload{})
	require.NoError(t, err)

	afterCount, err := audittest.AuditLogCount(ctx, ti.conn)
	require.NoError(t, err)
	require.Equal(t, beforeCount, afterCount, "no audit event should be written for a no-op disable")
}

func TestService_DisableWebhooks_ForbiddenWithoutOrgAdminGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsServiceRBAC(t)
	ctx = authztest.WithExactGrants(t, ctx)

	err := ti.service.DisableWebhooks(ctx, &gen.DisableWebhooksPayload{})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestService_DisableWebhooks_ForbiddenWithGrantForDifferentOrganization(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsServiceRBAC(t)
	ctx = authztest.WithExactGrants(t, ctx, authz.Grant{
		Scope:    authz.ScopeOrgAdmin,
		Selector: authz.NewSelector(authz.ScopeOrgAdmin, "org_other"),
	})

	err := ti.service.DisableWebhooks(ctx, &gen.DisableWebhooksPayload{})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}
