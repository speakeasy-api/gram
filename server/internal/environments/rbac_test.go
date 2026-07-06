package environments_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/environments"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestEnvironments_RBAC_ReadOps_DeniedWithNoGrants(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestEnvironmentService(t)
	ctx = authztest.WithExactGrants(t, ctx)

	_, err := ti.service.ListEnvironments(ctx, &gen.ListEnvironmentsPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestEnvironments_RBAC_ReadOps_AllowedWithBuildReadGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestEnvironmentService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	ctx = authztest.WithExactGrants(t, ctx, authz.Grant{Scope: authz.ScopeProjectRead, Selector: authz.NewSelector(authz.ScopeProjectRead, authCtx.ProjectID.String())})

	_, err := ti.service.ListEnvironments(ctx, &gen.ListEnvironmentsPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
}

func TestEnvironments_RBAC_ReadOps_AllowedWithBuildWriteGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestEnvironmentService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	ctx = authztest.WithExactGrants(t, ctx, authz.Grant{Scope: authz.ScopeProjectWrite, Selector: authz.NewSelector(authz.ScopeProjectWrite, authCtx.ProjectID.String())})

	_, err := ti.service.ListEnvironments(ctx, &gen.ListEnvironmentsPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
}

func TestEnvironments_RBAC_ReadOps_DeniedWithWrongResourceID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestEnvironmentService(t)
	ctx = authztest.WithExactGrants(t, ctx, authz.Grant{Scope: authz.ScopeProjectRead, Selector: authz.NewSelector(authz.ScopeProjectRead, uuid.NewString())})

	_, err := ti.service.ListEnvironments(ctx, &gen.ListEnvironmentsPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestEnvironments_RBAC_WriteOps_DeniedWithNoGrants(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestEnvironmentService(t)
	ctx = authztest.WithExactGrants(t, ctx)

	_, err := ti.service.CreateEnvironment(ctx, &gen.CreateEnvironmentPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		OrganizationID:   "",
		Name:             "rbac-test-env",
		Description:      nil,
		Entries:          []*gen.EnvironmentEntryInput{},
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestEnvironments_RBAC_WriteOps_DeniedWithReadOnlyGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestEnvironmentService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	ctx = authztest.WithExactGrants(t, ctx, authz.Grant{Scope: authz.ScopeProjectRead, Selector: authz.NewSelector(authz.ScopeProjectRead, authCtx.ProjectID.String())})

	_, err := ti.service.CreateEnvironment(ctx, &gen.CreateEnvironmentPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		OrganizationID:   "",
		Name:             "rbac-test-env",
		Description:      nil,
		Entries:          []*gen.EnvironmentEntryInput{},
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestEnvironments_RBAC_WriteOps_AllowedWithEnvironmentWrite(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestEnvironmentService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	ctx = authztest.WithExactGrants(t, ctx, envGrant(authz.ScopeEnvironmentWrite, authCtx.ProjectID.String()))

	_, err := ti.service.CreateEnvironment(ctx, &gen.CreateEnvironmentPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		OrganizationID:   "",
		Name:             "rbac-test-env",
		Description:      nil,
		Entries:          []*gen.EnvironmentEntryInput{},
	})
	require.NoError(t, err)
}

// project:write alone must NOT authorize creating an environment: the scope
// families are intentionally independent because env values include secrets.
func TestEnvironments_RBAC_Create_DeniedWithProjectWriteOnly(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestEnvironmentService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	ctx = authztest.WithExactGrants(t, ctx, authz.Grant{Scope: authz.ScopeProjectWrite, Selector: authz.NewSelector(authz.ScopeProjectWrite, authCtx.ProjectID.String())})

	_, err := ti.service.CreateEnvironment(ctx, &gen.CreateEnvironmentPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		OrganizationID:   "",
		Name:             "rbac-test-env",
		Description:      nil,
		Entries:          []*gen.EnvironmentEntryInput{},
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestEnvironments_RBAC_Update_AllowedWithEnvironmentWrite(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestEnvironmentService(t)

	env, err := ti.service.CreateEnvironment(ctx, &gen.CreateEnvironmentPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		OrganizationID:   "",
		Name:             "rbac-update-env",
		Description:      nil,
		Entries:          []*gen.EnvironmentEntryInput{},
	})
	require.NoError(t, err)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	ctx = authztest.WithExactGrants(t, ctx, envGrant(authz.ScopeEnvironmentWrite, authCtx.ProjectID.String()))

	_, err = ti.service.UpdateEnvironment(ctx, &gen.UpdateEnvironmentPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		Slug:             env.Slug,
		Description:      nil,
		Name:             nil,
		EntriesToUpdate: []*gen.EnvironmentEntryInput{
			{Name: "KEY", Value: "value"},
		},
		EntriesToRemove: []string{},
	})
	require.NoError(t, err)
}

// The customer-facing bug: environment:write alone was rejected because the
// handler gated on project:write. project:write must NOT authorize the update.
func TestEnvironments_RBAC_Update_DeniedWithProjectWriteOnly(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestEnvironmentService(t)

	env, err := ti.service.CreateEnvironment(ctx, &gen.CreateEnvironmentPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		OrganizationID:   "",
		Name:             "rbac-update-pw-env",
		Description:      nil,
		Entries:          []*gen.EnvironmentEntryInput{},
	})
	require.NoError(t, err)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	ctx = authztest.WithExactGrants(t, ctx, authz.Grant{Scope: authz.ScopeProjectWrite, Selector: authz.NewSelector(authz.ScopeProjectWrite, authCtx.ProjectID.String())})

	_, err = ti.service.UpdateEnvironment(ctx, &gen.UpdateEnvironmentPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		Slug:             env.Slug,
		Description:      nil,
		Name:             nil,
		EntriesToUpdate: []*gen.EnvironmentEntryInput{
			{Name: "KEY", Value: "value"},
		},
		EntriesToRemove: []string{},
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestEnvironments_RBAC_Delete_AllowedWithEnvironmentWrite(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestEnvironmentService(t)

	env, err := ti.service.CreateEnvironment(ctx, &gen.CreateEnvironmentPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		OrganizationID:   "",
		Name:             "rbac-delete-env",
		Description:      nil,
		Entries:          []*gen.EnvironmentEntryInput{},
	})
	require.NoError(t, err)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	ctx = authztest.WithExactGrants(t, ctx, envGrant(authz.ScopeEnvironmentWrite, authCtx.ProjectID.String()))

	err = ti.service.DeleteEnvironment(ctx, &gen.DeleteEnvironmentPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		Slug:             env.Slug,
	})
	require.NoError(t, err)
}

// A missing slug must not be an existence oracle: unauthorized callers get
// forbidden whether or not the environment exists.
func TestEnvironments_RBAC_Update_MissingSlug_DeniedWithoutEnvironmentWrite(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestEnvironmentService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	ctx = authztest.WithExactGrants(t, ctx, authz.Grant{Scope: authz.ScopeProjectWrite, Selector: authz.NewSelector(authz.ScopeProjectWrite, authCtx.ProjectID.String())})

	_, err := ti.service.UpdateEnvironment(ctx, &gen.UpdateEnvironmentPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		Slug:             types.Slug("does-not-exist"),
		Description:      nil,
		Name:             nil,
		EntriesToUpdate:  []*gen.EnvironmentEntryInput{},
		EntriesToRemove:  []string{},
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestEnvironments_RBAC_Update_MissingSlug_NotFoundWithEnvironmentWrite(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestEnvironmentService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	ctx = authztest.WithExactGrants(t, ctx, envGrant(authz.ScopeEnvironmentWrite, authCtx.ProjectID.String()))

	_, err := ti.service.UpdateEnvironment(ctx, &gen.UpdateEnvironmentPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		Slug:             types.Slug("does-not-exist"),
		Description:      nil,
		Name:             nil,
		EntriesToUpdate:  []*gen.EnvironmentEntryInput{},
		EntriesToRemove:  []string{},
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeNotFound, oopsErr.Code)
}

// project:write alone must NOT authorize deleting an environment.
func TestEnvironments_RBAC_Delete_DeniedWithProjectWriteOnly(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestEnvironmentService(t)

	env, err := ti.service.CreateEnvironment(ctx, &gen.CreateEnvironmentPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		OrganizationID:   "",
		Name:             "rbac-delete-pw-env",
		Description:      nil,
		Entries:          []*gen.EnvironmentEntryInput{},
	})
	require.NoError(t, err)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	ctx = authztest.WithExactGrants(t, ctx, authz.Grant{Scope: authz.ScopeProjectWrite, Selector: authz.NewSelector(authz.ScopeProjectWrite, authCtx.ProjectID.String())})

	err = ti.service.DeleteEnvironment(ctx, &gen.DeleteEnvironmentPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		Slug:             env.Slug,
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

// The idempotent no-op on a missing slug must not become an existence oracle:
// unauthorized callers get forbidden, not silent success.
func TestEnvironments_RBAC_Delete_MissingSlug_DeniedWithoutEnvironmentWrite(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestEnvironmentService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	ctx = authztest.WithExactGrants(t, ctx, authz.Grant{Scope: authz.ScopeProjectWrite, Selector: authz.NewSelector(authz.ScopeProjectWrite, authCtx.ProjectID.String())})

	err := ti.service.DeleteEnvironment(ctx, &gen.DeleteEnvironmentPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		Slug:             types.Slug("does-not-exist"),
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestEnvironments_RBAC_Delete_MissingSlug_NoopWithEnvironmentWrite(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestEnvironmentService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	ctx = authztest.WithExactGrants(t, ctx, envGrant(authz.ScopeEnvironmentWrite, authCtx.ProjectID.String()))

	err := ti.service.DeleteEnvironment(ctx, &gen.DeleteEnvironmentPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		Slug:             types.Slug("does-not-exist"),
	})
	require.NoError(t, err)
}

// Clone shares the slug-miss authz gate with update/delete.
func TestEnvironments_RBAC_Clone_MissingSlug_DeniedWithoutEnvironmentWrite(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestEnvironmentService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	ctx = authztest.WithExactGrants(t, ctx, authz.Grant{Scope: authz.ScopeProjectWrite, Selector: authz.NewSelector(authz.ScopeProjectWrite, authCtx.ProjectID.String())})

	_, err := ti.service.CloneEnvironment(ctx, &gen.CloneEnvironmentPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		Slug:             types.Slug("does-not-exist"),
		NewName:          "rbac-clone-missing-target",
		CopyValues:       nil,
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}
