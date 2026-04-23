package templates_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/templates"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestTemplates_RBAC_ReadOps_DeniedWithNoGrants(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestTemplateService(t)
	ctx = authz.WithExactGrants(t, ctx)

	_, err := ti.service.ListTemplates(ctx, &gen.ListTemplatesPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestTemplates_RBAC_ReadOps_AllowedWithBuildReadGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestTemplateService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	ctx = authz.WithExactGrants(t, ctx, authz.Grant{Scope: authz.ScopeProjectRead, Resource: authCtx.ProjectID.String()})

	_, err := ti.service.ListTemplates(ctx, &gen.ListTemplatesPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
}

func TestTemplates_RBAC_ReadOps_AllowedWithBuildWriteGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestTemplateService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	ctx = authz.WithExactGrants(t, ctx, authz.Grant{Scope: authz.ScopeProjectWrite, Resource: authCtx.ProjectID.String()})

	_, err := ti.service.ListTemplates(ctx, &gen.ListTemplatesPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
}

func TestTemplates_RBAC_ReadOps_DeniedWithWrongResourceID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestTemplateService(t)
	ctx = authz.WithExactGrants(t, ctx, authz.Grant{Scope: authz.ScopeProjectRead, Resource: uuid.NewString()})

	_, err := ti.service.ListTemplates(ctx, &gen.ListTemplatesPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestTemplates_RBAC_WriteOps_DeniedWithNoGrants(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestTemplateService(t)
	ctx = authz.WithExactGrants(t, ctx)

	_, err := ti.service.CreateTemplate(ctx, &gen.CreateTemplatePayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		Name:             "test-template",
		Prompt:           "hello",
		Description:      nil,
		Arguments:        nil,
		Engine:           "",
		Kind:             "",
		ToolsHint:        nil,
		ToolUrnsHint:     nil,
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestTemplates_RBAC_WriteOps_DeniedWithReadOnlyGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestTemplateService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	ctx = authz.WithExactGrants(t, ctx, authz.Grant{Scope: authz.ScopeProjectRead, Resource: authCtx.ProjectID.String()})

	_, err := ti.service.CreateTemplate(ctx, &gen.CreateTemplatePayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		Name:             "test-template",
		Prompt:           "hello",
		Description:      nil,
		Arguments:        nil,
		Engine:           "",
		Kind:             "",
		ToolsHint:        nil,
		ToolUrnsHint:     nil,
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestTemplates_RBAC_WriteOps_AllowedWithBuildWriteGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestTemplateService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	ctx = authz.WithExactGrants(t, ctx, authz.Grant{Scope: authz.ScopeProjectWrite, Resource: authCtx.ProjectID.String()})

	name := "nonexistent-template"
	err := ti.service.DeleteTemplate(ctx, &gen.DeleteTemplatePayload{
		ID:               nil,
		Name:             &name,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
}
