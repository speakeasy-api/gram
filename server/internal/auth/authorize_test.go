package auth_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
	"goa.design/goa/v3/security"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	agentsrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	keysrepo "github.com/speakeasy-api/gram/server/internal/keys/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/wide"
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
	ctx = wide.Start(ctx)
	key := createTestAPIKey(t, ctx, instance, &projects[0].ID)

	ctx, err := instance.authorizer.Authorize(ctx, key, apiKeyScheme)
	require.NoError(t, err)
	ctx, err = instance.authorizer.Authorize(ctx, projects[0].Slug, projectSlugScheme)
	require.NoError(t, err)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.Equal(t, projects[0].ID, *authCtx.ProjectID)
	require.Equal(t, projects[0].Slug, *authCtx.ProjectSlug)

	attrCounts := make(map[string]int)
	var apiKeySchemeFound, projectSchemeFound bool
	for _, eventAttr := range wide.Emit(ctx) {
		attrCounts[eventAttr.Key]++
		switch eventAttr.Key {
		case string(attr.RequestAuthSchemeAPIKeyKey):
			apiKeySchemeFound = true
			require.True(t, eventAttr.Value.Bool())
		case string(attr.RequestAuthSchemeProjectKey):
			projectSchemeFound = true
			require.True(t, eventAttr.Value.Bool())
		}
	}
	require.True(t, apiKeySchemeFound)
	require.True(t, projectSchemeFound)
	for _, key := range []string{
		string(attr.RequestAuthOrganizationIDKey),
		string(attr.RequestAuthOrganizationSlugKey),
		string(attr.RequestAuthAccountTypeKey),
		string(attr.RequestAuthAPIKeyIDKey),
		string(attr.RequestAuthProjectIDKey),
		string(attr.RequestAuthProjectSlugKey),
	} {
		require.Equal(t, 1, attrCounts[key], "attribute %q must be emitted once", key)
	}
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

	firstCtx, err := instance.authorizer.Authorize(wide.Start(t.Context()), session.SessionID, sessionScheme)
	require.NoError(t, err)
	firstCtx, err = instance.authorizer.Authorize(firstCtx, projects[0].Slug, projectSlugScheme)
	require.NoError(t, err)
	firstAuthCtx, ok := contextvalues.GetAuthContext(firstCtx)
	require.True(t, ok)
	require.Equal(t, projects[0].ID, *firstAuthCtx.ProjectID)

	for _, eventAttr := range wide.Emit(firstCtx) {
		require.NotEqual(t, session.SessionID, eventAttr.Value.Any(), "session token must not be logged")
	}
	secondCtx, err := instance.authorizer.Authorize(t.Context(), session.SessionID, sessionScheme)
	require.NoError(t, err)
	secondCtx, err = instance.authorizer.Authorize(secondCtx, projects[1].Slug, projectSlugScheme)
	require.NoError(t, err)
	secondAuthCtx, ok := contextvalues.GetAuthContext(secondCtx)
	require.True(t, ok)
	require.Equal(t, projects[1].ID, *secondAuthCtx.ProjectID)
}

func TestAuthorizeOrganizationlessSessionOmitsEmptyWideAttrs(t *testing.T) {
	t.Parallel()

	userInfo := defaultMockUserInfo()
	userInfo.Organizations = nil
	ctx, instance := newTestAuthService(t, userInfo)
	require.NoError(t, instance.createTestUser(ctx, userInfo))

	session := sessions.Session{
		SessionID: "organizationless-session",
		UserID:    userInfo.UserID,
	}
	require.NoError(t, instance.sessionManager.StoreSession(ctx, session))

	ctx = wide.Start(ctx)
	ctx, err := instance.authorizer.Authorize(ctx, session.SessionID, sessionScheme)
	require.NoError(t, err)

	emptyAuthKeys := map[string]struct{}{
		string(attr.RequestAuthOrganizationIDKey):   {},
		string(attr.RequestAuthOrganizationSlugKey): {},
		string(attr.RequestAuthAccountTypeKey):      {},
	}
	var userIDFound bool
	for _, eventAttr := range wide.Emit(ctx) {
		_, isEmptyAuthAttr := emptyAuthKeys[eventAttr.Key]
		require.False(t, isEmptyAuthAttr, "empty attribute %q must be omitted", eventAttr.Key)
		if eventAttr.Key == string(attr.RequestAuthUserIDKey) {
			userIDFound = true
			require.Equal(t, userInfo.UserID, eventAttr.Value.String())
		}
	}
	require.True(t, userIDFound)
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

func TestAuthorizePrincipalAPIKeyUsesLiveAgentAdmission(t *testing.T) {
	t.Parallel()

	ctx, instance, projects := newProjectAccessTest(t, "agent-project")
	userInfo := defaultMockUserInfo()
	organizationID := userInfo.Organizations[0].ID
	ownerUserID := userInfo.UserID
	key := createTestAPIKey(t, ctx, instance, nil)
	keyHash, err := auth.GetAPIKeyHash(key)
	require.NoError(t, err)

	agent, err := agentsrepo.New(instance.conn).CreateAgent(ctx, agentsrepo.CreateAgentParams{
		OrganizationID: organizationID, OwnerUserID: ownerUserID, Name: "Principal key agent",
	})
	require.NoError(t, err)
	projectID := projects[0].ID.String()
	seedUserProjectGrant(t, ctx, instance, organizationID, ownerUserID, projectID)
	seedPrincipalProjectGrant(t, ctx, instance, organizationID, urn.NewPrincipal(urn.PrincipalTypeAgent, agent.ID.String()), projectID)

	policy, err := authz.NewDelegatedPolicyV1([]authz.Grant{authz.NewGrant(authz.ScopeProjectRead, projectID)})
	require.NoError(t, err)
	rawPolicy, err := authz.EncodeDelegatedPolicy(authz.CurrentDelegatedPolicyVersion, policy)
	require.NoError(t, err)
	//nolint:glint // notestingrawsql: AIM-194 owns the future principal-key writer; this exercises the loaded-row admission path only
	_, err = instance.conn.Exec(ctx, `UPDATE api_keys SET scopes = '{}', subject_urn = $1, delegated_grants = $2, delegated_grants_version = $3, expires_at = $4 WHERE key_hash = $5`,
		"agent:"+agent.ID.String(), rawPolicy, int32(authz.CurrentDelegatedPolicyVersion), time.Now().Add(24*time.Hour), keyHash)
	require.NoError(t, err)

	admitted, err := instance.authorizer.Authorize(ctx, key, apiKeyScheme)
	require.NoError(t, err, "principal authorization ignores legacy transport scopes")
	authCtx, ok := contextvalues.GetAuthContext(admitted)
	require.True(t, ok)
	require.Empty(t, authCtx.UserID)
	require.Nil(t, authCtx.Email)
	actor, ok := contextvalues.AuthenticatedActor(admitted)
	require.True(t, ok)
	require.Equal(t, "agent:"+agent.ID.String(), actor.String())
	authorizer, owner, ok := contextvalues.PrincipalCredentialProvenance(admitted)
	require.True(t, ok)
	require.Equal(t, ownerUserID, authorizer)
	require.Equal(t, ownerUserID, owner)
	_, err = instance.authorizer.Authorize(admitted, projects[0].Slug, projectSlugScheme)
	require.NoError(t, err)

	legacyAgentScheme := &security.APIKeyScheme{Name: constants.KeySecurityScheme, RequiredScopes: []string{"agent"}}
	_, err = instance.authorizer.Authorize(ctx, key, legacyAgentScheme)
	var legacyRouteErr *oops.ShareableError
	require.ErrorAs(t, err, &legacyRouteErr)
	require.Equal(t, oops.CodeForbidden, legacyRouteErr.Code, "principal credentials cannot enter legacy scope-only agent routes")

	_, err = agentsrepo.New(instance.conn).SuspendAgent(ctx, agentsrepo.SuspendAgentParams{OrganizationID: organizationID, ID: agent.ID})
	require.NoError(t, err)
	_, err = instance.authorizer.Authorize(ctx, key, apiKeyScheme)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeUnauthorized, oopsErr.Code)

	_, err = agentsrepo.New(instance.conn).ResumeAgent(ctx, agentsrepo.ResumeAgentParams{OrganizationID: organizationID, ID: agent.ID})
	require.NoError(t, err)
	_, err = instance.authorizer.Authorize(ctx, key, apiKeyScheme)
	require.NoError(t, err)
	apiKey, err := keysrepo.New(instance.conn).GetAPIKeyByKeyHash(ctx, keyHash)
	require.NoError(t, err)
	_, err = keysrepo.New(instance.conn).DeleteAgentAPIKey(ctx, keysrepo.DeleteAgentAPIKeyParams{ID: apiKey.ID, OrganizationID: organizationID, SubjectUrn: pgtype.Text{String: apiKey.SubjectUrn.String, Valid: true}})
	require.NoError(t, err)

	start := make(chan struct{})
	results := make(chan error, 32)
	var workers sync.WaitGroup
	for range 32 {
		workers.Go(func() {
			<-start
			_, err := instance.authorizer.Authorize(ctx, key, apiKeyScheme)
			results <- err
		})
	}
	close(start)
	workers.Wait()
	close(results)
	for err := range results {
		require.ErrorAs(t, err, &oopsErr, "no admission may succeed after direct credential revocation commits")
		require.Equal(t, oops.CodeUnauthorized, oopsErr.Code)
	}
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

func seedPrincipalProjectGrant(t *testing.T, ctx context.Context, instance *testInstance, organizationID string, principal urn.Principal, projectID string) {
	t.Helper()

	selectors, err := authz.NewSelector(authz.ScopeProjectRead, projectID).MarshalJSON()
	require.NoError(t, err)
	_, err = accessrepo.New(instance.conn).UpsertPrincipalGrant(ctx, accessrepo.UpsertPrincipalGrantParams{
		OrganizationID: organizationID,
		PrincipalUrn:   principal,
		Scope:          string(authz.ScopeProjectRead),
		Selectors:      selectors,
	})
	require.NoError(t, err)
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
