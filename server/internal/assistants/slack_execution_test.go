package assistants

import (
	"encoding/json"
	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/assistantidentity"
	identityrepo "github.com/speakeasy-api/gram/server/internal/assistantidentity/repo"
	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/auth/assistanttokens"
	bgtriggers "github.com/speakeasy-api/gram/server/internal/background/triggers"
	slackrepo "github.com/speakeasy-api/gram/server/internal/slackdirectoryconnections/repo"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestSlackExecutionIngressSharesConversation(t *testing.T) {
	t.Parallel()
	db, err := assistantsInfra.CloneTestDatabase(t, "slack_invocation_isolation")
	require.NoError(t, err)
	project := newProvisioningProject(t, db, "slack-invocations")
	core := newProvisioningCore(t, db)
	assistant, err := core.CreateAssistant(t.Context(), "org-test", project, "user-1", "Slack", "openai/gpt-4o-mini", "", nil, nil, 300, 1, StatusActive)
	require.NoError(t, err)
	root, err := core.resolveDashboardTriggerInstance(t.Context(), "org-test", project, assistant.ID, assistant.Name)
	require.NoError(t, err)
	require.NoError(t, identityrepo.New(db).FixtureSlackExecutionMapping(t.Context(), identityrepo.FixtureSlackExecutionMappingParams{UserID: "user-2", OrganizationID: "org-test", SlackTeamID: "TEXAMPLE", SlackUserID: "UEXAMPLE", Generation: uuid.New()}))
	task := bgtriggers.Task{TargetKind: bgtriggers.TargetKindAssistant, TargetRef: assistant.ID.String(), TriggerInstanceID: root.String(), DefinitionSlug: sourceKindSlack, CorrelationID: "shared-slack-thread", EventID: "event-a", EventJSON: []byte(`{"team_id":"TEXAMPLE","channel_id":"CEXAMPLE","thread_id":"123","user_id":"UEXAMPLE","text":"shared request"}`)}
	a, err := core.EnqueueTriggerTask(t.Context(), task)
	require.NoError(t, err)
	retry, err := core.EnqueueTriggerTask(t.Context(), task)
	require.NoError(t, err)
	require.Equal(t, a.ThreadID, retry.ThreadID)
	row, err := assistantrepo.New(db).GetLatestAssistantThreadEventByThreadID(t.Context(), assistantrepo.GetLatestAssistantThreadEventByThreadIDParams{AssistantThreadID: a.ThreadID, ProjectID: project})
	require.NoError(t, err)
	captured, err := decodeExecution(row.NormalizedPayloadJson)
	require.NoError(t, err)
	require.Equal(t, "user-2", captured.HumanUserID)
	require.NotNil(t, captured.Slack)
	require.Equal(t, assistantidentity.ExecutionVersion, captured.Version)
	task.EventID = "event-b"
	task.EventJSON = []byte(`{"team_id":"TEXAMPLE","channel_id":"CEXAMPLE","thread_id":"123","user_id":"UUNMAPPED","text":"different caller"}`)
	b, err := core.EnqueueTriggerTask(t.Context(), task)
	require.NoError(t, err)
	require.Equal(t, a.ThreadID, b.ThreadID)
	qa := assistantrepo.New(db)
	ar, err := qa.LoadAssistantThreadForBootstrap(t.Context(), assistantrepo.LoadAssistantThreadForBootstrapParams{ThreadID: a.ThreadID, ProjectID: project})
	require.NoError(t, err)
	br, err := qa.LoadAssistantThreadForBootstrap(t.Context(), assistantrepo.LoadAssistantThreadForBootstrapParams{ThreadID: b.ThreadID, ProjectID: project})
	require.NoError(t, err)
	require.Equal(t, ar.ChatID, br.ChatID)
	row, err = qa.GetLatestAssistantThreadEventByThreadID(t.Context(), assistantrepo.GetLatestAssistantThreadEventByThreadIDParams{AssistantThreadID: b.ThreadID, ProjectID: project})
	require.NoError(t, err)
	fallback, err := decodeExecution(row.NormalizedPayloadJson)
	require.NoError(t, err)
	require.Equal(t, assistantidentity.ExecutionWorkload, fallback.Mode)
	require.Empty(t, fallback.HumanUserID)
	// Even with a mapping, bot provenance cannot delegate a human.
	bot, err := core.captureExecution(t.Context(), assistant, sourceKindSlack, b.ThreadID, uuid.NullUUID{UUID: root, Valid: true}, "bot", []byte(`{"team_id":"TEXAMPLE","user_id":"UEXAMPLE","bot_id":"BEXAMPLE"}`))
	require.NoError(t, err)
	envelope, err := decodeExecution(bot)
	require.NoError(t, err)
	require.Nil(t, envelope.Slack)
	require.Empty(t, envelope.HumanUserID)
	button, err := core.captureExecution(t.Context(), assistant, sourceKindSlack, b.ThreadID, uuid.NullUUID{UUID: root, Valid: true}, "button", []byte(`{"team_id":"TEXAMPLE","user_id":"UEXAMPLE","app_id":"AEXAMPLE","event_type":"block_actions"}`))
	require.NoError(t, err)
	clicked, err := decodeExecution(button)
	require.NoError(t, err)
	require.Equal(t, "user-2", clicked.HumanUserID, "application provenance does not erase a human button click")
	// Persisted retries keep the original mapping reference, not a new selection.
	encoded, err := json.Marshal(captured)
	require.NoError(t, err)
	var restored assistantidentity.Execution
	require.NoError(t, json.Unmarshal(encoded, &restored))
	require.Equal(t, captured.Slack, restored.Slack)
	require.Equal(t, captured.Ceiling.Digest, restored.Ceiling.Digest)
	require.NoError(t, restored.Check())
	manager := assistanttokens.New("test-secret", db, nil)
	manager.ConfigureExecutionIdentity(executionTestIssuer(t), testIdentityService)
	restored.ContinuationEventID = "resume-message"
	_, err = manager.GenerateExecution(t.Context(), restored)
	require.NoError(t, err, "resume preserves selected human provenance")
	require.NoError(t, slackrepo.New(db).RevokeSlackIdentityMapping(t.Context(), slackrepo.RevokeSlackIdentityMappingParams{OrganizationID: "org-test", SlackTeamID: "TEXAMPLE", SlackUserID: "UEXAMPLE"}))
	_, err = manager.GenerateExecution(t.Context(), restored)
	require.ErrorIs(t, err, assistantidentity.ErrActorIneligible, "resume cannot reselect workload after human mapping revocation")

}
