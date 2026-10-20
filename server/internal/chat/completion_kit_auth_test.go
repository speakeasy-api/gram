package chat_test

import (
	"goa.design/goa/v3/security"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	agentrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/assistantidentity"
	assistantsrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/auth/assistanttokens"
	"github.com/speakeasy-api/gram/server/internal/billing"
	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/constants"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	triggerrepo "github.com/speakeasy-api/gram/server/internal/triggers/repo"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
	"github.com/stretchr/testify/require"
)

func TestCompletionKitAPIKeyAdmission(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := t.Context()
	_, err := usersrepo.New(ti.conn).UpsertUser(ctx, usersrepo.UpsertUserParams{ID: "test-owner", Email: "owner@example.test", DisplayName: "Test owner"})
	require.NoError(t, err)
	_, err = orgrepo.New(ti.conn).UpsertOrganizationUserRelationship(ctx, orgrepo.UpsertOrganizationUserRelationshipParams{OrganizationID: ti.orgID, UserID: pgtype.Text{String: "test-owner", Valid: true}})
	require.NoError(t, err)
	assistantID := seedAssistant(t, ctx, ti)
	err = assistantsrepo.New(ti.conn).SetAssistantDesiredRuntimeVersionForTest(ctx, assistantsrepo.SetAssistantDesiredRuntimeVersionForTestParams{AssistantID: assistantID, ProjectID: ti.projectID, RuntimeVersion: 3})
	require.NoError(t, err)
	trigger, err := triggerrepo.New(ti.conn).CreateTriggerInstance(ctx, triggerrepo.CreateTriggerInstanceParams{
		OrganizationID: ti.orgID, ProjectID: ti.projectID, DefinitionSlug: "slack", Name: "Test trigger", TargetKind: "assistant", TargetRef: assistantID.String(), TargetDisplay: "Assistant", ConfigJson: []byte(`{}`), Status: "active",
	})
	require.NoError(t, err)
	identities := assistantidentity.New("https://example.test", audit.NewLogger())
	require.NoError(t, pgx.BeginFunc(ctx, ti.conn, func(tx pgx.Tx) error {
		require.NoError(t, identities.Provision(ctx, tx, assistantidentity.ProvisionParams{OrganizationID: ti.orgID, ProjectID: ti.projectID, AssistantID: assistantID, ActorUserID: "test-owner"}))
		return nil
	}))
	resolved, err := identities.Resolve(ctx, ti.conn, ti.orgID, ti.projectID, assistantID, trigger.ID)
	require.NoError(t, err)
	require.NotNil(t, resolved.Identity)
	runtimeID := uuid.New()
	now := pgtype.Timestamptz{Time: time.Now(), Valid: true}
	require.NoError(t, assistantsrepo.New(ti.conn).CreateAssistantRuntime(ctx, assistantsrepo.CreateAssistantRuntimeParams{
		ID: runtimeID, AssistantID: assistantID, ProjectID: ti.projectID, Backend: "local", BackendMetadataJson: []byte(`{}`), State: "active", WarmUntil: now, LastHeartbeatAt: now, UpdatedAt: now,
	}))
	err = chatrepo.New(ti.conn).SetKitCompletionRuntimeFixture(ctx, chatrepo.SetKitCompletionRuntimeFixtureParams{RuntimeID: runtimeID, ProjectID: ti.projectID, RuntimeVersion: 3, EndedAt: pgtype.Timestamptz{}})
	require.NoError(t, err)
	tokens := ti.service.TestingCompletionTokens()
	key, err := tokens.IssueKitAPIKey(ctx, assistanttokens.InferenceIssueInput{Identity: *resolved.Identity, RuntimeID: runtimeID, Lifetime: time.Hour}, "test")
	require.NoError(t, err)
	request := func(project string) *http.Request {
		r := httptest.NewRequestWithContext(ctx, http.MethodPost, "/chat/completions", nil)
		r.Header.Set(constants.APIKeyHeader, key.Key)
		r.Header.Set(constants.ProjectHeader, project)
		return r
	}
	_, err = ti.service.APIKeyAuth(ctx, key.Key, &security.APIKeyScheme{Name: constants.KeySecurityScheme, RequiredScopes: []string{"consumer"}})
	require.NoError(t, err)
	authorized, ac, source, err := ti.service.TestingAuthorizeCompletion(request(key.ProjectSlug), true)
	require.NoError(t, err)
	require.Equal(t, key.ID.String(), ac.APIKeyID)
	require.Equal(t, billing.ModelUsageSourceAssistants, source)
	require.True(t, assistanttokens.InferenceTranscriptOwned(authorized))
	_, _, _, err = ti.service.TestingAuthorizeCompletion(request(key.ProjectSlug), false)
	require.Error(t, err, "runtime key must not enter transcript authentication")
	_, _, _, err = ti.service.TestingAuthorizeCompletion(request("wrong-project"), true)
	require.Error(t, err)
	for _, state := range []string{"failed", "expired", "stopped"} {
		require.NoError(t, assistantsrepo.New(ti.conn).SetAssistantRuntimeStateForTest(ctx, assistantsrepo.SetAssistantRuntimeStateForTestParams{ProjectID: ti.projectID, RuntimeID: runtimeID, State: state, UpdatedAt: now}))
		_, _, _, err = ti.service.TestingAuthorizeCompletion(request(key.ProjectSlug), true)
		require.Error(t, err, "non-live runtime %s must reject its key", state)
	}
	require.NoError(t, assistantsrepo.New(ti.conn).SetAssistantRuntimeStateForTest(ctx, assistantsrepo.SetAssistantRuntimeStateForTestParams{ProjectID: ti.projectID, RuntimeID: runtimeID, State: "active", UpdatedAt: now}))
	err = chatrepo.New(ti.conn).SetKitCompletionRuntimeFixture(ctx, chatrepo.SetKitCompletionRuntimeFixtureParams{RuntimeID: runtimeID, ProjectID: ti.projectID, RuntimeVersion: 3, EndedAt: now})
	require.NoError(t, err)
	_, _, _, err = ti.service.TestingAuthorizeCompletion(request(key.ProjectSlug), true)
	require.Error(t, err, "ended runtime must fail even while its API key is live")
	err = chatrepo.New(ti.conn).SetKitCompletionRuntimeFixture(ctx, chatrepo.SetKitCompletionRuntimeFixtureParams{RuntimeID: runtimeID, ProjectID: ti.projectID, RuntimeVersion: 3, EndedAt: pgtype.Timestamptz{}})
	require.NoError(t, err)
	_, err = agentrepo.New(ti.conn).SuspendAgent(ctx, agentrepo.SuspendAgentParams{OrganizationID: ti.orgID, ID: resolved.Identity.AgentID})
	require.NoError(t, err)
	_, _, _, err = ti.service.TestingAuthorizeCompletion(request(key.ProjectSlug), true)
	require.Error(t, err, "live actor suspension must reject the runtime key")
	_, err = agentrepo.New(ti.conn).ResumeAgent(ctx, agentrepo.ResumeAgentParams{OrganizationID: ti.orgID, ID: resolved.Identity.AgentID})
	require.NoError(t, err)
	_, _, _, err = ti.service.TestingAuthorizeCompletion(request(key.ProjectSlug), true)
	require.NoError(t, err)
	err = chatrepo.New(ti.conn).RevokeKitCompletionBindingFixture(ctx, chatrepo.RevokeKitCompletionBindingFixtureParams{ID: key.ID, ProjectID: ti.projectID})
	require.NoError(t, err)
	_, _, _, err = ti.service.TestingAuthorizeCompletion(request(key.ProjectSlug), true)
	require.Error(t, err, "revoked binding must not fall back to ordinary chat")
	err = chatrepo.New(ti.conn).DeleteKitCompletionBindingFixture(ctx, chatrepo.DeleteKitCompletionBindingFixtureParams{ID: key.ID, ProjectID: ti.projectID})
	require.NoError(t, err)
	_, _, _, err = ti.service.TestingAuthorizeCompletion(request(key.ProjectSlug), true)
	require.Error(t, err, "unbound principal key must not enter completion authentication")
	_, err = ti.service.APIKeyAuth(ctx, key.Key, &security.APIKeyScheme{Name: constants.KeySecurityScheme, RequiredScopes: []string{"consumer"}})
	require.NoError(t, err, "unbound key remains a valid principal credential")
	_, err = ti.service.APIKeyAuth(ctx, key.Key, &security.APIKeyScheme{Name: constants.KeySecurityScheme, RequiredScopes: []string{"chat"}})
	require.Error(t, err, "principal keys remain forbidden on transcript transport")
}
