package platformmcp

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	agentsrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	organizationsrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

// ownerScopeAgents is the agent population every owner-visibility test needs:
// one the member owns, one they owned until it was latched for reassignment,
// and one somebody else owns.
type ownerScopeAgents struct {
	own        uuid.UUID
	latched    uuid.UUID
	foreign    uuid.UUID
	otherOwner string
	// email is the member's account email, which their hook rows are keyed by.
	email string
}

// seedOwnerScopeAgents makes the principal a real member with an email and
// seeds the three agents. Agent ownership needs the membership row, so it is
// created for both owners.
func seedOwnerScopeAgents(t *testing.T, ctx context.Context, conn *pgxpool.Pool, principal Principal) ownerScopeAgents {
	t.Helper()
	agents := ownerScopeAgents{own: uuid.Nil, latched: uuid.Nil, foreign: uuid.Nil, otherOwner: "user_" + uuid.NewString(), email: "member-" + uuid.NewString()[:8] + "@example.com"}
	_, err := usersrepo.New(conn).UpsertUser(ctx, usersrepo.UpsertUserParams{ID: principal.UserID, Email: agents.email, DisplayName: "Member", PhotoUrl: pgtype.Text{}, Admin: false})
	require.NoError(t, err)
	for _, owner := range []string{principal.UserID, agents.otherOwner} {
		_, err := organizationsrepo.New(conn).UpsertOrganizationUserRelationship(ctx, organizationsrepo.UpsertOrganizationUserRelationshipParams{
			OrganizationID: principal.OrganizationID,
			UserID:         conv.ToPGText(owner),
		})
		require.NoError(t, err)
	}
	agents.own = seedOwnerScopeAgent(t, ctx, conn, principal.OrganizationID, principal.UserID, "Own agent")
	agents.latched = seedOwnerScopeAgent(t, ctx, conn, principal.OrganizationID, principal.UserID, "Latched agent")
	require.NoError(t, testrepo.New(conn).SetAgentOwnerLatchFixture(ctx, agents.latched))
	agents.foreign = seedOwnerScopeAgent(t, ctx, conn, principal.OrganizationID, agents.otherOwner, "Someone else's agent")
	return agents
}

func seedOwnerScopeAgent(t *testing.T, ctx context.Context, conn *pgxpool.Pool, organizationID, ownerUserID, name string) uuid.UUID {
	t.Helper()
	agent, err := agentsrepo.New(conn).CreateAgent(ctx, agentsrepo.CreateAgentParams{
		OrganizationID: organizationID,
		OwnerUserID:    ownerUserID,
		ProjectID:      uuid.NullUUID{},
		Name:           name,
	})
	require.NoError(t, err)
	return agent.ID
}

// seedScopedChat inserts a chat with one message at `at`, attributed to a human
// owner, an agent, or both. An empty userID with an agent is the agent-only
// session an owner may read; both set is a human's session an agent took part in.
func seedScopedChat(t *testing.T, ctx context.Context, conn *pgxpool.Pool, project ResolvedProject, organizationID, userID string, agentID uuid.UUID, content string, at time.Time) uuid.UUID {
	t.Helper()
	chatID := uuid.New()
	_, err := chatrepo.New(conn).UpsertChat(ctx, chatrepo.UpsertChatParams{
		ID:             chatID,
		ProjectID:      project.ID,
		OrganizationID: organizationID,
		UserID:         conv.ToPGTextEmpty(userID),
		ExternalUserID: pgtype.Text{},
		Title:          conv.ToPGText("scoped " + chatID.String()[:8]),
	})
	require.NoError(t, err)
	if agentID != uuid.Nil {
		require.NoError(t, testrepo.New(conn).SetChatAgentFixture(ctx, testrepo.SetChatAgentFixtureParams{AgentID: uuid.NullUUID{UUID: agentID, Valid: true}, ID: chatID}))
	}
	seedRecallMessage(t, ctx, conn, chatID, project.ID, recallMessageSeed{role: "user", content: content, at: at})
	return chatID
}

// demoteScopedChat lands a human turn on an agent-only chat, the path UpsertChat
// demotes to the empty-string no-author shape neither the owner nor the human may open.
func demoteScopedChat(t *testing.T, ctx context.Context, conn *pgxpool.Pool, project ResolvedProject, organizationID string, chatID uuid.UUID, userID string) {
	t.Helper()
	_, err := chatrepo.New(conn).UpsertChat(ctx, chatrepo.UpsertChatParams{
		ID:             chatID,
		ProjectID:      project.ID,
		OrganizationID: organizationID,
		UserID:         conv.ToPGText(userID),
		ExternalUserID: pgtype.Text{},
		AgentID:        uuid.NullUUID{},
		Title:          pgtype.Text{},
	})
	require.NoError(t, err)
}

// platformMemberContext is the context the external Platform MCP endpoint
// prepares for a member: their auth context, the acting surface, exactly the
// grants given, and the principal.
func platformMemberContext(ctx context.Context, principal Principal, grants ...authz.Grant) context.Context {
	ctx = contextvalues.WithAuthenticatedActor(ctx, &contextvalues.AuthContext{ActiveOrganizationID: principal.OrganizationID, UserID: principal.UserID}, urn.NewPrincipal(urn.PrincipalTypeUser, principal.UserID))
	ctx = contextvalues.SetActingSurface(ctx, contextvalues.ActingSurfacePlatformMCP)
	if grants == nil {
		grants = []authz.Grant{}
	}
	return ContextWithPrincipal(authz.GrantsToContext(ctx, grants), principal)
}
