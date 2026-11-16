package toolsets_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/toolsets"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

func TestToolsetsService_ListToolSchemaStaticValues(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)
	deployment := createTodoDeploymentWithDocs(t, ctx, ti, "static-values-deployment", "static-values-doc")

	tools, err := testrepo.New(ti.conn).ListDeploymentHTTPTools(ctx, uuid.MustParse(deployment.Deployment.ID))
	require.NoError(t, err)
	require.NotEmpty(t, tools)

	toolURNs := make([]string, 0, len(tools))
	for _, tool := range tools {
		toolURNs = append(toolURNs, tool.ToolUrn.String())
	}

	toolset, err := ti.service.CreateToolset(ctx, &gen.CreateToolsetPayload{
		SessionToken:           nil,
		ApikeyToken:            nil,
		Name:                   "Static Values",
		Description:            nil,
		ToolUrns:               toolURNs,
		ResourceUrns:           nil,
		DefaultEnvironmentSlug: nil,
		ProjectSlugInput:       nil,
	})
	require.NoError(t, err)

	result, err := ti.service.ListToolSchemaStaticValues(ctx, &gen.ListToolSchemaStaticValuesPayload{
		Slug:             toolset.Slug,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.NotEmpty(t, result.Tools)

	var defaults map[string]string
	for _, tool := range result.Tools {
		if tool.ToolName != "static_values_doc_get_todos" {
			continue
		}
		defaults = map[string]string{}
		for _, value := range tool.Values {
			if value.Keyword == "default" {
				defaults[value.SchemaPath] = value.ValueJSON
			}
		}
	}

	require.Equal(t, map[string]string{
		"/properties/queryParameters/properties/limit":  "20",
		"/properties/queryParameters/properties/offset": "0",
	}, defaults)
}

func TestToolsetsService_ListToolSchemaStaticValues_Empty(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)
	toolset := createMinimalPrivateToolset(t, ctx, ti, "No Static Values")

	result, err := ti.service.ListToolSchemaStaticValues(ctx, &gen.ListToolSchemaStaticValuesPayload{
		Slug:             toolset.Slug,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Empty(t, result.Tools)
	require.NotNil(t, result.Tools)
}

func TestToolsetsService_ListToolSchemaStaticValues_NotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)

	_, err := ti.service.ListToolSchemaStaticValues(ctx, &gen.ListToolSchemaStaticValuesPayload{
		Slug:             "missing-toolset",
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeNotFound, oopsErr.Code)
}

func TestToolsetsService_ListToolSchemaStaticValues_RBAC(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)
	toolset := createMinimalPrivateToolset(t, ctx, ti, "Static Values RBAC")

	deniedCtx := authztest.WithExactGrants(t, ctx)
	_, err := ti.service.ListToolSchemaStaticValues(deniedCtx, &gen.ListToolSchemaStaticValuesPayload{
		Slug:             toolset.Slug,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)

	grantedCtx := authztest.WithExactGrants(t, ctx, authz.Grant{
		Scope:    authz.ScopeMCPRead,
		Selector: authz.NewSelector(authz.ScopeMCPRead, toolset.ID),
	})
	result, err := ti.service.ListToolSchemaStaticValues(grantedCtx, &gen.ListToolSchemaStaticValuesPayload{
		Slug:             toolset.Slug,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.NotNil(t, result.Tools)
}
