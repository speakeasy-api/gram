package assistants

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/agents/runtimepolicy"
	"github.com/speakeasy-api/gram/server/internal/auth/assistanttokens"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/chat"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
)

func TestKitInferenceLiveAdmissionAndCaptureOwnership(t *testing.T) {
	db, err := assistantsInfra.CloneTestDatabase(t, "kit_inference_live")
	require.NoError(t, err)
	project := newProvisioningProject(t, db, "kit-inference-live")
	seedProjectRead(t, db, "user-1", project)
	seedMCPConnect(t, db, "user-1", project)
	core := newProvisioningCore(t, db)
	assistant, err := core.CreateAssistant(t.Context(), "org-test", project, "user-1", "Inference", "openai/gpt-4o-mini", "", nil, nil, 300, 1, StatusActive, true, nil)
	require.NoError(t, err)
	trigger, err := core.resolveDashboardTriggerInstance(t.Context(), "org-test", project, assistant.ID, assistant.Name)
	require.NoError(t, err)
	resolution, err := core.identities.Resolve(t.Context(), db, "org-test", project, assistant.ID, trigger)
	require.NoError(t, err)
	require.NotNil(t, resolution.Identity)
	runtime := uuid.New()
	_, err = db.Exec(t.Context(), `INSERT INTO assistant_runtimes(id,project_id,assistant_id,assistant_thread_id,backend,state,runtime_version) VALUES($1,$2,$3,$4,'local','active',3)`, runtime, project, assistant.ID, uuid.Nil)
	require.NoError(t, err)
	thread := seedThreadWithEvent(t, db, assistant.ID, "inference", "inference", eventStatusPending)
	var chatID uuid.UUID
	require.NoError(t, db.QueryRow(t.Context(), `SELECT chat_id FROM assistant_threads WHERE project_id=$1 AND id=$2`, project, thread).Scan(&chatID))
	engine := authz.NewEngine(testenv.NewLogger(t), db, authztest.ChallengeLoggingAlwaysDisabled, nil, authz.EngineOpts{AdmitPrincipalCredential: runtimepolicy.AdmitPrincipalCredential, AdmitWorkloadSession: runtimepolicy.AdmitWorkloadSession})
	manager := assistanttokens.New("test-secret", db, engine, newTestPrincipalCredentials(t, db), nil)
	issue := func() assistanttokens.InferenceCredential {
		t.Helper()
		credential, err := manager.IssueInferenceCredential(t.Context(), assistanttokens.InferenceIssueInput{Identity: *resolution.Identity, RuntimeID: runtime, Lifetime: 24 * time.Hour})
		require.NoError(t, err)
		return credential
	}
	credential := issue()
	ctx, err := manager.AuthorizeInference(t.Context(), "Bearer "+credential.Token)
	require.NoError(t, err)
	actor, ok := contextvalues.AuthenticatedActor(ctx)
	require.True(t, ok)
	require.Equal(t, urn.PrincipalTypeWorkload, actor.Type)
	principal, ok := contextvalues.PrincipalCredentialAuthorization(ctx)
	require.True(t, ok)
	require.Empty(t, principal.AuthorizerUserID)
	_, _, err = manager.Authorize(t.Context(), credential.Token)
	require.Error(t, err, "inference bearer cannot authorize transcript/legacy APIs")
	ctx, err = manager.BindInferenceChat(ctx, chatID)
	require.NoError(t, err)
	require.True(t, assistanttokens.InferenceTranscriptOwned(ctx))
	// The trusted context bypasses both ingress and response persistence while
	// leaving chat identity available to telemetry; nil dependencies prove neither
	// branch can accidentally reach the canonical writer.
	capture := new(chat.ChatMessageCaptureStrategy)
	request := openrouter.CompletionRequest{ChatID: chatID, ProjectID: project.String(), OrgID: "org-test"}
	session, err := capture.StartOrResumeChat(ctx, request)
	require.NoError(t, err)
	require.Nil(t, session)
	require.NoError(t, capture.CaptureMessage(ctx, session, request, openrouter.CompletionResponse{}))
	require.Equal(t, chatID, request.ChatID)
	var messages int64
	require.NoError(t, db.QueryRow(t.Context(), `SELECT count(*) FROM chat_messages WHERE project_id=$1 AND chat_id=$2`, project, chatID).Scan(&messages))
	require.Zero(t, messages)
	// Runtime-wide revoke is the restore/startup rotation boundary.
	require.NoError(t, manager.RevokeRuntimeInferenceCredentials(t.Context(), project, runtime))
	_, err = manager.AuthorizeInference(t.Context(), credential.Token)
	require.Error(t, err)
	credential = issue()
	_, err = db.Exec(t.Context(), `UPDATE assistant_inference_credentials SET created_at=clock_timestamp()-interval '2 hours',expires_at=clock_timestamp()-interval '1 hour' WHERE project_id=$1 AND id=$2`, project, credential.ID)
	require.NoError(t, err)
	_, err = manager.AuthorizeInference(t.Context(), credential.Token)
	require.Error(t, err, "persisted expiry is enforced")
	credential = issue()
	_, err = db.Exec(t.Context(), `UPDATE agents SET suspended_at=clock_timestamp() WHERE project_id=$1 AND id=$2`, project, resolution.Identity.AgentID)
	require.NoError(t, err)
	_, err = manager.AuthorizeInference(t.Context(), credential.Token)
	require.Error(t, err, "live workload eligibility is checked on every request")
	_, err = db.Exec(t.Context(), `UPDATE agents SET suspended_at=NULL WHERE project_id=$1 AND id=$2`, project, resolution.Identity.AgentID)
	require.NoError(t, err)
	ctx, err = manager.AuthorizeInference(t.Context(), credential.Token)
	require.NoError(t, err)
	check := authz.MCPCheck(authz.ScopeMCPConnect, uuid.NewString(), project.String())
	require.NoError(t, engine.Require(ctx, check))
	deleteAgentScope(t, db, resolution.Identity.AgentID.String(), authz.ScopeMCPConnect)
	deleteAgentScope(t, db, resolution.Identity.AgentID.String(), authz.ScopeMCPRead)
	ctx, err = manager.AuthorizeInference(t.Context(), credential.Token)
	require.NoError(t, err)
	require.Error(t, engine.Require(ctx, check), "live policy narrows the persisted issuance ceiling")
	require.NoError(t, manager.RevokeInferenceCredential(t.Context(), project, credential.ID))
	_, err = manager.AuthorizeInference(t.Context(), credential.Token)
	require.Error(t, err)
}
