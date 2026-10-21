package chat_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	authrepo "github.com/speakeasy-api/gram/server/internal/auth/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestHandleTurnStreamSensitiveContentAccess(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		support bool
		workos  bool
		demo    bool
	}{
		{name: "normal"},
		{name: "support", support: true},
		{name: "workos", workos: true},
		{name: "support_demo", support: true, demo: true},
		{name: "workos_demo", workos: true, demo: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ti := newTestChatService(t)
			ctx := testenv.InitAuthContext(t, t.Context(), ti.conn, ti.sessions)
			authCtx, ok := contextvalues.GetAuthContext(ctx)
			require.True(t, ok)
			session, err := ti.sessions.GetSession(ctx, *authCtx.SessionID)
			require.NoError(t, err)
			ti.projectID = *authCtx.ProjectID
			ti.projectSlug = *authCtx.ProjectSlug
			if tc.demo {
				_, err := orgrepo.New(ti.conn).UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{
					ID: constants.DemoOrganizationID, Name: "Demo", Slug: "stream-demo", WorkosID: pgtype.Text{}, Whitelisted: pgtype.Bool{},
				})
				require.NoError(t, err)
				project, err := projectsrepo.New(ti.conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
					Name: "Demo", Slug: "stream-demo", OrganizationID: constants.DemoOrganizationID,
				})
				require.NoError(t, err)
				ti.projectID, ti.projectSlug = project.ID, project.Slug
				session.ActiveOrganizationID = constants.DemoOrganizationID
			}
			if tc.support {
				require.NoError(t, authrepo.New(ti.conn).SetUserAdminFixture(ctx, authrepo.SetUserAdminFixtureParams{
					Admin: true, UserID: session.UserID,
				}))
				require.NoError(t, ti.sessions.InvalidateUserInfoCache(ctx, session.UserID))
				session.SupportOrganizationID = session.ActiveOrganizationID
				session.SupportExpiresAt = time.Now().Add(time.Hour)
			}
			if tc.workos {
				session.ImpersonatorEmail = "support@example.com"
			}
			// Exercise the same cached session authentication as the raw HTTP route,
			// including the immutable impersonation markers set by Authenticate.
			session.SessionID = uuid.NewString()
			require.NoError(t, ti.sessions.StoreSession(ctx, session))
			authenticated, err := ti.sessions.Authenticate(t.Context(), session.SessionID)
			require.NoError(t, err)
			require.Equal(t, tc.support, contextvalues.IsSupportSession(authenticated))
			require.Equal(t, tc.workos, contextvalues.IsLegacyImpersonatedSession(authenticated))

			// Preserve generous project/chat grants so a denial cannot be attributed
			// to RBAC instead of the sensitive-content safeguard.
			ctx = authztest.WithExactGrants(t, ctx,
				authz.NewGrant(authz.ScopeProjectRead, authz.WildcardResource),
				authz.NewGrant(authz.ScopeChatRead, authz.WildcardResource),
			)
			chatID := seedChat(t, ctx, ti, session.UserID, "", "Stream authorization")
			stream := newTurnStream(t)
			ti.service.WithTurnStream(stream)
			_, err = stream.Publish(ctx, chatID, textFrame("private transcript"))
			require.NoError(t, err)
			_, err = stream.Publish(ctx, chatID, doneFrame())
			require.NoError(t, err)
			requestCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			req := httptest.NewRequestWithContext(requestCtx, http.MethodGet, "/chat/turn-stream?after=0-0&chat_id="+chatID.String(), nil)
			req.Header.Set(constants.SessionHeader, session.SessionID)
			req.Header.Set(constants.ProjectHeader, ti.projectSlug)
			recorder := httptest.NewRecorder()
			err = ti.service.HandleTurnStream(recorder, req)
			if (tc.support || tc.workos) && !tc.demo {
				var shareable *oops.ShareableError
				require.ErrorAs(t, err, &shareable)
				require.Equal(t, oops.CodeForbidden, shareable.Code)
				require.Empty(t, recorder.Body.String(), "blocked streams must not expose frames")
				require.Empty(t, recorder.Header().Get("Content-Type"))
			} else {
				require.NoError(t, err)
				require.Equal(t, http.StatusOK, recorder.Code)
				require.Equal(t, "text/event-stream", recorder.Header().Get("Content-Type"))
				require.Contains(t, recorder.Body.String(), "private transcript")
			}
		})
	}
}
