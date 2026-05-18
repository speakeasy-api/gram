package collections_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/collections"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// --- Read operations ---

func TestCollections_RBAC_List_DeniedWithNoGrants(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestCollectionsService(t)
	ctx = authztest.WithExactGrants(t, ctx)

	_, err := ti.service.List(ctx, &gen.ListPayload{})

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestCollections_RBAC_List_AllowedWithOrgReadGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestCollectionsService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	ctx = authztest.WithExactGrants(t, ctx, authz.Grant{
		Scope:    authz.ScopeOrgRead,
		Selector: authz.NewSelector(authz.ScopeOrgRead, authCtx.ActiveOrganizationID),
	})

	result, err := ti.service.List(ctx, &gen.ListPayload{})
	require.NoError(t, err)
	require.NotNil(t, result)
}

func TestCollections_RBAC_ListServers_DeniedWithNoGrants(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestCollectionsService(t)

	collection := createCollection(t, ctx, ti, "rbac-ls-test", "rbac-ls-test", "com.test.rbac-ls")

	ctx = authztest.WithExactGrants(t, ctx)

	_, err := ti.service.ListServers(ctx, &gen.ListServersPayload{CollectionSlug: collection.Slug})

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestCollections_RBAC_ListServers_AllowedWithOrgReadGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestCollectionsService(t)

	collection := createCollection(t, ctx, ti, "rbac-ls-ok", "rbac-ls-ok", "com.test.rbac-ls-ok")

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	ctx = authztest.WithExactGrants(t, ctx, authz.Grant{
		Scope:    authz.ScopeOrgRead,
		Selector: authz.NewSelector(authz.ScopeOrgRead, authCtx.ActiveOrganizationID),
	})

	result, err := ti.service.ListServers(ctx, &gen.ListServersPayload{CollectionSlug: collection.Slug})
	require.NoError(t, err)
	require.NotNil(t, result)
}

// --- Write operations ---

func TestCollections_RBAC_Create_DeniedWithNoGrants(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestCollectionsService(t)
	ctx = authztest.WithExactGrants(t, ctx)

	_, err := ti.service.Create(ctx, &gen.CreatePayload{
		Name:                 "rbac-create-test",
		Slug:                 "rbac-create-test",
		McpRegistryNamespace: "com.test.rbac-create",
		Visibility:           "private",
		ToolsetIds:           []string{},
	})

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestCollections_RBAC_Create_DeniedWithOrgReadGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestCollectionsService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	ctx = authztest.WithExactGrants(t, ctx, authz.Grant{
		Scope:    authz.ScopeOrgRead,
		Selector: authz.NewSelector(authz.ScopeOrgRead, authCtx.ActiveOrganizationID),
	})

	_, err := ti.service.Create(ctx, &gen.CreatePayload{
		Name:                 "rbac-create-ro",
		Slug:                 "rbac-create-ro",
		McpRegistryNamespace: "com.test.rbac-create-ro",
		Visibility:           "private",
		ToolsetIds:           []string{},
	})

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestCollections_RBAC_Create_AllowedWithOrgAdminGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestCollectionsService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	ctx = authztest.WithExactGrants(t, ctx, authz.Grant{
		Scope:    authz.ScopeOrgAdmin,
		Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID),
	})

	result, err := ti.service.Create(ctx, &gen.CreatePayload{
		Name:                 "rbac-create-ok",
		Slug:                 "rbac-create-ok",
		McpRegistryNamespace: "com.test.rbac-create-ok",
		Visibility:           "private",
		ToolsetIds:           []string{},
	})
	require.NoError(t, err)
	require.Equal(t, "rbac-create-ok", result.Name)
}

func TestCollections_RBAC_Update_DeniedWithNoGrants(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestCollectionsService(t)

	collection := createCollection(t, ctx, ti, "rbac-update-test", "rbac-update-test", "com.test.rbac-update")

	ctx = authztest.WithExactGrants(t, ctx)

	newName := "updated"
	_, err := ti.service.Update(ctx, &gen.UpdatePayload{
		CollectionID: collection.ID,
		Name:         &newName,
	})

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestCollections_RBAC_Update_AllowedWithOrgAdminGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestCollectionsService(t)

	collection := createCollection(t, ctx, ti, "rbac-update-ok", "rbac-update-ok", "com.test.rbac-update-ok")

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	ctx = authztest.WithExactGrants(t, ctx, authz.Grant{
		Scope:    authz.ScopeOrgAdmin,
		Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID),
	})

	newName := "updated"
	result, err := ti.service.Update(ctx, &gen.UpdatePayload{
		CollectionID: collection.ID,
		Name:         &newName,
	})
	require.NoError(t, err)
	require.Equal(t, "updated", result.Name)
}

func TestCollections_RBAC_Delete_DeniedWithNoGrants(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestCollectionsService(t)

	collection := createCollection(t, ctx, ti, "rbac-delete-test", "rbac-delete-test", "com.test.rbac-delete")

	ctx = authztest.WithExactGrants(t, ctx)

	err := ti.service.Delete(ctx, &gen.DeletePayload{CollectionID: collection.ID})

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestCollections_RBAC_Delete_AllowedWithOrgAdminGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestCollectionsService(t)

	collection := createCollection(t, ctx, ti, "rbac-delete-ok", "rbac-delete-ok", "com.test.rbac-delete-ok")

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	ctx = authztest.WithExactGrants(t, ctx, authz.Grant{
		Scope:    authz.ScopeOrgAdmin,
		Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID),
	})

	err := ti.service.Delete(ctx, &gen.DeletePayload{CollectionID: collection.ID})
	require.NoError(t, err)
}

func TestCollections_RBAC_AttachServer_DeniedWithNoGrants(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestCollectionsService(t)

	collection := createCollection(t, ctx, ti, "rbac-attach-test", "rbac-attach-test", "com.test.rbac-attach")
	toolset := createMCPEnabledToolset(t, ctx, ti, "rbac-attach-ts", "")

	ctx = authztest.WithExactGrants(t, ctx)

	_, err := ti.service.AttachServer(ctx, &gen.AttachServerPayload{
		CollectionID: collection.ID,
		ToolsetID:    toolset.ID,
	})

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestCollections_RBAC_AttachServer_AllowedWithOrgAdminGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestCollectionsService(t)

	collection := createCollection(t, ctx, ti, "rbac-attach-ok", "rbac-attach-ok", "com.test.rbac-attach-ok")
	toolset := createMCPEnabledToolset(t, ctx, ti, "rbac-attach-ok-ts", "")

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	ctx = authztest.WithExactGrants(t, ctx, authz.Grant{
		Scope:    authz.ScopeOrgAdmin,
		Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID),
	})

	result, err := ti.service.AttachServer(ctx, &gen.AttachServerPayload{
		CollectionID: collection.ID,
		ToolsetID:    toolset.ID,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
}

func TestCollections_RBAC_DetachServer_DeniedWithNoGrants(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestCollectionsService(t)

	collection := createCollection(t, ctx, ti, "rbac-detach-test", "rbac-detach-test", "com.test.rbac-detach")
	toolset := createMCPEnabledToolset(t, ctx, ti, "rbac-detach-ts", "")

	_, err := ti.service.AttachServer(ctx, &gen.AttachServerPayload{
		CollectionID: collection.ID,
		ToolsetID:    toolset.ID,
	})
	require.NoError(t, err)

	ctx = authztest.WithExactGrants(t, ctx)

	err = ti.service.DetachServer(ctx, &gen.DetachServerPayload{
		CollectionID: collection.ID,
		ToolsetID:    toolset.ID,
	})

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestCollections_RBAC_DetachServer_AllowedWithOrgAdminGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestCollectionsService(t)

	collection := createCollection(t, ctx, ti, "rbac-detach-ok", "rbac-detach-ok", "com.test.rbac-detach-ok")
	toolset := createMCPEnabledToolset(t, ctx, ti, "rbac-detach-ok-ts", "")

	_, err := ti.service.AttachServer(ctx, &gen.AttachServerPayload{
		CollectionID: collection.ID,
		ToolsetID:    toolset.ID,
	})
	require.NoError(t, err)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	ctx = authztest.WithExactGrants(t, ctx, authz.Grant{
		Scope:    authz.ScopeOrgAdmin,
		Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID),
	})

	err = ti.service.DetachServer(ctx, &gen.DetachServerPayload{
		CollectionID: collection.ID,
		ToolsetID:    toolset.ID,
	})
	require.NoError(t, err)
}

// --- Wrong resource ID ---

func TestCollections_RBAC_List_DeniedWithWrongOrgID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestCollectionsService(t)

	ctx = authztest.WithExactGrants(t, ctx, authz.Grant{
		Scope:    authz.ScopeOrgRead,
		Selector: authz.NewSelector(authz.ScopeOrgRead, uuid.NewString()),
	})

	_, err := ti.service.List(ctx, &gen.ListPayload{})

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestCollections_RBAC_Create_DeniedWithWrongOrgID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestCollectionsService(t)

	ctx = authztest.WithExactGrants(t, ctx, authz.Grant{
		Scope:    authz.ScopeOrgAdmin,
		Selector: authz.NewSelector(authz.ScopeOrgAdmin, uuid.NewString()),
	})

	_, err := ti.service.Create(ctx, &gen.CreatePayload{
		Name:                 "rbac-wrong-org",
		Slug:                 "rbac-wrong-org",
		McpRegistryNamespace: "com.test.rbac-wrong-org",
		Visibility:           "private",
		ToolsetIds:           []string{},
	})

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}
