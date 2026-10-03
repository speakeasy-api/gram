package assistants

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/speakeasy-api/gram/server/internal/assistantidentity"
	identityrepo "github.com/speakeasy-api/gram/server/internal/assistantidentity/repo"
	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/auth/assistanttokens"
	bgtriggers "github.com/speakeasy-api/gram/server/internal/background/triggers"
	"github.com/speakeasy-api/gram/server/internal/cache"
	slackrepo "github.com/speakeasy-api/gram/server/internal/slackdirectoryconnections/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
	triggerrepo "github.com/speakeasy-api/gram/server/internal/triggers/repo"
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
	task.SlackExecution = slackSelectionForTest(t, db, "TEXAMPLE", "UEXAMPLE")
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
	task.SlackExecution = slackSelectionForTest(t, db, "TEXAMPLE", "UUNMAPPED")
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
	bot, err := core.captureExecution(t.Context(), assistant, sourceKindSlack, b.ThreadID, uuid.NullUUID{UUID: root, Valid: true}, "bot", []byte(`{"team_id":"TEXAMPLE","user_id":"UEXAMPLE","bot_id":"BEXAMPLE"}`), &bgtriggers.SlackExecutionSelection{FallbackReason: "slack_non_user_event"})
	require.NoError(t, err)
	envelope, err := decodeExecution(bot)
	require.NoError(t, err)
	require.Nil(t, envelope.Slack)
	require.Empty(t, envelope.HumanUserID)
	button, err := core.captureExecution(t.Context(), assistant, sourceKindSlack, b.ThreadID, uuid.NullUUID{UUID: root, Valid: true}, "button", []byte(`{"team_id":"TEXAMPLE","user_id":"UEXAMPLE","app_id":"AEXAMPLE","event_type":"block_actions"}`), slackSelectionForTest(t, db, "TEXAMPLE", "UEXAMPLE"))
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

// Use the real Slack normalizer and trusted ingress selection, not a duplicate
// capture implementation. Omit the routing cursor: these tests isolate identity.
func slackSelectionForTest(t *testing.T, db *pgxpool.Pool, team, user string) *bgtriggers.SlackExecutionSelection {
	t.Helper()
	definition, ok := bgtriggers.GetDefinition(bgtriggers.DefinitionSlugSlack)
	require.True(t, ok)
	body, err := json.Marshal(map[string]any{"type": "event_callback", "team_id": team, "event_id": "selection-test", "event": map[string]any{"type": "message", "user": user}})
	require.NoError(t, err)
	normalized, err := definition.HandleWebhook(body, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, normalized.Event)
	app := bgtriggers.NewApp(testenv.NewLogger(t), db, nil, slackSelectionEnvironment{}, bgtriggers.NewTriggerDeliveryLogger(nil), nil, nil, nil, nil, nil, cache.NoopCache)
	task, err := app.ProcessEvent(t.Context(), triggerrepo.TriggerInstance{ID: uuid.New(), OrganizationID: "org-test", ProjectID: uuid.New(), EnvironmentID: uuid.NullUUID{UUID: uuid.New(), Valid: true}, DefinitionSlug: bgtriggers.DefinitionSlugSlack, TargetKind: bgtriggers.TargetKindAssistant, TargetRef: uuid.NewString(), Status: bgtriggers.StatusActive, ConfigJson: []byte(`{"event_types":["message"]}`)}, *normalized.Event)
	require.NoError(t, err)
	require.NotNil(t, task)
	require.NotNil(t, task.SlackExecution)
	return task.SlackExecution
}

type slackSelectionEnvironment struct{}

func (slackSelectionEnvironment) Load(context.Context, uuid.UUID, toolconfig.SlugOrID) (map[string]string, error) {
	return map[string]string{"SLACK_SIGNING_SECRET": "test-secret"}, nil
}

func TestSlackExecutionSelectionCannotBeReplacedAtPersistence(t *testing.T) {
	t.Parallel()
	for _, change := range []string{"revoke", "reassign", "regrant"} {
		t.Run(change, func(t *testing.T) {
			t.Parallel()
			db, err := assistantsInfra.CloneTestDatabase(t, "slack_ingress_pins")
			require.NoError(t, err)
			project := newProvisioningProject(t, db, "slack-ingress-pins")
			core := newProvisioningCore(t, db)
			assistant, err := core.CreateAssistant(t.Context(), "org-test", project, "user-1", "Slack", "openai/gpt-4o-mini", "", nil, nil, 300, 1, StatusActive)
			require.NoError(t, err)
			root, err := core.resolveDashboardTriggerInstance(t.Context(), "org-test", project, assistant.ID, assistant.Name)
			require.NoError(t, err)
			require.NoError(t, identityrepo.New(db).FixtureSlackExecutionMapping(t.Context(), identityrepo.FixtureSlackExecutionMappingParams{UserID: "user-2", OrganizationID: "org-test", SlackTeamID: "TEXAMPLE", SlackUserID: "UEXAMPLE", Generation: uuid.New()}))
			selection := slackSelectionForTest(t, db, "TEXAMPLE", "UEXAMPLE")
			q := slackrepo.New(db)
			require.NoError(t, q.RevokeSlackIdentityMapping(t.Context(), slackrepo.RevokeSlackIdentityMappingParams{OrganizationID: "org-test", SlackTeamID: "TEXAMPLE", SlackUserID: "UEXAMPLE"}))
			denied := slackSelectionForTest(t, db, "TEXAMPLE", "UEXAMPLE")
			require.True(t, denied.Denied)
			if change != "revoke" {
				human := "user-2"
				if change == "reassign" {
					human = "user-1"
				}
				require.NoError(t, q.ConfirmSlackIdentityMapping(t.Context(), slackrepo.ConfirmSlackIdentityMappingParams{OrganizationID: "org-test", SlackTeamID: "TEXAMPLE", SlackUserID: "UEXAMPLE", UserID: human}))
				require.NoError(t, q.AdvanceSlackMappingRevision(t.Context(), slackrepo.AdvanceSlackMappingRevisionParams{OrganizationID: "org-test", ID: selection.Delegation.MembershipID}))
			}
			// Caller JSON cannot change an ingress choice or supply the typed handoff.
			payload := []byte(`{"team_id":"TEXAMPLE","channel_id":"CEXAMPLE","thread_id":"123","user_id":"forged","_gram_execution":{"human_user_id":"forged"},"SlackExecution":{"HumanUserID":"forged"}}`)
			task := bgtriggers.Task{TargetKind: bgtriggers.TargetKindAssistant, TargetRef: assistant.ID.String(), TriggerInstanceID: root.String(), DefinitionSlug: sourceKindSlack, CorrelationID: "ingress-thread", EventID: "captured-before-change", EventJSON: payload, SlackExecution: selection}
			result, err := core.EnqueueTriggerTask(t.Context(), task)
			require.NoError(t, err)
			row, err := assistantrepo.New(db).GetLatestAssistantThreadEventByThreadID(t.Context(), assistantrepo.GetLatestAssistantThreadEventByThreadIDParams{AssistantThreadID: result.ThreadID, ProjectID: project})
			require.NoError(t, err)
			execution, err := decodeExecution(row.NormalizedPayloadJson)
			require.NoError(t, err)
			require.Equal(t, "user-2", execution.HumanUserID)
			require.Equal(t, selection.Delegation.MappingID, execution.Slack.MappingID)
			require.Equal(t, selection.Delegation.MappingRevision, execution.Slack.MappingRevision)
			require.ErrorIs(t, assistantidentity.ValidateSlackDelegation(t.Context(), db, *execution), assistantidentity.ErrActorIneligible)
			// A duplicate never needs fresh authority, even if its new task is invalid.
			task.SlackExecution = nil
			task.EventJSON = []byte(`not-json`)
			retry, err := core.EnqueueTriggerTask(t.Context(), task)
			require.NoError(t, err)
			require.Equal(t, result.ThreadID, retry.ThreadID)
			_, err = core.captureExecution(t.Context(), assistant, sourceKindSlack, result.ThreadID, uuid.NullUUID{UUID: root, Valid: true}, "missing", payload, nil)
			require.ErrorIs(t, err, assistantidentity.ErrInvalidIdentity)
			_, err = core.captureExecution(t.Context(), assistant, sourceKindSlack, result.ThreadID, uuid.NullUUID{UUID: root, Valid: true}, "denied", payload, denied)
			require.ErrorIs(t, err, assistantidentity.ErrActorIneligible, "later remapping cannot replace an ingress denial")
		})
	}
}

func TestSlackExecutionLegacyBypassesIngressSelection(t *testing.T) {
	t.Parallel()
	db, err := assistantsInfra.CloneTestDatabase(t, "slack_ingress_legacy")
	require.NoError(t, err)
	project, assistantID, _, threadID := insertAssistantFixture(t, db)
	core := newProvisioningCore(t, db)
	root, err := core.resolveDashboardTriggerInstance(t.Context(), "org-test", project, assistantID, "Legacy")
	require.NoError(t, err)
	for _, trigger := range []uuid.NullUUID{{}, {UUID: root, Valid: true}} {
		for _, selection := range []*bgtriggers.SlackExecutionSelection{nil, {Denied: true}} {
			raw, err := core.captureExecution(t.Context(), assistantRecord{ID: assistantID, ProjectID: project, OrganizationID: "org-test"}, sourceKindSlack, threadID, trigger, "legacy", []byte(`{"_gram_execution":{"human_user_id":"forged"},"text":"legacy"}`), selection)
			require.NoError(t, err)
			require.JSONEq(t, `{"_gram_source_kind":"slack","text":"legacy"}`, string(raw))
		}
	}
}

func TestSlackExecutionIngressRejectsKnownIneligibleMapping(t *testing.T) {
	t.Parallel()
	for _, change := range []string{"revoke", "inactive", "bot", "conflict"} {
		t.Run(change, func(t *testing.T) {
			t.Parallel()
			db, err := assistantsInfra.CloneTestDatabase(t, "slack_ingress_ineligible")
			require.NoError(t, err)
			_ = newProvisioningProject(t, db, "slack-ingress-ineligible")
			q := identityrepo.New(db)
			require.NoError(t, q.FixtureSlackExecutionMapping(t.Context(), identityrepo.FixtureSlackExecutionMappingParams{UserID: "user-2", OrganizationID: "org-test", SlackTeamID: "TEXAMPLE", SlackUserID: "UEXAMPLE", Generation: uuid.New()}))
			p := identityrepo.FixtureInvalidateSlackExecutionMembershipParams{OrganizationID: "org-test", SlackTeamID: "TEXAMPLE", SlackUserID: "UEXAMPLE", Status: "active", MemberType: "person", MappingRevision: 1}
			switch change {
			case "revoke":
				require.NoError(t, slackrepo.New(db).RevokeSlackIdentityMapping(t.Context(), slackrepo.RevokeSlackIdentityMappingParams{OrganizationID: "org-test", SlackTeamID: "TEXAMPLE", SlackUserID: "UEXAMPLE"}))
			case "inactive":
				p.Status = "deactivated"
			case "bot":
				p.MemberType = "bot"
			case "conflict":
				p.ConflictReason = pgtype.Text{String: "changed", Valid: true}
			}
			require.NoError(t, q.FixtureInvalidateSlackExecutionMembership(t.Context(), p))
			selection := slackSelectionForTest(t, db, "TEXAMPLE", "UEXAMPLE")
			require.True(t, selection.Denied, "known ineligible history must not become workload fallback")
			require.Empty(t, selection.HumanUserID)
			require.Empty(t, selection.FallbackReason)
			require.Nil(t, selection.Delegation)
		})
	}
}
