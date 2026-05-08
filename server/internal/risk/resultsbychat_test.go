package risk_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/risk"
	"github.com/speakeasy-api/gram/server/internal/authz"
	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

func seedChatWithUser(t *testing.T, ti *testInstance, projectID uuid.UUID, orgID, externalUserID string) (uuid.UUID, uuid.UUID) {
	t.Helper()
	ctx := t.Context()

	chatID, err := uuid.NewV7()
	require.NoError(t, err)

	_, err = ti.chatRepo.UpsertChat(ctx, chatrepo.UpsertChatParams{
		ID:             chatID,
		ProjectID:      projectID,
		OrganizationID: orgID,
		UserID:         pgtype.Text{},
		ExternalUserID: pgtype.Text{String: externalUserID, Valid: externalUserID != ""},
		Title:          pgtype.Text{String: "test chat", Valid: true},
	})
	require.NoError(t, err)

	msgID, err := testrepo.New(ti.conn).InsertChatMessage(ctx, testrepo.InsertChatMessageParams{
		ChatID:    chatID,
		ProjectID: uuid.NullUUID{UUID: projectID, Valid: true},
		Role:      "user",
		Content:   "test message",
	})
	require.NoError(t, err)

	return chatID, msgID
}

func TestListRiskResultsByChat_GroupsFindings(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)},
	)

	policy, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{Name: new("ByChat Test")})
	require.NoError(t, err)

	policyID, _ := uuid.Parse(policy.ID)

	// Create two chats, seed findings in both
	_, msg1 := seedChatWithUser(t, ti, *authCtx.ProjectID, authCtx.ActiveOrganizationID, "alice@example.com")
	_, msg2 := seedChatWithUser(t, ti, *authCtx.ProjectID, authCtx.ActiveOrganizationID, "bob@example.com")

	seedRiskResult(t, ti, *authCtx.ProjectID, authCtx.ActiveOrganizationID, policyID, 1, msg1, true)
	seedRiskResult(t, ti, *authCtx.ProjectID, authCtx.ActiveOrganizationID, policyID, 1, msg1, true)
	seedRiskResult(t, ti, *authCtx.ProjectID, authCtx.ActiveOrganizationID, policyID, 1, msg2, true)

	result, err := ti.service.ListRiskResultsByChat(ctx, &gen.ListRiskResultsByChatPayload{})
	require.NoError(t, err)
	require.Len(t, result.Chats, 2)

	// Results are ordered by latest_detected DESC; both have same time so order may vary.
	// Just verify the counts sum to 3.
	totalFindings := result.Chats[0].FindingsCount + result.Chats[1].FindingsCount
	require.Equal(t, int64(3), totalFindings)
}

func TestListRiskResultsByChat_ExcludesNotFound(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)},
	)

	policy, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{Name: new("ByChat NotFound")})
	require.NoError(t, err)

	policyID, _ := uuid.Parse(policy.ID)
	_, msgID := seedChatWithUser(t, ti, *authCtx.ProjectID, authCtx.ActiveOrganizationID, "")
	seedRiskResult(t, ti, *authCtx.ProjectID, authCtx.ActiveOrganizationID, policyID, 1, msgID, false)

	result, err := ti.service.ListRiskResultsByChat(ctx, &gen.ListRiskResultsByChatPayload{})
	require.NoError(t, err)
	require.Empty(t, result.Chats)
}

func TestListRiskResultsByChat_CursorPagination(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)},
	)

	policy, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{Name: new("ByChat Cursor")})
	require.NoError(t, err)

	policyID, _ := uuid.Parse(policy.ID)

	// Create 3 chats with findings
	for range 3 {
		_, msgID := seedChatWithUser(t, ti, *authCtx.ProjectID, authCtx.ActiveOrganizationID, "")
		seedRiskResult(t, ti, *authCtx.ProjectID, authCtx.ActiveOrganizationID, policyID, 1, msgID, true)
	}

	// First page without cursor returns all 3 (< page size of 50)
	result, err := ti.service.ListRiskResultsByChat(ctx, &gen.ListRiskResultsByChatPayload{})
	require.NoError(t, err)
	require.Len(t, result.Chats, 3)
	require.Nil(t, result.NextCursor)
}

func TestListRiskResultsByChat_Unauthorized(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)
	ctx = withExactAccessGrants(t, ctx, ti.conn)

	_, err := ti.service.ListRiskResultsByChat(ctx, &gen.ListRiskResultsByChatPayload{})
	require.Error(t, err)
}

func TestListRiskResultsByChat_IncludesExternalUserID(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)},
	)

	policy, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{Name: new("ByChat User")})
	require.NoError(t, err)

	policyID, _ := uuid.Parse(policy.ID)
	_, msgID := seedChatWithUser(t, ti, *authCtx.ProjectID, authCtx.ActiveOrganizationID, "alice@example.com")
	seedRiskResult(t, ti, *authCtx.ProjectID, authCtx.ActiveOrganizationID, policyID, 1, msgID, true)

	result, err := ti.service.ListRiskResultsByChat(ctx, &gen.ListRiskResultsByChatPayload{})
	require.NoError(t, err)
	require.Len(t, result.Chats, 1)
	require.NotNil(t, result.Chats[0].UserID)
	require.Equal(t, "alice@example.com", *result.Chats[0].UserID)
}
