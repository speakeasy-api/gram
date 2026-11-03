package mcp

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgsrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/stretchr/testify/require"
)

func TestResolvePlatformThreadContextAuthorization(t *testing.T) {
	t.Parallel()
	db, err := TestInfra.CloneTestDatabase(t, "mcp_platform_thread_context")
	require.NoError(t, err)
	ctx := t.Context()
	orgID := "org-thread-test"
	require.NoError(t, orgsrepo.New(db).CreateOrganizationMetadata(ctx, orgsrepo.CreateOrganizationMetadataParams{ID: orgID, Name: "Thread Test", Slug: "thread-test"}))
	project, err := projectsrepo.New(db).CreateProject(ctx, projectsrepo.CreateProjectParams{Name: "Project", Slug: "project", OrganizationID: orgID})
	require.NoError(t, err)
	queries := assistantrepo.New(db)
	createAssistant := func(name string) uuid.UUID {
		t.Helper()
		row, err := queries.CreateAssistant(ctx, assistantrepo.CreateAssistantParams{ProjectID: project.ID, OrganizationID: orgID, CreatedByUserID: pgtype.Text{}, Name: name, Model: "openai/gpt-4o-mini", Instructions: "", WarmTtlSeconds: 300, MaxConcurrency: 1, Status: "active"})
		require.NoError(t, err)
		return row.ID
	}
	createThread := func(assistantID uuid.UUID) (uuid.UUID, uuid.UUID) {
		t.Helper()
		chatID := uuid.New()
		require.NoError(t, queries.UpsertAssistantChat(ctx, assistantrepo.UpsertAssistantChatParams{ChatID: chatID, ProjectID: project.ID, OrganizationID: orgID, UserID: pgtype.Text{}, Title: pgtype.Text{}}))
		threadID, err := queries.UpsertAssistantThread(ctx, assistantrepo.UpsertAssistantThreadParams{AssistantID: assistantID, ProjectID: project.ID, CorrelationID: uuid.NewString(), ChatID: chatID, SourceKind: "dashboard", SourceRefJson: []byte(`{}`)})
		require.NoError(t, err)
		return threadID, chatID
	}
	firstAssistant, secondAssistant := createAssistant("First"), createAssistant("Second")
	firstThread, firstChat := createThread(firstAssistant)
	siblingThread, siblingChat := createThread(firstAssistant)
	otherThread, otherChat := createThread(secondAssistant)
	logger := testenv.NewLogger(t)
	service := &Service{db: db, logger: logger, authz: authz.NewEngine(logger, db, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient())}
	sessionID := "thread-test-session"
	auth := &contextvalues.AuthContext{ActiveOrganizationID: orgID, ProjectID: &project.ID, UserID: "thread-test-user", SessionID: &sessionID, AccountType: "enterprise"}
	callerCtx := contextvalues.SetAuthContext(ctx, auth)
	allowedCtx := authztest.WithExactGrants(t, callerCtx, authz.NewGrant(authz.ScopeAssistantRead, firstAssistant.String()), authz.NewGrant(authz.ScopeAssistantRead, secondAssistant.String()))
	// One caller may explicitly select siblings and another authorized assistant;
	// there is no new same-thread, creator, or same-assistant restriction.
	for _, target := range []struct{ threadID, assistantID, chatID uuid.UUID }{
		{firstThread, firstAssistant, firstChat},
		{siblingThread, firstAssistant, siblingChat},
		{otherThread, secondAssistant, otherChat},
	} {
		principal, chatID, err := service.resolvePlatformThreadContext(allowedCtx, target.threadID, false)
		require.NoError(t, err)
		require.Equal(t, contextvalues.AssistantPrincipal{AssistantID: target.assistantID, ThreadID: target.threadID}, principal)
		require.Equal(t, target.chatID.String(), chatID)
		gotAuth, ok := contextvalues.GetAuthContext(allowedCtx)
		require.True(t, ok)
		require.Same(t, auth, gotAuth)
		_, hasPrincipal := contextvalues.GetAssistantPrincipal(allowedCtx)
		require.False(t, hasPrincipal)
	}
	for _, grants := range [][]authz.Grant{
		{},
		{authz.NewGrant(authz.ScopeProjectRead, project.ID.String()), authz.NewGrant(authz.ScopeProjectWrite, project.ID.String())},
		{authz.NewGrant(authz.ScopeAssistantRead, firstAssistant.String())},
	} {
		deniedCtx := authztest.WithExactGrants(t, callerCtx, grants...)
		principal, chatID, err := service.resolvePlatformThreadContext(deniedCtx, otherThread, false)
		var denied *oops.ShareableError
		require.ErrorAs(t, err, &denied)
		require.Equal(t, oops.CodeNotFound, denied.Code, "a denial must not reveal that the thread exists")
		require.Zero(t, principal)
		require.Empty(t, chatID)
	}
	// Tools that change state need write access to the target assistant.
	_, _, err = service.resolvePlatformThreadContext(allowedCtx, firstThread, true)
	var readOnly *oops.ShareableError
	require.ErrorAs(t, err, &readOnly)
	require.Equal(t, oops.CodeNotFound, readOnly.Code)
	writeCtx := authztest.WithExactGrants(t, callerCtx, authz.NewGrant(authz.ScopeAssistantWrite, firstAssistant.String()))
	principal, _, err := service.resolvePlatformThreadContext(writeCtx, firstThread, true)
	require.NoError(t, err)
	require.Equal(t, firstThread, principal.ThreadID)
	// Project scoping remains enforced even with an otherwise valid assistant grant.
	otherProjectID := uuid.New()
	wrongProject := contextvalues.SetAuthContext(allowedCtx, &contextvalues.AuthContext{ActiveOrganizationID: orgID, ProjectID: &otherProjectID, UserID: auth.UserID})
	_, _, err = service.resolvePlatformThreadContext(wrongProject, firstThread, false)
	var notFound *oops.ShareableError
	require.ErrorAs(t, err, &notFound)
	require.Equal(t, oops.CodeNotFound, notFound.Code)
	wrongOrganization := contextvalues.SetAuthContext(allowedCtx, &contextvalues.AuthContext{ActiveOrganizationID: "org_other_thread_test", ProjectID: &project.ID, UserID: auth.UserID})
	_, _, err = service.resolvePlatformThreadContext(wrongOrganization, firstThread, false)
	require.ErrorAs(t, err, &notFound)
	require.Equal(t, oops.CodeNotFound, notFound.Code)
	_, _, err = service.resolvePlatformThreadContext(allowedCtx, uuid.Nil, false)
	var badRequest *oops.ShareableError
	require.ErrorAs(t, err, &badRequest)
	require.Equal(t, oops.CodeBadRequest, badRequest.Code)
	_, _, err = service.resolvePlatformThreadContext(ctx, firstThread, false)
	var unauthorized *oops.ShareableError
	require.ErrorAs(t, err, &unauthorized)
	require.Equal(t, oops.CodeUnauthorized, unauthorized.Code)
}
