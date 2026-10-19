package chat_test

import (
	"goa.design/goa/v3/security"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/speakeasy-api/gram/server/internal/assistantidentity"
	assistantsrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/auth/assistanttokens"
	"github.com/speakeasy-api/gram/server/internal/billing"
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
	_, err = ti.conn.Exec(ctx, "UPDATE assistants SET runtime_version = 3 WHERE id = $1", assistantID)
	require.NoError(t, err)
	trigger, err := triggerrepo.New(ti.conn).CreateTriggerInstance(ctx, triggerrepo.CreateTriggerInstanceParams{
		OrganizationID: ti.orgID, ProjectID: ti.projectID, DefinitionSlug: "slack", Name: "Test trigger", TargetKind: "assistant", TargetRef: assistantID.String(), TargetDisplay: "Assistant", ConfigJson: []byte(`{}`), Status: "active",
	})
	require.NoError(t, err)
	identities := assistantidentity.New("https://example.test", audit.NewLogger())
	tx, err := ti.conn.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx)
	require.NoError(t, identities.Provision(ctx, tx, assistantidentity.ProvisionParams{OrganizationID: ti.orgID, ProjectID: ti.projectID, AssistantID: assistantID, ActorUserID: "test-owner"}))
	require.NoError(t, tx.Commit(ctx))
	resolved, err := identities.Resolve(ctx, ti.conn, ti.orgID, ti.projectID, assistantID, trigger.ID)
	require.NoError(t, err)
	require.NotNil(t, resolved.Identity)
	runtimeID := uuid.New()
	now := pgtype.Timestamptz{Time: time.Now(), Valid: true}
	require.NoError(t, assistantsrepo.New(ti.conn).CreateAssistantRuntime(ctx, assistantsrepo.CreateAssistantRuntimeParams{
		ID: runtimeID, AssistantID: assistantID, ProjectID: ti.projectID, Backend: "local", BackendMetadataJson: []byte(`{}`), State: "running", WarmUntil: now, LastHeartbeatAt: now, UpdatedAt: now,
	}))
	_, err = ti.conn.Exec(ctx, "UPDATE assistant_runtimes SET runtime_version = 3 WHERE id = $1", runtimeID)
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
	_, err = ti.conn.Exec(ctx, "UPDATE assistant_runtimes SET ended_at = clock_timestamp() WHERE id = $1", runtimeID)
	require.NoError(t, err)
	_, _, _, err = ti.service.TestingAuthorizeCompletion(request(key.ProjectSlug), true)
	require.Error(t, err, "ended runtime must fail even while its API key is live")
	_, err = ti.conn.Exec(ctx, "UPDATE assistant_runtimes SET ended_at = NULL WHERE id = $1", runtimeID)
	require.NoError(t, err)
	_, err = ti.conn.Exec(ctx, "UPDATE agents SET suspended_at = clock_timestamp() WHERE id = $1", resolved.Identity.AgentID)
	require.NoError(t, err)
	_, _, _, err = ti.service.TestingAuthorizeCompletion(request(key.ProjectSlug), true)
	require.Error(t, err, "live actor suspension must reject the runtime key")
	_, err = ti.conn.Exec(ctx, "UPDATE agents SET suspended_at = NULL WHERE id = $1", resolved.Identity.AgentID)
	require.NoError(t, err)
	_, _, _, err = ti.service.TestingAuthorizeCompletion(request(key.ProjectSlug), true)
	require.NoError(t, err)
	_, err = ti.conn.Exec(ctx, "UPDATE assistant_inference_credentials SET revoked_at = clock_timestamp() WHERE id = $1", key.ID)
	require.NoError(t, err)
	_, _, _, err = ti.service.TestingAuthorizeCompletion(request(key.ProjectSlug), true)
	require.Error(t, err, "revoked binding must not fall back to ordinary chat")
	_, err = ti.conn.Exec(ctx, "DELETE FROM assistant_inference_credentials WHERE id = $1", key.ID)
	require.NoError(t, err)
	_, _, _, err = ti.service.TestingAuthorizeCompletion(request(key.ProjectSlug), true)
	require.Error(t, err, "unbound principal key must not enter completion authentication")
	_, err = ti.service.APIKeyAuth(ctx, key.Key, &security.APIKeyScheme{Name: constants.KeySecurityScheme, RequiredScopes: []string{"consumer"}})
	require.NoError(t, err, "unbound key remains a valid principal credential")
	_, err = ti.service.APIKeyAuth(ctx, key.Key, &security.APIKeyScheme{Name: constants.KeySecurityScheme, RequiredScopes: []string{"chat"}})
	require.Error(t, err, "principal keys remain forbidden on transcript transport")
}
