package toolsets_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/toolsets"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	environmentsRepo "github.com/speakeasy-api/gram/server/internal/environments/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func ensureProjectEnvironment(t *testing.T, ctx context.Context, ti *testInstance, slug string) environmentsRepo.Environment {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	queries := environmentsRepo.New(ti.conn)
	existing, err := queries.GetEnvironmentBySlug(ctx, environmentsRepo.GetEnvironmentBySlugParams{Slug: slug, ProjectID: *authCtx.ProjectID})
	if err == nil {
		return existing
	}
	require.ErrorIs(t, err, pgx.ErrNoRows)

	created, err := queries.CreateEnvironment(ctx, environmentsRepo.CreateEnvironmentParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      *authCtx.ProjectID,
		Name:           slug,
		Slug:           slug,
		Description:    pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err)
	return created
}

func projectMCPWriteGrant(t *testing.T, ctx context.Context) authz.Grant {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	return authz.Grant{Scope: authz.ScopeMCPWrite, Selector: authz.NewSelector(authz.ScopeMCPWrite, authCtx.ProjectID.String())}
}

func createToolsetWithDefaultEnvironment(ctx context.Context, ti *testInstance, name string, slug *string) (*types.Toolset, error) {
	toolset, err := ti.service.CreateToolset(ctx, &gen.CreateToolsetPayload{
		SessionToken:           nil,
		ApikeyToken:            nil,
		ProjectSlugInput:       nil,
		Name:                   name,
		Description:            nil,
		ToolUrns:               []string{},
		ResourceUrns:           []string{},
		DefaultEnvironmentSlug: (*types.Slug)(slug),
	})
	if err != nil {
		return nil, fmt.Errorf("create toolset: %w", err)
	}
	return toolset, nil
}

func updateToolsetDefaultEnvironment(ctx context.Context, ti *testInstance, toolsetSlug types.Slug, slug string) (*types.Toolset, error) {
	toolset, err := ti.service.UpdateToolset(ctx, &gen.UpdateToolsetPayload{
		SessionToken:           nil,
		ApikeyToken:            nil,
		ProjectSlugInput:       nil,
		Slug:                   toolsetSlug,
		Name:                   nil,
		Description:            nil,
		DefaultEnvironmentSlug: (*types.Slug)(&slug),
		ToolUrns:               nil,
		ResourceUrns:           nil,
		PromptTemplateNames:    nil,
		McpSlug:                nil,
		McpEnabled:             nil,
		McpIsPublic:            nil,
		CustomDomainID:         nil,
	})
	if err != nil {
		return nil, fmt.Errorf("update toolset: %w", err)
	}
	return toolset, nil
}

func requireForbidden(t *testing.T, err error) {
	t.Helper()

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestToolsets_RBAC_Create_DefaultEnvironmentRequiresEnvironmentRead(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)
	env := ensureProjectEnvironment(t, ctx, ti, "rbac-create-env")

	denied := authztest.WithExactGrants(t, ctx, projectMCPWriteGrant(t, ctx))
	_, err := createToolsetWithDefaultEnvironment(denied, ti, "rbac-default-env-denied", &env.Slug)
	requireForbidden(t, err)

	allowed := authztest.WithExactGrants(t, ctx, projectMCPWriteGrant(t, ctx), authz.NewGrant(authz.ScopeEnvironmentRead, env.ID.String()))
	created, err := createToolsetWithDefaultEnvironment(allowed, ti, "rbac-default-env-allowed", &env.Slug)
	require.NoError(t, err)
	require.NotNil(t, created.DefaultEnvironmentSlug)
	require.Equal(t, env.Slug, string(*created.DefaultEnvironmentSlug))
}

func TestToolsets_RBAC_Create_DefaultEnvironmentAutofillRequiresEnvironmentRead(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)
	env := ensureProjectEnvironment(t, ctx, ti, "default")

	denied := authztest.WithExactGrants(t, ctx, projectMCPWriteGrant(t, ctx))
	created, err := createToolsetWithDefaultEnvironment(denied, ti, "rbac-autofill-denied", nil)
	require.NoError(t, err)
	require.Nil(t, created.DefaultEnvironmentSlug)

	allowed := authztest.WithExactGrants(t, ctx, projectMCPWriteGrant(t, ctx), authz.NewGrant(authz.ScopeEnvironmentRead, env.ID.String()))
	created, err = createToolsetWithDefaultEnvironment(allowed, ti, "rbac-autofill-allowed", nil)
	require.NoError(t, err)
	require.NotNil(t, created.DefaultEnvironmentSlug)
	require.Equal(t, "default", string(*created.DefaultEnvironmentSlug))
}

func TestToolsets_RBAC_Update_ChangingDefaultEnvironmentRequiresEnvironmentRead(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)
	current := ensureProjectEnvironment(t, ctx, ti, "rbac-update-current-env")
	next := ensureProjectEnvironment(t, ctx, ti, "rbac-update-next-env")
	toolset, err := createToolsetWithDefaultEnvironment(ctx, ti, "rbac-update-default-env", &current.Slug)
	require.NoError(t, err)

	writer := authztest.WithExactGrants(t, ctx, authz.Grant{Scope: authz.ScopeMCPWrite, Selector: authz.NewSelector(authz.ScopeMCPWrite, toolset.ID)})

	_, err = updateToolsetDefaultEnvironment(writer, ti, toolset.Slug, next.Slug)
	requireForbidden(t, err)

	// Resubmitting the current default selects nothing new.
	_, err = updateToolsetDefaultEnvironment(writer, ti, toolset.Slug, current.Slug)
	require.NoError(t, err)

	reader := authztest.WithExactGrants(t, ctx, authz.Grant{Scope: authz.ScopeMCPWrite, Selector: authz.NewSelector(authz.ScopeMCPWrite, toolset.ID)}, authz.NewGrant(authz.ScopeEnvironmentRead, next.ID.String()))
	updated, err := updateToolsetDefaultEnvironment(reader, ti, toolset.Slug, next.Slug)
	require.NoError(t, err)
	require.NotNil(t, updated.DefaultEnvironmentSlug)
	require.Equal(t, next.Slug, string(*updated.DefaultEnvironmentSlug))
}

func TestToolsets_RBAC_Clone_CopiesDefaultEnvironmentOnlyWhenReadable(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)
	env := ensureProjectEnvironment(t, ctx, ti, "rbac-clone-env")
	original, err := createToolsetWithDefaultEnvironment(ctx, ti, "rbac-clone-source", &env.Slug)
	require.NoError(t, err)

	clone := func(ctx context.Context) *types.Toolset {
		t.Helper()
		cloned, err := ti.service.CloneToolset(ctx, &gen.CloneToolsetPayload{
			Slug:             original.Slug,
			SessionToken:     nil,
			ApikeyToken:      nil,
			ProjectSlugInput: nil,
		})
		require.NoError(t, err)
		return cloned
	}
	cloneGrants := []authz.Grant{projectMCPWriteGrant(t, ctx), {Scope: authz.ScopeMCPRead, Selector: authz.NewSelector(authz.ScopeMCPRead, original.ID)}}

	require.Nil(t, clone(authztest.WithExactGrants(t, ctx, cloneGrants...)).DefaultEnvironmentSlug)

	readable := clone(authztest.WithExactGrants(t, ctx, append(cloneGrants, authz.NewGrant(authz.ScopeEnvironmentRead, env.ID.String()))...))
	require.NotNil(t, readable.DefaultEnvironmentSlug)
	require.Equal(t, env.Slug, string(*readable.DefaultEnvironmentSlug))
}
