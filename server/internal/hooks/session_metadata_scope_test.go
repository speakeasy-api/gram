package hooks

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	projectsRepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
)

// siblingProjectContext switches ctx to a second, real project in the same
// organization, for flows that write rows keyed to the project.
func siblingProjectContext(t *testing.T, ctx context.Context, ti *testInstance) context.Context {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	slug := "sibling-" + uuid.NewString()[:8]
	project, err := projectsRepo.New(ti.conn).CreateProject(ctx, projectsRepo.CreateProjectParams{
		Name:           slug,
		Slug:           slug,
		OrganizationID: authCtx.ActiveOrganizationID,
	})
	require.NoError(t, err)
	sibling := *authCtx
	sibling.ProjectID = &project.ID
	sibling.ProjectSlug = &project.Slug
	return contextvalues.SetAuthContext(ctx, &sibling)
}

func testSessionMetadata(t *testing.T, ctx context.Context, sessionID string) SessionMetadata {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	return SessionMetadata{
		SessionID:           sessionID,
		ServiceName:         "claude-code",
		UserEmail:           "owner@example.com",
		UserID:              "",
		Provider:            providerAnthropic,
		ExternalOrgID:       "claude-org-owner",
		ExternalAccountUUID: "",
		ExternalAccountID:   "",
		DeviceID:            "owner-device",
		Hostname:            "",
		Cwd:                 "",
		AccountType:         "",
		BillingMode:         "",
		UserAccountID:       "",
		ObservedUserEmail:   "",
		GramOrgID:           authCtx.ActiveOrganizationID,
		ProjectID:           testProjectID(t, ctx),
	}
}

func TestGetSessionMetadata_OtherProjectIsMissForAuthenticatedReader(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)

	otherCtx := otherProjectContext(t, ctx)
	sessionID := uuid.NewString()
	require.NoError(t, ti.service.cacheSessionMetadata(otherCtx, testSessionMetadata(t, otherCtx, sessionID)))

	_, err := ti.service.getSessionMetadata(ctx, sessionID)
	require.ErrorIs(t, err, errSessionMetadataOtherProject, "another project's identity is never lent out")

	// An unauthenticated Claude hook only has the session id to go on.
	_, err = ti.service.getSessionMetadata(t.Context(), sessionID)
	require.NoError(t, err)
}

func TestCacheSessionMetadata_RefusesOtherProjectOverwrite(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	otherCtx := otherProjectContext(t, ctx)

	sessionID := uuid.NewString()
	require.NoError(t, ti.service.cacheSessionMetadata(ctx, testSessionMetadata(t, ctx, sessionID)))
	require.ErrorIs(t, ti.service.cacheSessionMetadata(otherCtx, testSessionMetadata(t, otherCtx, sessionID)), errSessionMetadataOtherProject)

	owned, err := ti.service.getSessionMetadata(ctx, sessionID)
	require.NoError(t, err)
	require.Equal(t, testProjectID(t, ctx), owned.ProjectID)

	// The owning project can still update its own entry.
	update := testSessionMetadata(t, ctx, sessionID)
	update.Hostname = "owner-host"
	require.NoError(t, ti.service.cacheSessionMetadata(ctx, update))
}

// Two projects racing to attribute a new session id: exactly one claims it.
func TestCacheSessionMetadata_ConcurrentFirstClaimsHaveOneWinner(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)

	for range 20 {
		sessionID := uuid.NewString()
		contexts := []context.Context{ctx, otherProjectContext(t, ctx)}
		errs := make(chan error, len(contexts))
		for _, writerCtx := range contexts {
			metadata := testSessionMetadata(t, writerCtx, sessionID)
			go func() {
				errs <- ti.service.cacheSessionMetadata(writerCtx, metadata)
			}()
		}
		wins := 0
		for range contexts {
			if err := <-errs; err == nil {
				wins++
			} else {
				require.ErrorIs(t, err, errSessionMetadataOtherProject)
			}
		}
		require.Equal(t, 1, wins, "exactly one project claims a new session id")
	}
}

// An OTEL export from another project for an already-attributed session id
// must not re-point the session or adopt its unauthenticated hooks.
func TestClaudeOTELLogs_DoesNotAdoptOtherProjectSession(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	siblingCtx := siblingProjectContext(t, ctx, ti)

	sessionID := uuid.NewString()
	require.NoError(t, ti.service.cacheSessionMetadata(ctx, testSessionMetadata(t, ctx, sessionID)))
	prompt := "owner prompt"
	require.NoError(t, ti.service.cache.ListAppend(ctx, hookPendingCacheKey("", sessionID), gen.ClaudePayload{
		HookEventName: "UserPromptSubmit", SessionID: &sessionID, Prompt: &prompt,
	}, time.Minute))

	require.NoError(t, ti.service.Logs(siblingCtx, claudeSessionLogsPayload(sessionID, "intruder@example.com")))

	owned, err := ti.service.getSessionMetadata(ctx, sessionID)
	require.NoError(t, err, "the owning project's session metadata survives")
	require.Equal(t, "owner@example.com", owned.UserEmail)

	var buffered []gen.ClaudePayload
	require.NoError(t, ti.service.cache.ListRange(ctx, hookPendingCacheKey("", sessionID), 0, -1, &buffered))
	require.Len(t, buffered, 1, "the session's unauthenticated hooks are not flushed into another project")
}

// A hook that authenticated to one project is only ever flushed by that
// project's OTEL export, even when another project attributes the session id.
func TestFlushPendingHooks_AuthenticatedHookStaysInItsProject(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	siblingCtx := siblingProjectContext(t, ctx, ti)

	sessionID := uuid.NewString()
	prompt := "buffered before otel"
	_, err := ti.service.Claude(ctx, &gen.ClaudePayload{HookEventName: "UserPromptSubmit", SessionID: &sessionID, Prompt: &prompt})
	require.NoError(t, err)

	require.NoError(t, ti.service.Logs(siblingCtx, claudeSessionLogsPayload(sessionID, "sibling@example.com")))

	var buffered []gen.ClaudePayload
	require.NoError(t, ti.service.cache.ListRange(ctx, hookPendingCacheKey(testProjectID(t, ctx), sessionID), 0, -1, &buffered))
	require.Len(t, buffered, 1, "another project's OTEL export never flushes this project's hooks")
}

func claudeSessionLogsPayload(sessionID, userEmail string) *gen.LogsPayload {
	return claudeLogsPayload(
		[]*gen.OTELResourceAttribute{resourceStrAttr("service.name", "claude-code")},
		&gen.OTELScope{Name: new("claude-code"), Version: new("1.0.0")},
		&gen.OTELLogRecord{
			Body: &gen.OTELLogBody{StringValue: new("session api request")},
			Attributes: []*gen.OTELAttribute{
				strAttr("session.id", sessionID),
				strAttr("user.email", userEmail),
			},
		},
	)
}
