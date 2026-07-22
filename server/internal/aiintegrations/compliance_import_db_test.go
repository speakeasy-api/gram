package aiintegrations

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

func TestUpsertActivityChatPreservesResolvedUserOnUnresolvedResync(t *testing.T) {
	t.Parallel()

	ctx, conn, store, orgID := newStoreTestDB(t)

	project, err := projectsrepo.New(conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
		Name:           "Compliance Import Test Project",
		Slug:           "project-" + uuid.NewString()[:8],
		OrganizationID: orgID,
	})
	require.NoError(t, err)

	userRow, err := usersrepo.New(conn).UpsertUser(ctx, usersrepo.UpsertUserParams{
		ID:          "user_" + uuid.NewString(),
		Email:       "ada@example.com",
		DisplayName: "Ada",
		PhotoUrl:    conv.ToPGTextEmpty(""),
		Admin:       false,
	})
	require.NoError(t, err)
	require.NoError(t, testrepo.New(conn).CreateOrganizationUserRelationshipFixture(ctx, testrepo.CreateOrganizationUserRelationshipFixtureParams{
		OrganizationID: orgID,
		UserID:         conv.ToPGText(userRow.ID),
	}))

	extOrgID := "ext-org"
	created := upsertConfigWithTx(t, ctx, conn, store, orgID, ProviderAnthropicCompliance, "anthropic-key", true, true, &extOrgID, nil)
	cfg := created.Config
	cfg.ProjectID = project.ID

	svc := NewComplianceImportService(testenv.NewLogger(t), conn, nil, nil, func(context.Context, string, int) {})
	resolver := newConnectedUserResolver(conn, orgID)

	// First activity: the actor email resolves to a connected user.
	resolved := complianceUserActivity("act_1", "chat_ext_1")
	resolved.Actor.EmailAddress = "ada@example.com"
	resolved.Actor.UserID = "anthropic_user_1"
	chatID, _, err := svc.upsertActivityChat(ctx, cfg, resolved, resolver)
	require.NoError(t, err)

	chatRow, err := chatrepo.New(conn).GetChat(ctx, chatID)
	require.NoError(t, err)
	require.Equal(t, userRow.ID, chatRow.UserID.String)

	// A later activity for the same chat whose actor carries no identity (e.g.
	// a claude_chat_updated event with an empty actor email) must not clobber
	// the previously resolved user.
	unresolved := complianceUserActivity("act_2", "chat_ext_1")
	unresolved.Type = anthropicComplianceActivityUpdated
	unresolved.Actor.EmailAddress = ""
	unresolved.Actor.UserID = ""
	sameChatID, _, err := svc.upsertActivityChat(ctx, cfg, unresolved, resolver)
	require.NoError(t, err)
	require.Equal(t, chatID, sameChatID)

	chatRow, err = chatrepo.New(conn).GetChat(ctx, chatID)
	require.NoError(t, err)
	require.Equal(t, userRow.ID, chatRow.UserID.String, "resolved user must survive an unresolved re-sync")
	require.Equal(t, "anthropic_user_1", chatRow.ExternalUserID.String, "external user id must survive an unresolved re-sync")
}

func TestConnectedUserResolverMatchesMixedCaseStoredEmail(t *testing.T) {
	t.Parallel()

	ctx, conn, _, orgID := newStoreTestDB(t)

	// WorkOS-synced users can be stored with their original casing; the
	// compliance feed reports lowercase emails.
	userRow, err := usersrepo.New(conn).UpsertUser(ctx, usersrepo.UpsertUserParams{
		ID:          "user_" + uuid.NewString(),
		Email:       "Ada.Lovelace@Example.com",
		DisplayName: "Ada",
		PhotoUrl:    conv.ToPGTextEmpty(""),
		Admin:       false,
	})
	require.NoError(t, err)
	require.NoError(t, testrepo.New(conn).CreateOrganizationUserRelationshipFixture(ctx, testrepo.CreateOrganizationUserRelationshipFixtureParams{
		OrganizationID: orgID,
		UserID:         conv.ToPGText(userRow.ID),
	}))

	resolver := newConnectedUserResolver(conn, orgID)
	resolvedID, err := resolver.resolve(ctx, "ada.lovelace@example.com")
	require.NoError(t, err)
	require.Equal(t, userRow.ID, resolvedID)
}
