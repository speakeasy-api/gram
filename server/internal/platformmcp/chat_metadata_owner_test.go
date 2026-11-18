package platformmcp

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/authz"
	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// The member's scope travels to the chat query as the same user and owned-agent
// parameters the dashboard listing uses, on the page and on the fallback count.
func TestListChatsOwnScopeReachesTheQuery(t *testing.T) {
	t.Parallel()

	principal := registrationServicePrincipal()
	agentID := uuid.New()
	reader := &recordingChatReader{rows: nil, params: nil, err: nil, countParams: nil, count: 0}
	service, _ := newChatMetadataService(t, reader, allowBudget())
	service.visibility = fixedChatListVisibility{scope: chatListScope{userID: principal.UserID, ownedAgentIDs: []uuid.UUID{agentID}}, err: nil}

	_, err := service.List(t.Context(), principal, ListChatsInput{AgentID: agentID.String()})
	require.NoError(t, err)
	require.Len(t, reader.params, 1)
	require.Equal(t, principal.UserID, reader.params[0].UserID)
	require.Equal(t, []uuid.UUID{agentID}, reader.params[0].OwnedAgentIds)
	require.Equal(t, agentID, reader.params[0].AgentID.UUID, "the agent filter still narrows inside the scope")

	// A page past the end counts under the same scope.
	rows := chatListRows(3, 3)
	reader.rows = rows
	first, err := service.List(t.Context(), principal, ListChatsInput{Limit: 2})
	require.NoError(t, err)
	require.NotEmpty(t, first.NextCursor)
	reader.rows = nil
	_, err = service.List(t.Context(), principal, ListChatsInput{Limit: 2, Cursor: first.NextCursor})
	require.NoError(t, err)
	require.Len(t, reader.countParams, 1)
	require.Equal(t, principal.UserID, reader.countParams[0].UserID)
	require.Equal(t, []uuid.UUID{agentID}, reader.countParams[0].OwnedAgentIds)
}

// A person reference can only narrow a member's listing: one naming the member
// selects their own sessions without their agents', and one naming anyone else
// is refused as unavailable rather than widening the page.
func TestListChatsOwnScopeUserReferenceOnlyNarrows(t *testing.T) {
	t.Parallel()

	principal := registrationServicePrincipal()
	agentID := uuid.New()
	reader := &recordingChatReader{rows: []chatrepo.ListChatsRow{chatListRow(uuid.New(), chatListTestNow, principal.UserID, "", "", 1)}, params: nil, err: nil, countParams: nil, count: 0}
	service, project := newChatMetadataService(t, reader, allowBudget())
	service.visibility = fixedChatListVisibility{scope: chatListScope{userID: principal.UserID, ownedAgentIDs: []uuid.UUID{agentID}}, err: nil}

	page, err := service.List(t.Context(), principal, ListChatsInput{})
	require.NoError(t, err)
	require.Len(t, page.Chats, 1)
	self := page.Chats[0].UserReference
	require.NotEmpty(t, self)

	_, err = service.List(t.Context(), principal, ListChatsInput{UserReference: self})
	require.NoError(t, err)
	require.Equal(t, principal.UserID, reader.params[1].UserID)
	require.Empty(t, reader.params[1].OwnedAgentIds, "a self reference narrows to the member's own sessions")

	for name, identity := range map[string]string{
		"another member":   FormatSubjectIdentity(SubjectIdentityUser, "user_someone_else"),
		"an external user": FormatSubjectIdentity(SubjectIdentityExternal, "external-1"),
	} {
		other, err := service.references.EncodeScoped(principal, subjectKindUser, chatListUserScope(project.ID), identity, chatListTestNow)
		require.NoError(t, err)
		_, err = service.List(t.Context(), principal, ListChatsInput{UserReference: other})
		require.ErrorIs(t, err, ErrSubjectReferenceNotFound, name)
	}
	require.Len(t, reader.params, 2, "a refused reference never reaches the database")

	// A cursor minted in the member's scope does not resume project-wide.
	reader.rows = chatListRows(3, 3)
	first, err := service.List(t.Context(), principal, ListChatsInput{Limit: 2})
	require.NoError(t, err)
	require.NotEmpty(t, first.NextCursor)
	service.visibility = fixedChatListVisibility{scope: chatListScope{userID: "", ownedAgentIDs: nil}, err: nil}
	_, err = service.List(t.Context(), principal, ListChatsInput{Limit: 2, Cursor: first.NextCursor})
	require.ErrorIs(t, err, ErrSubjectReferenceNotFound)
}

func TestListChatsVisibilityErrorsRefuseTheListing(t *testing.T) {
	t.Parallel()

	reader := &recordingChatReader{rows: nil, params: nil, err: nil, countParams: nil, count: 0}
	service, _ := newChatMetadataService(t, reader, allowBudget())
	service.visibility = fixedChatListVisibility{scope: chatListScope{}, err: ErrForbidden}
	_, err := service.List(t.Context(), registrationServicePrincipal(), ListChatsInput{})
	require.ErrorIs(t, err, ErrForbidden)
	require.Empty(t, reader.params)
}

// The dashboard visibility rule, end to end against Postgres and the
// authorization engine: a member lists their own chats and their owned agents'
// agent-only chats; chat:read on the project lists everyone's.
func TestListChatsOwnerVisibilityIntegration(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_chat_list_owner_scope")
	require.NoError(t, err)
	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	agents := seedOwnerScopeAgents(t, ctx, conn, principal)
	// Chats are created on the database clock and the window end is truncated
	// to the second, so the listing's clock runs a minute ahead of seeding.
	seed := func(userID string, agentID uuid.UUID) uuid.UUID {
		return seedScopedChat(t, ctx, conn, project, principal.OrganizationID, userID, agentID, "listed", time.Now().UTC().Add(-time.Minute))
	}
	own := seed(principal.UserID, uuid.Nil)
	ownAgent := seed("", agents.own)
	other := seed(agents.otherOwner, uuid.Nil)
	latchedAgent := seed("", agents.latched)
	foreignAgent := seed("", agents.foreign)
	ownAgentForOther := seed(agents.otherOwner, agents.own)

	engine := authz.NewEngine(testenv.NewLogger(t), conn, func(context.Context, string) (bool, error) { return false, nil }, nil)
	codec, err := newSubjectReferenceCodec("chat-list-owner-test-key")
	require.NoError(t, err)
	chats := chatrepo.New(conn)
	// Findings are faked so the test needs no ClickHouse; the visibility and the
	// chat query are the production ones.
	service := &ChatMetadataService{
		projects:     postgresRiskProjectResolver{queries: platformrepo.New(conn)},
		chats:        chats,
		visibility:   authzChatListVisibility{engine: engine, agents: chats},
		references:   codec,
		budget:       allowBudget(),
		now:          func() time.Time { return time.Now().UTC().Add(time.Minute) },
		riskFindings: fakeChatRiskFindings{count: 0},
	}
	listed := func(ctx context.Context, input ListChatsInput) []string {
		t.Helper()
		input.ProjectID = project.ID.String()
		output, err := service.List(ctx, principal, input)
		require.NoError(t, err)
		ids := make([]string, 0, len(output.Chats))
		for _, chat := range output.Chats {
			ids = append(ids, chat.ChatID)
		}
		require.Equal(t, len(ids), output.TotalMatches)
		return ids
	}

	member := platformMemberContext(ctx, principal)
	require.ElementsMatch(t, uuidStrings([]uuid.UUID{own, ownAgent}), listed(member, ListChatsInput{}))
	require.Equal(t, uuidStrings([]uuid.UUID{ownAgent}), listed(member, ListChatsInput{AgentID: agents.own.String()}))
	require.Empty(t, listed(member, ListChatsInput{AgentID: agents.foreign.String()}), "an agent filter cannot reach another owner's agent")
	require.Empty(t, listed(member, ListChatsInput{AgentID: agents.latched.String()}), "a latched agent is nobody's")

	orgAdmin := platformMemberContext(ctx, principal, authz.NewGrant(authz.ScopeOrgAdmin, principal.OrganizationID), authz.NewGrant(authz.ScopeProjectRead, project.ID.String()))
	require.ElementsMatch(t, uuidStrings([]uuid.UUID{own, ownAgent}), listed(orgAdmin, ListChatsInput{}), "organization administration does not imply chat:read")

	everything := uuidStrings([]uuid.UUID{own, ownAgent, other, latchedAgent, foreignAgent, ownAgentForOther})
	reviewer := platformMemberContext(ctx, principal, authz.NewGrant(authz.ScopeChatRead, project.ID.String()))
	require.ElementsMatch(t, everything, listed(reviewer, ListChatsInput{}))
	otherProject := platformMemberContext(ctx, principal, authz.NewGrant(authz.ScopeChatRead, uuid.NewString()))
	require.ElementsMatch(t, uuidStrings([]uuid.UUID{own, ownAgent}), listed(otherProject, ListChatsInput{}), "chat:read on another project does not widen this one")

	// The managed-assistant runtime reads project-wide, as the dashboard
	// listing treats it.
	assistant := contextvalues.SetAssistantPrincipal(member, contextvalues.AssistantPrincipal{AssistantID: uuid.New(), ThreadID: uuid.New()})
	require.ElementsMatch(t, everything, listed(assistant, ListChatsInput{}))

	// A principal that is not the authenticated user gets no own scope.
	impostor := principal
	impostor.UserID = agents.otherOwner
	_, err = service.List(member, impostor, ListChatsInput{ProjectID: project.ID.String()})
	require.ErrorIs(t, err, ErrForbidden)
}
