package agent_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/agent"
	"github.com/speakeasy-api/gram/server/internal/chat"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	hooksrepo "github.com/speakeasy-api/gram/server/internal/hooks/repo"
)

// seedCapturedChat inserts a chat row the way hook ingest does, owned by the
// given user id, and returns the chat id derived from the session id.
func seedCapturedChat(t *testing.T, ti *testInstance, sessionID, userID, title string) uuid.UUID {
	t.Helper()
	chatID := chat.SessionIDToChatID(sessionID)
	_, err := hooksrepo.New(ti.conn).UpsertClaudeCodeSession(t.Context(), hooksrepo.UpsertClaudeCodeSessionParams{
		ID:             chatID,
		ProjectID:      ti.projectID,
		OrganizationID: ti.orgID,
		UserID:         conv.ToPGTextEmpty(userID),
		ExternalUserID: conv.ToPGTextEmpty("dev@acme.corp"),
		UserAccountID:  uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		Title:          conv.ToPGTextEmpty(title),
		Cwd:            conv.ToPGTextEmpty("/home/dev/code/api"),
	})
	require.NoError(t, err)
	return chatID
}

// The fleet-shared org install key must never read session metadata: it is
// per-user chat data, and the vouched-email path would let any key holder
// enumerate any employee's session titles.
func TestGetSessionMeta_InstallKeyRefused(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAgentService(t)

	_, err := ti.service.GetSessionMeta(ctx, &gen.GetSessionMetaPayload{
		SessionIds: []string{uuid.NewString()},
	})
	require.Error(t, err)
	require.ErrorContains(t, err, "per-user agent key")
}

// A per-user key sees exactly the sessions its owner has captured chats for:
// other users' sessions and unknown ids are silently omitted, and non-UUID
// session ids resolve through the same mapping hook ingest uses.
func TestGetSessionMeta_OwnerMatchAndEcho(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAgentService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotEmpty(t, authCtx.UserID, "test auth context must carry a user id for owner matching")

	ownedUUID := uuid.NewString()
	ownedOpaque := "ses_" + uuid.NewString()
	foreign := uuid.NewString()

	ownedChatID := seedCapturedChat(t, ti, ownedUUID, authCtx.UserID, "fix flaky auth test")
	seedCapturedChat(t, ti, ownedOpaque, authCtx.UserID, "opencode session")
	seedCapturedChat(t, ti, foreign, "some-other-user", "not yours")

	userCtx := withPerUserKeyAuth(t, ctx, "dev@acme.corp")
	res, err := ti.service.GetSessionMeta(userCtx, &gen.GetSessionMetaPayload{
		SessionIds: []string{ownedUUID, ownedOpaque, foreign, uuid.NewString()},
	})
	require.NoError(t, err)
	require.Len(t, res.Sessions, 2, "only the caller's captured sessions resolve")

	byID := map[string]*gen.AgentSessionMeta{}
	for _, s := range res.Sessions {
		byID[s.SessionID] = s
	}
	require.Contains(t, byID, ownedUUID)
	require.Contains(t, byID, ownedOpaque, "non-UUID session ids resolve via the ingest mapping")
	require.Equal(t, ownedChatID.String(), byID[ownedUUID].ChatID)
	require.NotNil(t, byID[ownedUUID].Title)
	require.Equal(t, "fix flaky auth test", *byID[ownedUUID].Title)
}

// Personal-account sessions resolve for their owner like any other session
// (Q2 decision, 2026-08-10):
// the caller is the authenticated owner reading their own metadata. Owner
// matching still applies — this test pins the inclusion so a future privacy
// tightening is a deliberate choice, not silent drift.
func TestGetSessionMeta_PersonalAccountIncludedForOwner(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAgentService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	account, err := hooksrepo.New(ti.conn).UpsertUserAccount(ctx, hooksrepo.UpsertUserAccountParams{
		OrganizationID:      ti.orgID,
		Provider:            "anthropic",
		ExternalAccountUuid: "acct-" + uuid.NewString(),
		UserID:              conv.ToPGTextEmpty(authCtx.UserID),
		ExternalOrgID:       conv.ToPGTextEmpty(""),
		ExternalAccountID:   conv.ToPGTextEmpty(""),
		Email:               conv.ToPGTextEmpty("personal@example.com"),
		AccountType:         conv.ToPGTextEmpty("personal"),
	})
	require.NoError(t, err)
	accountID := account.ID

	sessionID := uuid.NewString()
	chatID := chat.SessionIDToChatID(sessionID)
	_, err = hooksrepo.New(ti.conn).UpsertClaudeCodeSession(ctx, hooksrepo.UpsertClaudeCodeSessionParams{
		ID:             chatID,
		ProjectID:      ti.projectID,
		OrganizationID: ti.orgID,
		UserID:         conv.ToPGTextEmpty(authCtx.UserID),
		ExternalUserID: conv.ToPGTextEmpty("dev@acme.corp"),
		UserAccountID:  uuid.NullUUID{UUID: accountID, Valid: true},
		Title:          conv.ToPGTextEmpty("personal side project"),
		Cwd:            conv.ToPGTextEmpty(""),
	})
	require.NoError(t, err)

	userCtx := withPerUserKeyAuth(t, ctx, "dev@acme.corp")
	res, err := ti.service.GetSessionMeta(userCtx, &gen.GetSessionMetaPayload{
		SessionIds: []string{sessionID},
	})
	require.NoError(t, err)
	require.Len(t, res.Sessions, 1, "the owner's personal-account sessions resolve like any other")
	require.Equal(t, chatID.String(), res.Sessions[0].ChatID)
	require.NotNil(t, res.Sessions[0].Title)
	require.Equal(t, "personal side project", *res.Sessions[0].Title)
}

// The whole surface stays dark until the organization is enrolled in the
// session_portability product feature.
func TestGetSessionMeta_FeatureDisabled(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAgentService(t)
	ti.features.sessionPortability = false

	userCtx := withPerUserKeyAuth(t, ctx, "dev@acme.corp")
	_, err := ti.service.GetSessionMeta(userCtx, &gen.GetSessionMetaPayload{
		SessionIds: []string{uuid.NewString()},
	})
	require.Error(t, err)
	require.ErrorContains(t, err, "not enabled")
}
