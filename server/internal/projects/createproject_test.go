package projects_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	pluginsv1 "github.com/speakeasy-api/gram/infra/gen/gram/plugins/v1"
	gen "github.com/speakeasy-api/gram/server/gen/projects"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

func TestProjectsService_CreateProject_CreatesAuditLog(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestProjectsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)
	ctx = withAccessGrants(t, ctx, ti.conn, authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)})

	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionProjectCreate)
	require.NoError(t, err)

	beforeRows, err := testrepo.New(ti.conn).ListPublishOutboxRows(ctx)
	require.NoError(t, err)
	beforeIDs := make(map[int64]bool, len(beforeRows))
	for _, row := range beforeRows {
		beforeIDs[row.ID] = true
	}

	name := "audit-create-project-" + uuid.NewString()[:8]
	result, err := ti.service.CreateProject(ctx, &gen.CreateProjectPayload{
		ApikeyToken:    nil,
		SessionToken:   nil,
		OrganizationID: authCtx.ActiveOrganizationID,
		Name:           name,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.Project)
	require.Equal(t, name, result.Project.Name)

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionProjectCreate)
	require.NoError(t, err)
	require.Equal(t, beforeCount+1, afterCount)

	afterRows, err := testrepo.New(ti.conn).ListPublishOutboxRows(ctx)
	require.NoError(t, err)
	var lifecycleHints []*pluginsv1.RoleProvisioningRequested
	for _, row := range afterRows {
		if beforeIDs[row.ID] || row.Topic != "gram.plugins.v1.RoleProvisioningRequested" {
			continue
		}
		require.Equal(t, authCtx.ActiveOrganizationID, row.OrganizationID)
		hint := new(pluginsv1.RoleProvisioningRequested)
		require.NoError(t, proto.Unmarshal(row.Message, hint))
		lifecycleHints = append(lifecycleHints, hint)
	}
	require.Len(t, lifecycleHints, 1)
	expected := pluginsv1.RoleProvisioningRequested_builder{OrganizationId: new(authCtx.ActiveOrganizationID)}.Build()
	require.True(t, proto.Equal(expected, lifecycleHints[0]), "unexpected lifecycle hint: %s", lifecycleHints[0])
}

func TestProjectsService_CreateProject_ForbiddenDoesNotCreateAuditLog(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestProjectsService(t)
	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionProjectCreate)
	require.NoError(t, err)

	result, err := ti.service.CreateProject(ctx, &gen.CreateProjectPayload{
		ApikeyToken:    nil,
		SessionToken:   nil,
		OrganizationID: "org_not_allowed",
		Name:           "forbidden-project",
	})
	require.Error(t, err)
	require.Nil(t, result)

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionProjectCreate)
	require.NoError(t, err)
	require.Equal(t, beforeCount, afterCount)
}

func TestProjectsService_CreateProject_ForbiddenWithoutOrgAdminGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestProjectsService(t)
	ctx = authz.GrantsToContext(ctx, nil)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	result, err := ti.service.CreateProject(ctx, &gen.CreateProjectPayload{
		ApikeyToken:    nil,
		SessionToken:   nil,
		OrganizationID: authCtx.ActiveOrganizationID,
		Name:           "forbidden-without-org-admin",
	})
	require.Error(t, err)
	require.Nil(t, result)

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestProjectsService_CreateProject_AuditLogRecord(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestProjectsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)
	ctx = withAccessGrants(t, ctx, ti.conn, authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)})

	name := "audit-create-project-record-" + uuid.NewString()[:8]
	result, err := ti.service.CreateProject(ctx, &gen.CreateProjectPayload{
		ApikeyToken:    nil,
		SessionToken:   nil,
		OrganizationID: authCtx.ActiveOrganizationID,
		Name:           name,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.Project)

	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionProjectCreate)
	require.NoError(t, err)
	require.Equal(t, string(audit.ActionProjectCreate), record.Action)
	require.Equal(t, "project", record.SubjectType)
	require.Equal(t, result.Project.Name, record.SubjectDisplay)
	require.Equal(t, string(result.Project.Slug), record.SubjectSlug)
	require.Nil(t, record.BeforeSnapshot)
	require.Nil(t, record.AfterSnapshot)
	require.Nil(t, record.Metadata)
}
