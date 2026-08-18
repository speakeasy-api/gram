package chat_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/chat"
	agentrepo "github.com/speakeasy-api/gram/server/internal/agent/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
)

// seedSessionLink inserts one lineage edge through the agent package's
// sqlc-generated insert — the same write the move-report handler performs; the
// handler's own behavior is covered by the agent service tests.
func seedSessionLink(t *testing.T, ctx context.Context, ti *chatTestInstance, parentChatID uuid.UUID, childChatID *uuid.UUID, targetHarness string) {
	t.Helper()
	var childID uuid.NullUUID
	childSessionID := pgtype.Text{}
	if childChatID != nil {
		childID = uuid.NullUUID{UUID: *childChatID, Valid: true}
		childSessionID = pgtype.Text{String: childChatID.String(), Valid: true}
	}
	require.NoError(t, agentrepo.New(ti.conn).InsertChatSessionLink(ctx, agentrepo.InsertChatSessionLinkParams{
		ProjectID:       ti.projectID,
		OrganizationID:  ti.orgID,
		ParentChatID:    parentChatID,
		ChildChatID:     childID,
		ParentSessionID: parentChatID.String(),
		ChildSessionID:  childSessionID,
		TargetHarness:   targetHarness,
		SourceSurface:   pgtype.Text{},
		ActorEmail:      pgtype.Text{String: "dev@acme.corp", Valid: true},
		DeviceSerial:    pgtype.Text{},
		DeviceHostname:  pgtype.Text{String: "dev-macbook-pro", Valid: true},
	}))
}

// An edge is visible from either end: the parent's detail panel shows "moved
// to", the child's shows "derived from" — one endpoint serves both.
func TestListSessionLinks_ParentAndChildDirections(t *testing.T) {
	t.Parallel()

	ti := newTestChatService(t)
	ctx := initSessionCtx(t, ti)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	parentID := seedChat(t, ctx, ti, authCtx.UserID, "", "original session")
	childID := seedChat(t, ctx, ti, authCtx.UserID, "", "continued session")
	seedSessionLink(t, ctx, ti, parentID, &childID, "claude-code")

	fromParent, err := ti.service.ListSessionLinks(ctx, &gen.ListSessionLinksPayload{
		SessionToken:      nil,
		ProjectSlugInput:  nil,
		ChatSessionsToken: nil,
		ChatIds:           []string{parentID.String()},
	})
	require.NoError(t, err)
	require.Len(t, fromParent.Links, 1)
	link := fromParent.Links[0]
	require.Equal(t, parentID.String(), link.ParentChatID)
	require.NotNil(t, link.ChildChatID)
	require.Equal(t, childID.String(), *link.ChildChatID)
	require.True(t, link.ChildCaptured)
	require.NotNil(t, link.ParentTitle)
	require.Equal(t, "original session", *link.ParentTitle)
	require.NotNil(t, link.ChildTitle)
	require.Equal(t, "continued session", *link.ChildTitle)
	require.True(t, link.ParentCaptured)
	require.Equal(t, "claude-code", link.TargetHarness)
	require.Equal(t, "move", link.Kind)

	fromChild, err := ti.service.ListSessionLinks(ctx, &gen.ListSessionLinksPayload{
		SessionToken:      nil,
		ProjectSlugInput:  nil,
		ChatSessionsToken: nil,
		ChatIds:           []string{childID.String()},
	})
	require.NoError(t, err)
	require.Len(t, fromChild.Links, 1)
	require.Equal(t, parentID.String(), fromChild.Links[0].ParentChatID)
}

// A move whose continuation id was unknowable (Cursor) renders as a dangling
// edge: no child id, no child title, not navigable — but still shown.
func TestListSessionLinks_DanglingEdge(t *testing.T) {
	t.Parallel()

	ti := newTestChatService(t)
	ctx := initSessionCtx(t, ti)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	parentID := seedChat(t, ctx, ti, authCtx.UserID, "", "moved to cursor")
	seedSessionLink(t, ctx, ti, parentID, nil, "cursor")

	res, err := ti.service.ListSessionLinks(ctx, &gen.ListSessionLinksPayload{
		SessionToken:      nil,
		ProjectSlugInput:  nil,
		ChatSessionsToken: nil,
		ChatIds:           []string{parentID.String()},
	})
	require.NoError(t, err)
	require.Len(t, res.Links, 1)
	link := res.Links[0]
	require.Nil(t, link.ChildChatID)
	require.Nil(t, link.ChildTitle)
	require.False(t, link.ChildCaptured)
	require.Equal(t, "cursor", link.TargetHarness)
}

// Members without chat:read see only edges touching their own sessions — a
// crafted chat_ids list must not leak another member's titles or moves.
func TestListSessionLinks_MemberVisibilityScoped(t *testing.T) {
	t.Parallel()

	ti := newTestChatService(t)
	adminCtx := initSessionCtx(t, ti)

	otherParent := seedChat(t, adminCtx, ti, "user-someone-else", "", "someone else's session")
	otherChild := seedChat(t, adminCtx, ti, "user-someone-else", "", "their continuation")
	seedSessionLink(t, adminCtx, ti, otherParent, &otherChild, "claude-code")

	memberCtx, memberUserID := memberSessionCtx(t, ti)
	mineParent := seedChat(t, memberCtx, ti, memberUserID, "", "my session")
	seedSessionLink(t, memberCtx, ti, mineParent, nil, "cursor")
	// An edge from the member's chat into another member's chat is visible
	// (the member owns one end) but the foreign end's title and captured
	// state must be masked.
	crossChild := seedChat(t, adminCtx, ti, "user-someone-else", "", "their other continuation")
	seedSessionLink(t, memberCtx, ti, mineParent, &crossChild, "claude-code")

	res, err := ti.service.ListSessionLinks(memberCtx, &gen.ListSessionLinksPayload{
		SessionToken:      nil,
		ProjectSlugInput:  nil,
		ChatSessionsToken: nil,
		ChatIds:           []string{otherParent.String(), mineParent.String()},
	})
	require.NoError(t, err)
	require.Len(t, res.Links, 2, "only edges touching the member's own chats are visible")
	for _, link := range res.Links {
		require.Equal(t, mineParent.String(), link.ParentChatID)
		require.True(t, link.ParentCaptured)
		require.Nil(t, link.ChildTitle, "foreign child titles stay masked")
		require.False(t, link.ChildCaptured, "foreign children read as not navigable")
	}

	adminRes, err := ti.service.ListSessionLinks(adminCtx, &gen.ListSessionLinksPayload{
		SessionToken:      nil,
		ProjectSlugInput:  nil,
		ChatSessionsToken: nil,
		ChatIds:           []string{otherParent.String(), mineParent.String()},
	})
	require.NoError(t, err)
	require.Len(t, adminRes.Links, 3, "chat:read holders see every edge")
	for _, link := range adminRes.Links {
		if link.ChildChatID != nil && *link.ChildChatID == crossChild.String() {
			require.NotNil(t, link.ChildTitle, "unrestricted callers see the real title")
			require.True(t, link.ChildCaptured)
		}
	}
}
