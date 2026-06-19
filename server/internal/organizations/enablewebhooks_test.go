package organizations_test

import (
	"testing"
	"time"

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
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/svix/svix-webhooks/go/models"
)

const testSvixAppID = "app_01TEST"

func TestService_EnableWebhooks(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionOrganizationWebhooksEnabled)
	require.NoError(t, err)

	orgID := authCtx.ActiveOrganizationID
	ti.svixSrv.On("GetOrCreateApp", mock.Anything, mock.MatchedBy(func(inp *models.ApplicationIn) bool {
		return inp != nil && inp.Uid != nil && *inp.Uid == orgID
	})).Return(&models.ApplicationOut{
		Id:        testSvixAppID,
		Name:      mockidp.MockOrgSlug,
		Metadata:  map[string]string{},
		CreatedAt: time.Now().UTC().Truncate(time.Second),
		UpdatedAt: time.Now().UTC().Truncate(time.Second),
	}, true, nil).Once()

	err = ti.service.EnableWebhooks(ctx, &gen.EnableWebhooksPayload{})
	require.NoError(t, err)

	org, err := orgrepo.New(ti.conn).GetOrganizationMetadata(ctx, authCtx.ActiveOrganizationID)
	require.NoError(t, err)
	require.Equal(t, testSvixAppID, org.SvixAppID.String)
	require.True(t, org.WebhooksEnabled.Bool)

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionOrganizationWebhooksEnabled)
	require.NoError(t, err)
	require.Equal(t, beforeCount+1, afterCount)

	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionOrganizationWebhooksEnabled)
	require.NoError(t, err)
	require.Equal(t, string(audit.ActionOrganizationWebhooksEnabled), record.Action)
	require.Equal(t, "organization", record.SubjectType)
	require.Equal(t, mockidp.MockOrgName, record.SubjectDisplay)
	require.Equal(t, mockidp.MockOrgSlug, record.SubjectSlug)

	ti.svixSrv.AssertExpectations(t)
}

// TestService_EnableWebhooks_ReuseExistingApp verifies that when the org already has a
// svix app ID stored, EnableWebhooks re-enables webhooks without calling GetOrCreate.
func TestService_EnableWebhooks_ReuseExistingApp(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	// Pre-seed a svix app ID. UpsertSvixAppID also sets webhooks_enabled=TRUE; that's fine —
	// EnableWebhooks is idempotent and should succeed without calling GetOrCreate.
	_, err := orgrepo.New(ti.conn).UpsertSvixAppID(ctx, orgrepo.UpsertSvixAppIDParams{
		ID:        authCtx.ActiveOrganizationID,
		SvixAppID: conv.ToPGText(testSvixAppID),
	})
	require.NoError(t, err)

	// No GetOrCreate expectation registered — if the impl calls it, the mock will panic.
	err = ti.service.EnableWebhooks(ctx, &gen.EnableWebhooksPayload{})
	require.NoError(t, err)

	org, err := orgrepo.New(ti.conn).GetOrganizationMetadata(ctx, authCtx.ActiveOrganizationID)
	require.NoError(t, err)
	require.Equal(t, testSvixAppID, org.SvixAppID.String)
	require.True(t, org.WebhooksEnabled.Bool)

	count, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionOrganizationWebhooksEnabled)
	require.NoError(t, err)
	require.EqualValues(t, 1, count) // only the call above, not the UpsertSvixAppID seed

	ti.svixSrv.AssertExpectations(t)
}

func TestService_EnableWebhooks_ForbiddenWithoutOrgAdminGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsServiceRBAC(t)
	ctx = authztest.WithExactGrants(t, ctx)

	err := ti.service.EnableWebhooks(ctx, &gen.EnableWebhooksPayload{})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestService_EnableWebhooks_ForbiddenWithGrantForDifferentOrganization(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsServiceRBAC(t)
	ctx = authztest.WithExactGrants(t, ctx, authz.Grant{
		Scope:    authz.ScopeOrgAdmin,
		Selector: authz.NewSelector(authz.ScopeOrgAdmin, "org_other"),
	})

	err := ti.service.EnableWebhooks(ctx, &gen.EnableWebhooksPayload{})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}
