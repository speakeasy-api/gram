package platformmcp

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
)

// ownerScopeChats is the chat population an owner-visibility test reads
// against: two the member may reach and four they may not.
type ownerScopeChats struct {
	agents ownerScopeAgents
	// own is the member's session; ownAgent is an agent-only session of an
	// agent they own.
	own, ownAgent uuid.UUID
	// other belongs to someone else; latchedAgent and foreignAgent are
	// agent-only sessions nobody, or somebody else, answers for; ownAgentForOther
	// is somebody else's session the member's agent took part in.
	other, latchedAgent, foreignAgent, ownAgentForOther uuid.UUID
}

func (c ownerScopeChats) visible() []uuid.UUID {
	return []uuid.UUID{c.own, c.ownAgent}
}

func (c ownerScopeChats) hidden() []uuid.UUID {
	return []uuid.UUID{c.other, c.latchedAgent, c.foreignAgent, c.ownAgentForOther}
}

func seedOwnerScopeChats(t *testing.T, ctx context.Context, f chatSearchFixture, content string, at time.Time) ownerScopeChats {
	t.Helper()
	agents := seedOwnerScopeAgents(t, ctx, f.db, f.principal)
	seed := func(userID string, agentID uuid.UUID) uuid.UUID {
		return seedScopedChat(t, ctx, f.db, f.project, f.principal.OrganizationID, userID, agentID, content, at)
	}
	return ownerScopeChats{
		agents:           agents,
		own:              seed(f.principal.UserID, uuid.Nil),
		ownAgent:         seed("", agents.own),
		other:            seed(agents.otherOwner, uuid.Nil),
		latchedAgent:     seed("", agents.latched),
		foreignAgent:     seed("", agents.foreign),
		ownAgentForOther: seed(agents.otherOwner, agents.own),
	}
}

func chatIDsOf(hits []ChatMessageHit) []string {
	ids := make([]string, 0, len(hits))
	for _, hit := range hits {
		ids = append(ids, hit.ChatID)
	}
	return ids
}

// A member without chat:read searches and reads their own chats and the
// agent-only chats of agents they currently own, and nothing else.
func TestChatSearchMemberSeesOwnAndOwnedAgentChatsOnly(t *testing.T) {
	t.Parallel()
	f := newChatSearchFixture(t)
	admin := f.context(t.Context())
	member := platformMemberContext(t.Context(), f.principal)
	chats := seedOwnerScopeChats(t, admin, f, "scope_needle", f.base)

	output, err := f.service.Search(member, f.principal, f.search("scope_needle"))
	require.NoError(t, err)
	require.ElementsMatch(t, uuidStrings(chats.visible()), chatIDsOf(output.Hits))

	for _, chatID := range chats.visible() {
		read, err := f.service.Read(member, f.principal, ReadChatInput{ProjectID: f.project.ID.String(), ChatID: chatID.String()})
		require.NoError(t, err, chatID)
		require.Len(t, read.Messages, 1)
	}
	for _, chatID := range chats.hidden() {
		_, err := f.service.Read(member, f.principal, ReadChatInput{ProjectID: f.project.ID.String(), ChatID: chatID.String()})
		var permission *ExternalAuthorizationError
		require.ErrorAs(t, err, &permission, chatID)
		require.Equal(t, "chat:read", permission.RequiredScope)
	}
	// Own-scope reads are audited like any other transcript access.
	count, err := audittest.AuditLogCountByAction(admin, f.db, audit.ActionChatSessionAccess)
	require.NoError(t, err)
	require.EqualValues(t, 4, count, "two chats found by the search and two reads")

	// The unrestricted grant sees everything, and the organization admin role
	// alone does not.
	all, err := f.service.Search(admin, f.principal, f.search("scope_needle"))
	require.NoError(t, err)
	require.ElementsMatch(t, uuidStrings(append(chats.visible(), chats.hidden()...)), chatIDsOf(all.Hits))
	orgAdmin := platformMemberContext(t.Context(), f.principal, authz.NewGrant(authz.ScopeOrgAdmin, f.principal.OrganizationID))
	narrowed, err := f.service.Search(orgAdmin, f.principal, f.search("scope_needle"))
	require.NoError(t, err)
	require.ElementsMatch(t, uuidStrings(chats.visible()), chatIDsOf(narrowed.Hits))
}

// Filters never widen a member's scope: naming another owner or another
// owner's chat selects nothing, and a cursor minted in the member's own scope
// does not resume once the scope changes.
func TestChatSearchMemberFiltersCannotWiden(t *testing.T) {
	t.Parallel()
	f := newChatSearchFixture(t)
	admin := f.context(t.Context())
	member := platformMemberContext(t.Context(), f.principal)
	chats := seedOwnerScopeChats(t, admin, f, "widen_needle", f.base)

	byOwner := f.search("widen_needle")
	byOwner.OwnerUserID = chats.agents.otherOwner
	output, err := f.service.Search(member, f.principal, byOwner)
	require.NoError(t, err)
	require.Empty(t, output.Hits)

	byChat := f.search("widen_needle")
	byChat.ChatIDs = uuidStrings([]uuid.UUID{chats.other, chats.foreignAgent, chats.ownAgent})
	output, err = f.service.Search(member, f.principal, byChat)
	require.NoError(t, err)
	require.Equal(t, []string{chats.ownAgent.String()}, chatIDsOf(output.Hits))

	paged := f.search("widen_needle")
	paged.Limit = 1
	first, err := f.service.Search(member, f.principal, paged)
	require.NoError(t, err)
	require.NotEmpty(t, first.NextCursor)
	paged.Cursor = first.NextCursor
	_, err = f.service.Search(admin, f.principal, paged)
	require.ErrorIs(t, err, ErrSubjectReferenceNotFound, "a cursor is bound to the scope that minted it")
	_, err = f.service.Search(member, f.principal, paged)
	require.NoError(t, err)
}
