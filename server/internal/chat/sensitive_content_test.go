package chat_test

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/chat"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

func TestChatSensitiveContentAccess(t *testing.T) {
	t.Parallel()
	for _, session := range []string{"ordinary", "support", "workos", "demo_support", "demo_workos"} {
		t.Run(session, func(t *testing.T) {
			t.Parallel()
			ti := newTestChatService(t)
			ctx := grantOrgAdminWithChatRead(t, initSessionCtx(t, ti))
			authCtx, ok := contextvalues.GetAuthContext(ctx)
			require.True(t, ok)

			allowed := session == "ordinary" || session == "demo_support" || session == "demo_workos"
			if session == "demo_support" || session == "demo_workos" {
				require.NoError(t, testrepo.New(ti.conn).CreateOrganizationMetadataFixture(ctx, testrepo.CreateOrganizationMetadataFixtureParams{
					ID: constants.DemoOrganizationID, Name: "Demo fixture", Slug: "demo-sensitive-content", GramAccountType: "demo",
					FreeTrialStartedAt: pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true},
					FreeTrialEndsAt:    pgtype.Timestamptz{Time: time.Now().UTC().Add(24 * time.Hour), Valid: true},
				}))
				project, err := projectsrepo.New(ti.conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
					OrganizationID: constants.DemoOrganizationID, Name: "Demo fixture", Slug: "demo-sensitive-content",
				})
				require.NoError(t, err)
				demoAuth := *authCtx
				demoAuth.ActiveOrganizationID = constants.DemoOrganizationID
				demoAuth.ProjectID = &project.ID
				authCtx = &demoAuth
				ctx = contextvalues.SetAuthContext(ctx, authCtx)
				ti.projectID, ti.orgID = project.ID, constants.DemoOrganizationID
			}
			own := seedChat(t, ctx, ti, authCtx.UserID, "", "private own title")
			other := seedChat(t, ctx, ti, "other-user", "", "private other title")
			copied := *authCtx
			switch session {
			case "support", "demo_support":
				copied.IsAdmin = true
				copied.SupportOrganizationID = copied.ActiveOrganizationID
				ctx = contextvalues.WithValidatedSupportSession(ctx, &copied)
			case "workos", "demo_workos":
				ctx = contextvalues.WithValidatedGramSession(ctx, &copied, true)
			}
			ctx = authztest.WithExactGrants(t, ctx,
				authz.NewGrant(authz.ScopeChatRead, authz.WildcardResource),
				authz.NewGrant(authz.ScopeChatWrite, authz.WildcardResource))
			t.Run("list_titles", func(t *testing.T) {
				result, err := ti.service.ListChats(ctx, defaultPayload())
				require.NoError(t, err)
				require.Equal(t, 2, result.Total)
				require.Len(t, result.Chats, 2)
				for _, chat := range result.Chats {
					if allowed {
						require.Contains(t, []string{"private own title", "private other title"}, chat.Title)
					} else {
						require.Equal(t, "Session hidden during impersonation", chat.Title)
					}
				}
			})

			t.Run("title_search", func(t *testing.T) {
				payload := defaultPayload()
				query := "private other"
				payload.Search = &query
				result, err := ti.service.ListChats(ctx, payload)
				if allowed {
					require.NoError(t, err)
					require.Equal(t, 1, result.Total)
				} else {
					require.Error(t, err, "search must not act as an oracle for hidden titles")
					require.Nil(t, result)
					var shareable *oops.ShareableError
					require.ErrorAs(t, err, &shareable)
					require.Equal(t, oops.CodeForbidden, shareable.Code)
				}
			})
			t.Run("linked_titles", func(t *testing.T) {
				seedSessionLink(t, ctx, ti, own, &other, "test-harness")
				result, err := ti.service.ListSessionLinks(ctx, &gen.ListSessionLinksPayload{ChatIds: []string{own.String()}})
				require.NoError(t, err)
				require.Len(t, result.Links, 1)
				parent, child := "private own title", "private other title"
				if !allowed {
					parent, child = "Session hidden during impersonation", "Session hidden during impersonation"
				}
				require.NotNil(t, result.Links[0].ParentTitle)
				require.NotNil(t, result.Links[0].ChildTitle)
				require.Equal(t, parent, *result.Links[0].ParentTitle)
				require.Equal(t, child, *result.Links[0].ChildTitle)
			})
			for name, chatID := range map[string]string{"own": own.String(), "other": other.String()} {
				t.Run(name, func(t *testing.T) {
					before, err := audittest.AuditLogCountByAction(t.Context(), ti.conn, audit.ActionChatSessionAccess)
					require.NoError(t, err)
					result, err := ti.service.LoadChat(ctx, loadPayload(chatID))
					if allowed {
						require.NoError(t, err)
						require.Equal(t, chatID, result.ID)
					} else {
						require.Error(t, err)
						require.Nil(t, result)
						var shareable *oops.ShareableError
						require.ErrorAs(t, err, &shareable)
						require.Equal(t, oops.CodeForbidden, shareable.Code)
						after, err := audittest.AuditLogCountByAction(t.Context(), ti.conn, audit.ActionChatSessionAccess)
						require.NoError(t, err)
						require.Equal(t, before, after)
					}

					pinErr := ti.service.SetPinned(ctx, &gen.SetPinnedPayload{ID: chatID, Pinned: true})
					if allowed {
						require.NoError(t, pinErr)
					} else {
						var shareable *oops.ShareableError
						require.ErrorAs(t, pinErr, &shareable)
						require.Equal(t, oops.CodeForbidden, shareable.Code)
					}
				})
			}
		})
	}
}
