package chat_test

import (
	"context"
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	agentrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// asMember returns ctx authenticated as userID holding exactly grants.
func asMember(t *testing.T, ctx context.Context, userID string, grants ...authz.Grant) context.Context {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	copied := *authCtx
	copied.UserID = userID
	return authztest.WithExactGrants(t, contextvalues.SetAuthContext(ctx, &copied), grants...)
}

// readable returns which of chats the caller can load directly.
func readable(t *testing.T, ti *chatTestInstance, ctx context.Context, chats []uuid.UUID) []string {
	t.Helper()
	var ids []string
	for _, id := range chats {
		_, err := ti.service.LoadChat(ctx, loadPayload(id.String()))
		if err == nil {
			ids = append(ids, id.String())
			continue
		}
		requireOopsCode(t, err, oops.CodeForbidden)
	}
	slices.Sort(ids)
	return ids
}

// listed walks the listing one row per page and returns every id and the
// total each page reported.
func listed(t *testing.T, ti *chatTestInstance, ctx context.Context) ([]string, []int) {
	t.Helper()
	var ids []string
	var totals []int
	for offset := 0; ; offset++ {
		payload := defaultPayload()
		payload.Limit, payload.Offset = 1, offset
		page, err := ti.service.ListChats(ctx, payload)
		require.NoError(t, err)
		totals = append(totals, page.Total)
		if len(page.Chats) == 0 {
			break
		}
		for _, c := range page.Chats {
			ids = append(ids, c.ID)
		}
	}
	slices.Sort(ids)
	return ids, totals
}

// The evidence gate for option D: reading an agent's history after its
// ownership is transferred, through the direct read and the listing, with the
// organization switched onto the schema.
func TestAgentHistorySchema(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ti.schemaActive.Store(true)
	base := initSessionCtx(t, ti)
	authCtx, ok := contextvalues.GetAuthContext(base)
	require.True(t, ok)

	u1, u2, manager, reviewer := authCtx.UserID, "user-new-owner", "user-agent-manager", "user-session-reviewer"
	agent := seedAgent(t, base, ti, u1, "Transferred Agent")
	seedAgent(t, base, ti, u2, "Second Owner's Other Agent")

	agentOnly := seedAgentChat(t, base, ti, agent, "agent-only history")
	humanAndAgent := seedChat(t, base, ti, u1, "", "U1's own session")
	linkAgent(t, base, ti, humanAndAgent, agent)
	unattributed := seedChat(t, base, ti, "", "", "no attribution")
	// An agent id with no agent row in this organization, as a reference to
	// another tenant's agent would be: there is no ownership fact to find.
	dangling := seedChat(t, base, ti, "", "", "dangling agent reference")
	_, err := ti.conn.Exec(base, "UPDATE chats SET agent_id = $1 WHERE id = $2", uuid.New(), dangling)
	require.NoError(t, err)
	all := []uuid.UUID{agentOnly, humanAndAgent, unattributed, dangling}

	management := []authz.Grant{
		authz.NewGrant(authz.ScopeAgentRead, authz.WildcardResource),
		authz.NewGrant(authz.ScopeAgentWrite, authz.WildcardResource),
		authz.NewGrant(authz.ScopeAgentAuthorize, authz.WildcardResource),
		authz.NewGrant(authz.ScopeAgentTransfer, authz.WildcardResource),
	}
	callers := map[string]context.Context{
		"former owner": asMember(t, base, u1),
		"new owner":    asMember(t, base, u2),
		"manager":      asMember(t, base, manager, management...),
		"reviewer":     asMember(t, base, reviewer, authz.NewGrant(authz.ScopeChatRead, authz.WildcardResource)),
	}
	s := func(ids ...uuid.UUID) []string {
		out := make([]string, 0, len(ids))
		for _, id := range ids {
			out = append(out, id.String())
		}
		slices.Sort(out)
		return out
	}

	check := func(t *testing.T, want map[string][]string) {
		t.Helper()
		for name, ctx := range callers {
			direct := readable(t, ti, ctx, all)
			require.Equal(t, want[name], direct, "%s: direct reads", name)

			ids, totals := listed(t, ti, ctx)
			require.Equal(t, direct, ids, "%s: listing returns exactly what direct reads allow", name)
			for _, total := range totals {
				require.Equal(t, len(direct), total, "%s: every page, including the empty one past the end, reports the same total", name)
			}
		}
	}

	// Before the transfer U1 owns the agent.
	check(t, map[string][]string{
		"former owner": s(agentOnly, humanAndAgent),
		"new owner":    nil,
		"manager":      nil,
		"reviewer":     s(all...),
	})

	_, err = agentrepo.New(ti.conn).TransferAgent(base, agentrepo.TransferAgentParams{OwnerUserID: u2, OrganizationID: ti.orgID, ID: agent})
	require.NoError(t, err)

	// After it, agent-only history moves to U2; U1 keeps their own session,
	// which names the agent but is attributed to them.
	check(t, map[string][]string{
		"former owner": s(humanAndAgent),
		"new owner":    s(agentOnly),
		"manager":      nil,
		"reviewer":     s(all...),
	})
}

func linkAgent(t *testing.T, ctx context.Context, ti *chatTestInstance, chatID, agentID uuid.UUID) {
	t.Helper()
	_, err := ti.conn.Exec(ctx, "UPDATE chats SET agent_id = $1 WHERE id = $2", agentID, chatID)
	require.NoError(t, err)
}
