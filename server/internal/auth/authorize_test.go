package auth_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"goa.design/goa/v3/security"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	keysrepo "github.com/speakeasy-api/gram/server/internal/keys/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

var (
	apiKeyScheme = &security.APIKeyScheme{
		Name:           constants.KeySecurityScheme,
		Scopes:         []string{},
		RequiredScopes: []string{"producer"},
	}
	projectSlugScheme = &security.APIKeyScheme{
		Name:           constants.ProjectSlugSecuritySchema,
		Scopes:         []string{},
		RequiredScopes: []string{},
	}
	sessionScheme = &security.APIKeyScheme{
		Name:           constants.SessionSecurityScheme,
		Scopes:         []string{},
		RequiredScopes: []string{},
	}
)

func TestAuthorizeProjectBoundKeyAllowsBoundProjectSlug(t *testing.T) {
	t.Parallel()

	ctx, instance, projects := newProjectAccessTest(t, "bound-project")
	key := createTestAPIKey(t, ctx, instance, &projects[0].ID)

	ctx, err := instance.authorizer.Authorize(ctx, key, apiKeyScheme)
	require.NoError(t, err)
	ctx, err = instance.authorizer.Authorize(ctx, projects[0].Slug, projectSlugScheme)
	require.NoError(t, err)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.Equal(t, projects[0].ID, *authCtx.ProjectID)
	require.Equal(t, projects[0].Slug, *authCtx.ProjectSlug)
}

func TestAuthorizeProjectBoundKeyRejectsSiblingProjectSlugWithoutRepointing(t *testing.T) {
	t.Parallel()

	ctx, instance, projects := newProjectAccessTest(t, "bound-project", "sibling-project")
	key := createTestAPIKey(t, ctx, instance, &projects[0].ID)

	ctx, err := instance.authorizer.Authorize(ctx, key, apiKeyScheme)
	require.NoError(t, err)
	ctx, err = instance.authorizer.Authorize(ctx, projects[1].Slug, projectSlugScheme)

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.Equal(t, projects[0].ID, *authCtx.ProjectID)
	require.Nil(t, authCtx.ProjectSlug)
}

func TestAuthorizeOrganizationWideKeyAllowsProjectSlug(t *testing.T) {
	t.Parallel()

	ctx, instance, projects := newProjectAccessTest(t, "first-project", "second-project")
	key := createTestAPIKey(t, ctx, instance, nil)

	ctx, err := instance.authorizer.Authorize(ctx, key, apiKeyScheme)
	require.NoError(t, err)
	ctx, err = instance.authorizer.Authorize(ctx, projects[1].Slug, projectSlugScheme)
	require.NoError(t, err)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.Equal(t, projects[1].ID, *authCtx.ProjectID)
	require.Equal(t, projects[1].Slug, *authCtx.ProjectSlug)
}

func TestAuthorizeSessionCanSelectGrantedOrganizationProjects(t *testing.T) {
	t.Parallel()

	ctx, instance, projects := newProjectAccessTest(t, "first-project", "second-project")
	userInfo := defaultMockUserInfo()
	seedUserProjectGrant(t, ctx, instance, userInfo.Organizations[0].ID, userInfo.UserID, projects[0].ID.String())
	seedUserProjectGrant(t, ctx, instance, userInfo.Organizations[0].ID, userInfo.UserID, projects[1].ID.String())

	session := sessions.Session{
		SessionID:            "project-access-session",
		ActiveOrganizationID: userInfo.Organizations[0].ID,
		UserID:               userInfo.UserID,
		WorkOSSessionID:      "workos-project-access-session",
		ImpersonatorEmail:    "",
	}
	require.NoError(t, instance.sessionManager.StoreSession(ctx, session))

	firstCtx, err := instance.authorizer.Authorize(t.Context(), session.SessionID, sessionScheme)
	require.NoError(t, err)
	firstCtx, err = instance.authorizer.Authorize(firstCtx, projects[0].Slug, projectSlugScheme)
	require.NoError(t, err)
	firstAuthCtx, ok := contextvalues.GetAuthContext(firstCtx)
	require.True(t, ok)
	require.Equal(t, projects[0].ID, *firstAuthCtx.ProjectID)

	secondCtx, err := instance.authorizer.Authorize(t.Context(), session.SessionID, sessionScheme)
	require.NoError(t, err)
	secondCtx, err = instance.authorizer.Authorize(secondCtx, projects[1].Slug, projectSlugScheme)
	require.NoError(t, err)
	secondAuthCtx, ok := contextvalues.GetAuthContext(secondCtx)
	require.True(t, ok)
	require.Equal(t, projects[1].ID, *secondAuthCtx.ProjectID)
}

func TestAuthorizeSessionRejectsUngrantedOrganizationProject(t *testing.T) {
	t.Parallel()

	ctx, instance, projects := newProjectAccessTest(t, "granted-project", "ungranted-project")
	userInfo := defaultMockUserInfo()
	seedUserProjectGrant(t, ctx, instance, userInfo.Organizations[0].ID, userInfo.UserID, projects[0].ID.String())

	session := sessions.Session{
		SessionID:            "project-access-session-ungranted",
		ActiveOrganizationID: userInfo.Organizations[0].ID,
		UserID:               userInfo.UserID,
		WorkOSSessionID:      "workos-project-access-session-ungranted",
		ImpersonatorEmail:    "",
	}
	require.NoError(t, instance.sessionManager.StoreSession(ctx, session))

	authedCtx, err := instance.authorizer.Authorize(t.Context(), session.SessionID, sessionScheme)
	require.NoError(t, err)

	_, err = instance.authorizer.Authorize(authedCtx, projects[0].Slug, projectSlugScheme)
	require.NoError(t, err)

	_, err = instance.authorizer.Authorize(authedCtx, projects[1].Slug, projectSlugScheme)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestAuthorizeProjectBoundKeyAllowsEmptySlugForSingleProjectOrganization(t *testing.T) {
	t.Parallel()

	ctx, instance, projects := newProjectAccessTest(t, "only-project")
	key := createTestAPIKey(t, ctx, instance, &projects[0].ID)

	ctx, err := instance.authorizer.Authorize(ctx, key, apiKeyScheme)
	require.NoError(t, err)
	ctx, err = instance.authorizer.Authorize(ctx, "", projectSlugScheme)
	require.NoError(t, err)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.Equal(t, projects[0].ID, *authCtx.ProjectID)
	require.Equal(t, projects[0].Slug, *authCtx.ProjectSlug)
}

func newProjectAccessTest(t *testing.T, projectSlugs ...string) (context.Context, *testInstance, []projectsrepo.Project) {
	t.Helper()

	userInfo := defaultMockUserInfo()
	ctx, instance := newTestAuthService(t, userInfo)
	require.NoError(t, instance.createTestUser(ctx, userInfo))
	require.NoError(t, instance.createTestOrganization(ctx, userInfo.Organizations[0], userInfo.UserID))

	projects := make([]projectsrepo.Project, 0, len(projectSlugs))
	for _, slug := range projectSlugs {
		project, err := instance.createTestProject(ctx, userInfo.Organizations[0].ID, slug, slug)
		require.NoError(t, err)
		projects = append(projects, project)
	}

	return ctx, instance, projects
}

func createTestAPIKey(t *testing.T, ctx context.Context, instance *testInstance, projectID *uuid.UUID) string {
	t.Helper()

	key := "gram_local_" + uuid.NewString()
	keyHash, err := auth.GetAPIKeyHash(key)
	require.NoError(t, err)

	var nullableProjectID uuid.NullUUID
	if projectID != nil {
		nullableProjectID = uuid.NullUUID{UUID: *projectID, Valid: true}
	}

	userInfo := defaultMockUserInfo()
	_, err = keysrepo.New(instance.conn).CreateAPIKey(ctx, keysrepo.CreateAPIKeyParams{
		OrganizationID:  userInfo.Organizations[0].ID,
		ProjectID:       nullableProjectID,
		CreatedByUserID: userInfo.UserID,
		Name:            "project-access-key",
		KeyPrefix:       key[:16],
		KeyHash:         keyHash,
		Scopes:          []string{"producer"},
	})
	require.NoError(t, err)

	return key
}

func seedUserProjectGrant(t *testing.T, ctx context.Context, instance *testInstance, organizationID string, userID string, projectID string) {
	t.Helper()

	selectors, err := authz.NewSelector(authz.ScopeProjectRead, projectID).MarshalJSON()
	require.NoError(t, err)

	_, err = accessrepo.New(instance.conn).UpsertPrincipalGrant(ctx, accessrepo.UpsertPrincipalGrantParams{
		OrganizationID: organizationID,
		PrincipalUrn:   urn.NewPrincipal(urn.PrincipalTypeUser, userID),
		Scope:          string(authz.ScopeProjectRead),
		Selectors:      selectors,
	})
	require.NoError(t, err)
}
