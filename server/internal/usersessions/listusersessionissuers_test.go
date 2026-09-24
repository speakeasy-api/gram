package usersessions_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/user_session_issuers"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

func TestListUserSessionIssuers(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	for _, slug := range []string{"a", "b", "c"} {
		_, err := ti.service.CreateUserSessionIssuer(ctx, &gen.CreateUserSessionIssuerPayload{
			SessionToken:         nil,
			ApikeyToken:          nil,
			ProjectSlugInput:     nil,
			Slug:                 "list-" + slug,
			AuthnChallengeMode:   "chain",
			SessionDurationHours: 24,
		})
		require.NoError(t, err)
	}

	got, err := ti.service.ListUserSessionIssuers(ctx, &gen.ListUserSessionIssuersPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		Cursor:           nil,
		Limit:            nil,
		McpResourceID:    nil,
	})
	require.NoError(t, err)
	require.Len(t, got.Items, 3)
	require.Nil(t, got.NextCursor, "non-paged result must not return a cursor")
}

func TestListUserSessionIssuers_BadCursor(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	bad := "not-a-timestamp"
	_, err := ti.service.ListUserSessionIssuers(ctx, &gen.ListUserSessionIssuersPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		Cursor:           &bad,
		Limit:            nil,
		McpResourceID:    nil,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestListUserSessionIssuers_RBACForbidden(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	_, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	// Empty grant set under enterprise enforcement — list must be denied.
	ctx = withExactAuthzGrants(t, ctx, ti.conn)

	_, err := ti.service.ListUserSessionIssuers(ctx, &gen.ListUserSessionIssuersPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		Cursor:           nil,
		Limit:            nil,
		McpResourceID:    nil,
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestListUserSessionIssuers_AllowsProjectScopedMCPWrite(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	selector := authz.NewSelector(authz.ScopeMCPWrite, authz.WildcardResource)
	selector[authz.SelectorKeyProjectID] = authCtx.ProjectID.String()
	ctx = withExactAuthzGrants(t, ctx, ti.conn, authz.NewGrantWithSelector(authz.ScopeMCPWrite, selector))

	got, err := ti.service.ListUserSessionIssuers(ctx, &gen.ListUserSessionIssuersPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		Cursor:           nil,
		Limit:            nil,
		McpResourceID:    nil,
	})
	require.NoError(t, err)
	require.Empty(t, got.Items)
}

func TestListUserSessionIssuers_AllowsResourceScopedMCPWrite(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	toolset := createIssuerListToolset(t, ctx, ti, *authCtx.ProjectID, "resource-scoped-list")
	resourceID := toolset.ID.String()
	ctx = withExactAuthzGrants(t, ctx, ti.conn, authz.NewGrant(authz.ScopeMCPWrite, resourceID))

	got, err := ti.service.ListUserSessionIssuers(ctx, &gen.ListUserSessionIssuersPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		Cursor:           nil,
		Limit:            nil,
		McpResourceID:    &resourceID,
	})
	require.NoError(t, err)
	require.Empty(t, got.Items)
}

func TestListUserSessionIssuers_RejectsResourceFromSiblingProject(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	siblingProjectID := createSiblingProject(t, ctx, ti.conn, "issuer-list-resource-sibling")
	toolset := createIssuerListToolset(t, ctx, ti, siblingProjectID, "issuer-list-resource-sibling")
	resourceID := toolset.ID.String()
	ctx = withExactAuthzGrants(t, ctx, ti.conn, authz.NewGrant(authz.ScopeMCPWrite, resourceID))

	_, err := ti.service.ListUserSessionIssuers(ctx, &gen.ListUserSessionIssuersPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		Cursor:           nil,
		Limit:            nil,
		McpResourceID:    &resourceID,
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestListUserSessionIssuers_ExcludesSiblingProject(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	sp := seedSiblingProject(t, ctx, ti, "list-iss-sibling")

	listed, err := ti.service.ListUserSessionIssuers(ctx, &gen.ListUserSessionIssuersPayload{
		Cursor:           nil,
		Limit:            nil,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		McpResourceID:    nil,
	})
	require.NoError(t, err)
	for _, item := range listed.Items {
		require.NotEqual(t, sp.issuerID.String(), item.ID)
	}
}

func createIssuerListToolset(t *testing.T, ctx context.Context, ti *testInstance, projectID uuid.UUID, slug string) toolsetsrepo.Toolset {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	toolset, err := toolsetsrepo.New(ti.conn).CreateToolset(ctx, toolsetsrepo.CreateToolsetParams{
		OrganizationID:         authCtx.ActiveOrganizationID,
		ProjectID:              projectID,
		Name:                   slug,
		Slug:                   slug,
		Description:            pgtype.Text{String: "", Valid: false},
		DefaultEnvironmentSlug: pgtype.Text{String: "", Valid: false},
		McpSlug:                pgtype.Text{String: "", Valid: false},
		McpEnabled:             false,
	})
	require.NoError(t, err)
	return toolset
}
