package chat_test

import (
	"github.com/google/uuid"
	"testing"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
)

func TestConversationEvidenceUsesPlatformMemberOwnershipAndExactGrants(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := t.Context()
	auth := &contextvalues.AuthContext{UserID: "owner", ActiveOrganizationID: ti.orgID, ProjectID: &ti.projectID}
	chatID := seedChat(t, ctx, ti, auth.UserID, "external-owner", "Evidence example")
	member := *auth
	member.SessionID = nil
	ctx = contextvalues.WithAuthenticatedActor(ctx, &member, urn.NewPrincipal(urn.PrincipalTypeUser, member.UserID))
	ctx = contextvalues.SetActingSurface(ctx, contextvalues.ActingSurfacePlatformMCP)
	ctx = authztest.WithExactGrants(t, ctx)
	allowed, err := ti.service.ReadableConversations(ctx, []string{chatID.String()})
	require.NoError(t, err)
	require.Equal(t, []string{chatID.String()}, allowed, "an authenticated owner does not need an unrelated external-user token")
	untrusted := contextvalues.SetAuthContext(t.Context(), &member)
	allowed, err = ti.service.ReadableConversations(untrusted, []string{chatID.String()})
	require.NoError(t, err)
	require.Empty(t, allowed, "a sessionless external-user context cannot inherit the Platform member bypass")
	foreign := member
	foreign.ProjectID = new(uuid.New())
	allowed, err = ti.service.ReadableConversations(contextvalues.SetAuthContext(ctx, &foreign), []string{chatID.String()})
	require.NoError(t, err)
	require.Empty(t, allowed, "exact project scoping also applies to owners")
	other := seedChat(t, ctx, ti, "another-user", "another-external-owner", "Private example")
	allowed, err = ti.service.ReadableConversations(ctx, []string{other.String()})
	require.NoError(t, err)
	require.Empty(t, allowed)
	granted := authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeChatRead, other.String()))
	allowed, err = ti.service.ReadableConversations(granted, []string{chatID.String(), other.String()})
	require.NoError(t, err)
	require.ElementsMatch(t, []string{chatID.String(), other.String()}, allowed)
	missing, err := ti.service.ReadableConversations(granted, []string{uuid.NewString()})
	require.NoError(t, err)
	require.Empty(t, missing)
	support := member
	support.IsAdmin = true
	support.SupportOrganizationID = support.ActiveOrganizationID
	supportCtx := contextvalues.WithValidatedSupportSession(contextvalues.SetAuthContext(granted, &support), &support)
	require.True(t, contextvalues.IsSupportSession(supportCtx))
	allowed, err = ti.service.ReadableConversations(supportCtx, []string{other.String()})
	require.NoError(t, err)
	require.Empty(t, allowed, "support impersonation cannot obtain evidence references")
}
