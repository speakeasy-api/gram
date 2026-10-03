package assistants

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/assistantidentity"
	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/auth/assistanttokens"
	bgtriggers "github.com/speakeasy-api/gram/server/internal/background/triggers"
	"github.com/speakeasy-api/gram/server/internal/mcpauthz"
	slackrepo "github.com/speakeasy-api/gram/server/internal/slackdirectoryconnections/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
)

func TestExecutionActorSelectionNeverFabricatesAutonomousHuman(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, source, payload, mapped, wantHuman, wantFallback string
		lookupError, errorExpected                             bool
	}{
		{name: "dashboard", source: sourceKindDashboard, payload: `{"user_id":"human-a"}`, wantHuman: "human-a"},
		{name: "dashboard missing sender", source: sourceKindDashboard, payload: `{}`, errorExpected: true},
		{name: "cron", source: sourceKindCron, payload: `{"user_id":"ignored"}`},
		{name: "ordinary autonomous", source: sourceKindGithub, payload: `{}`},
		{name: "slack mapped", source: sourceKindSlack, payload: `{"team_id":"workspace","user_id":"sender"}`, mapped: "mapped-human", wantHuman: "mapped-human"},
		{name: "slack absent", source: sourceKindSlack, payload: `{"team_id":"workspace","user_id":"sender"}`, wantFallback: "slack_mapping_unavailable"},
		{name: "slack unavailable", source: sourceKindSlack, payload: `{"team_id":"workspace","user_id":"sender"}`, lookupError: true, wantFallback: "slack_mapping_unavailable"},
		{name: "wake captured requester", source: sourceKindWake, payload: `{"identity_version":1,"requester_user_id":"requester"}`, wantHuman: "requester"},
		{name: "wake captured owner", source: sourceKindWake, payload: `{"identity_version":1,"requester_user_id":"owner"}`, wantHuman: "owner"},
		{name: "wake missing capture", source: sourceKindWake, payload: `{"identity_version":1}`, errorExpected: true},
		{name: "wake legacy ignores requester", source: sourceKindWake, payload: `{"requester_user_id":"untrusted"}`, wantHuman: "owner"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			mode, human, fallback, err := selectExecutionActor(t.Context(), assistantRecord{OrganizationID: "org-test", CreatedByUserID: "owner"}, tc.source, assistantThreadEventRecord{NormalizedPayloadJSON: []byte(tc.payload)}, func(_ context.Context, p slackrepo.ResolveSlackMappingUserParams) (string, error) {
				require.Equal(t, "org-test", p.OrganizationID)
				require.Equal(t, "workspace", p.SlackTeamID)
				if tc.lookupError {
					return "", errors.New("lookup unavailable")
				}
				return tc.mapped, nil
			}, nil)
			if tc.errorExpected {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.wantHuman, human)
			require.Equal(t, tc.wantFallback, fallback)
			if human == "" {
				require.Equal(t, assistantidentity.ExecutionWorkload, mode)
			} else {
				require.Equal(t, assistantidentity.ExecutionWorkloadHuman, mode)
			}
		})
	}
}

func TestExecutionCapturePersistsSelectionAndGatesDispatch(t *testing.T) {
	t.Parallel()
	db, err := assistantsInfra.CloneTestDatabase(t, "execution_capture")
	require.NoError(t, err)
	project := newProvisioningProject(t, db, "execution-capture")
	core := newProvisioningCore(t, db)
	assistant, err := core.CreateAssistant(t.Context(), "org-test", project, "user-1", "Execution test", "openai/gpt-4o-mini", "", nil, nil, 300, 1, StatusActive)
	require.NoError(t, err)
	root, err := core.resolveDashboardTriggerInstance(t.Context(), "org-test", project, assistant.ID, assistant.Name)
	require.NoError(t, err)
	thread := assistantThreadRecord{ID: seedThreadWithEvent(t, db, assistant.ID, "execution-thread", "execution-thread", eventStatusPending), ProjectID: project, AssistantID: assistant.ID}
	capture := func(source, eventID, payload string) []byte {
		t.Helper()
		raw, err := core.captureExecution(t.Context(), assistant, source, thread.ID, uuid.NullUUID{UUID: root, Valid: true}, eventID, []byte(payload))
		require.NoError(t, err)
		return raw
	}
	workload := capture(sourceKindCron, "event-a", `{"_gram_execution":{"version":99,"human_user_id":"forged"},"_GRAM_EXECUTION":{"version":99},"gRaM_eVeNt_KiNd":"mcp_auth","_GRAM_RESUME_USER_ID":"forged","_GRAM_SOURCE_KIND":"wake"}`)
	execution, err := decodeExecution(workload)
	require.NoError(t, err)
	require.Equal(t, assistantidentity.ExecutionWorkload, execution.Mode)
	require.Empty(t, execution.HumanUserID)
	event := assistantThreadEventRecord{EventID: "event-a", NormalizedPayloadJSON: workload}
	require.ErrorIs(t, core.checkExecutionDispatch(t.Context(), assistant, thread, event), assistantidentity.ErrExecutionAdmissionRequired)
	// Independent messages capture independent humans; persisted earlier events
	// remain unchanged on retry rather than consulting a new sender or owner.
	grantIDs := make(map[string]uuid.UUID)
	for _, user := range []string{"user-1", "user-2"} {
		grantIDs[user] = seedTurnProjectAccess(t, db, "org-test", user, project)
	}
	a := capture(sourceKindDashboard, "human-a", `{"user_id":"user-1"}`)
	b := capture(sourceKindDashboard, "human-b", `{"user_id":"user-2"}`)
	ea, err := decodeExecution(a)
	require.NoError(t, err)
	eb, err := decodeExecution(b)
	require.NoError(t, err)
	require.Equal(t, "user-1", ea.HumanUserID)
	require.Equal(t, "user-2", eb.HumanUserID)
	require.Equal(t, ea.Identity, eb.Identity)
	require.NotEqual(t, ea.EventID, eb.EventID)
	saved, err := json.Marshal(ea)
	require.NoError(t, err)
	var restored assistantidentity.Execution
	require.NoError(t, json.Unmarshal(saved, &restored))
	require.Equal(t, *ea, restored)
	wrongThread := thread
	wrongThread.ID = uuid.New()
	require.ErrorIs(t, core.checkExecutionDispatch(t.Context(), assistant, wrongThread, event), assistantidentity.ErrInvalidIdentity)
	otherTenant := assistant
	otherTenant.OrganizationID = "org-other"
	require.ErrorIs(t, core.checkExecutionDispatch(t.Context(), otherTenant, thread, event), assistantidentity.ErrInvalidIdentity)
	// Corrupt metadata cannot downgrade to legacy, and actual binding deletion
	// invalidates the queued envelope before the model-admission gate is reached.
	require.Error(t, core.checkExecutionDispatch(t.Context(), assistant, thread, assistantThreadEventRecord{NormalizedPayloadJSON: []byte(`{"_gram_execution":null}`)}))
	require.NoError(t, core.checkExecutionDispatch(t.Context(), assistant, thread, assistantThreadEventRecord{NormalizedPayloadJSON: []byte(`{}`)}))

	// Mint/validate uses the existing stable signing infrastructure and fresh live
	// authority checks. Cryptographic identity is never business authorization.
	manager := assistanttokens.New("legacy-test-secret", db, nil)
	manager.ConfigureExecutionIdentity(executionTestIssuer(t), testIdentityService)
	core.assistantTokens = manager
	token, err := manager.GenerateExecution(t.Context(), *execution)
	require.NoError(t, err)
	target := assistanttokens.ExecutionTarget{EventID: execution.InvocationEventID(), OrganizationID: "org-test", ProjectID: project, AssistantID: assistant.ID, ThreadID: thread.ID}
	admitted, err := manager.ValidateExecution(t.Context(), token, target)
	require.NoError(t, err)
	require.Equal(t, *execution, *admitted)
	require.ErrorIs(t, manager.AuthorizeExecution(t.Context(), token, target), assistantidentity.ErrExecutionAdmissionRequired)
	_, err = manager.Validate(token)
	require.Error(t, err, "execution token must not enter legacy UserID authorization")
	wrongTarget := target
	wrongTarget.EventID = "different-event"
	_, err = manager.ValidateExecution(t.Context(), token, wrongTarget)
	require.Error(t, err, "same-thread invocation mismatch")
	wrongTarget = target
	wrongTarget.OrganizationID = "org-other"
	_, err = manager.ValidateExecution(t.Context(), token, wrongTarget)
	require.Error(t, err)
	stale := *execution
	stale.Identity.TriggerGeneration++
	_, err = manager.GenerateExecution(t.Context(), stale)
	require.Error(t, err)

	humanToken, err := manager.GenerateExecution(t.Context(), *eb)
	require.NoError(t, err)
	_, err = accessrepo.New(db).DeletePrincipalGrant(t.Context(), accessrepo.DeletePrincipalGrantParams{ID: grantIDs["user-2"], OrganizationID: "org-test"})
	require.NoError(t, err)
	_, err = manager.ValidateExecution(t.Context(), humanToken, assistanttokens.ExecutionTarget{OrganizationID: target.OrganizationID, ProjectID: target.ProjectID, AssistantID: target.AssistantID, ThreadID: target.ThreadID, EventID: eb.InvocationEventID()})
	require.ErrorIs(t, err, assistantidentity.ErrActorIneligible)
	_, err = manager.GenerateExecution(t.Context(), *eb)
	require.ErrorIs(t, err, assistantidentity.ErrActorIneligible)
	_, err = manager.GenerateExecution(t.Context(), *execution)
	require.NoError(t, err, "autonomous identity does not impersonate denied human")
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = core.captureExecution(cancelled, assistant, sourceKindCron, thread.ID, uuid.NullUUID{UUID: root, Valid: true}, "cancelled", []byte(`{}`))
	require.Error(t, err, "binding lookup failure must not become legacy")
	origin, err := assistantrepo.New(db).InsertAssistantThreadEvent(t.Context(), assistantrepo.InsertAssistantThreadEventParams{AssistantThreadID: thread.ID, AssistantID: assistant.ID, ProjectID: project, EventID: execution.InvocationEventID(), CorrelationID: "execution-thread", Status: eventStatusCompleted, NormalizedPayloadJson: workload, SourcePayloadJson: []byte(`{}`)})
	require.NoError(t, err)
	state, err := manager.GenerateExecutionMCPAuthFlow(t.Context(), assistanttokens.MCPAuthFlowInput{OriginatingEventID: origin, Execution: execution, OrgID: "org-test", ProjectID: project, AssistantID: assistant.ID, ThreadID: thread.ID, UserID: "user-1", FlowID: "flow-test", AttemptID: "attempt-test"})
	require.NoError(t, err)
	claims, err := manager.ValidateMCPAuthFlow(state)
	require.NoError(t, err)
	require.Equal(t, *execution, *claims.Execution)
	require.Equal(t, origin.String(), claims.OriginatingEventID)
	require.NoError(t, manager.ValidateExecutionMCPAuthFlow(t.Context(), claims))
	require.ErrorIs(t, manager.ValidateExecutionMCPAuthFlow(cancelled, claims), context.Canceled, "origin lookup outages remain infrastructure errors")
	// A later invocation does not reattribute a still-valid older OAuth flow.
	laterPayload := capture(sourceKindCron, "later-event", `{}`)
	later, err := decodeExecution(laterPayload)
	require.NoError(t, err)
	laterID, err := assistantrepo.New(db).InsertAssistantThreadEvent(t.Context(), assistantrepo.InsertAssistantThreadEventParams{AssistantThreadID: thread.ID, AssistantID: assistant.ID, ProjectID: project, EventID: later.InvocationEventID(), CorrelationID: "execution-thread", Status: eventStatusCompleted, NormalizedPayloadJson: laterPayload, SourcePayloadJson: []byte(`{}`)})
	require.NoError(t, err)
	require.NoError(t, manager.ValidateExecutionMCPAuthFlow(t.Context(), claims), "do not bind an older flow to mutable latest event")
	for _, mutate := range []func(*assistanttokens.MCPAuthFlowClaims){
		func(c *assistanttokens.MCPAuthFlowClaims) { c.Execution = later },
		func(c *assistanttokens.MCPAuthFlowClaims) { c.OriginatingEventID = laterID.String() },
		func(c *assistanttokens.MCPAuthFlowClaims) { c.OriginatingEventID = "" },
		func(c *assistanttokens.MCPAuthFlowClaims) { c.ThreadID = uuid.NewString() },
		func(c *assistanttokens.MCPAuthFlowClaims) { c.AssistantID = uuid.NewString() },
		func(c *assistanttokens.MCPAuthFlowClaims) { c.ProjectID = uuid.NewString() },
		func(c *assistanttokens.MCPAuthFlowClaims) { c.OrgID = "other-org" },
	} {
		mismatched := *claims
		mutate(&mismatched)
		require.ErrorIs(t, manager.ValidateExecutionMCPAuthFlow(t.Context(), &mismatched), assistantidentity.ErrInvalidIdentity)
	}
	_, err = manager.GenerateExecutionMCPAuthFlow(t.Context(), assistanttokens.MCPAuthFlowInput{OriginatingEventID: origin, Execution: later, OrgID: "org-test", ProjectID: project, AssistantID: assistant.ID, ThreadID: thread.ID})
	require.ErrorIs(t, err, assistantidentity.ErrInvalidIdentity, "cannot sign state for a different persisted invocation")
	_, err = manager.GenerateMCPAuthFlow(assistanttokens.MCPAuthFlowInput{OriginatingEventID: origin, Execution: execution})
	require.ErrorIs(t, err, assistantidentity.ErrInvalidIdentity, "external legacy flow cannot carry execution linkage")
	svc := &Service{core: core, logger: core.logger}
	created, err := svc.enqueueMCPAuthEvent(t.Context(), project, assistant.ID, thread.ID, "attempt-test", mcpAuthEventPayload{GramEventKind: mcpAuthEventKind, ActorUserID: claims.UserID, Execution: claims.Execution})
	require.NoError(t, err)
	require.True(t, created)
	require.NoError(t, manager.ValidateExecutionMCPAuthFlow(t.Context(), claims), "callback retry still pins original event")
	created, err = svc.enqueueMCPAuthEvent(t.Context(), project, assistant.ID, thread.ID, "attempt-test", mcpAuthEventPayload{GramEventKind: mcpAuthEventKind, ActorUserID: claims.UserID, Execution: claims.Execution})
	require.NoError(t, err)
	require.False(t, created, "signed attempt must not enqueue twice")
	row, err := assistantrepo.New(db).GetLatestAssistantThreadEventByThreadID(t.Context(), assistantrepo.GetLatestAssistantThreadEventByThreadIDParams{AssistantThreadID: thread.ID, ProjectID: project})
	require.NoError(t, err)
	continued, err := decodeExecution(row.NormalizedPayloadJson)
	require.NoError(t, err)
	require.Equal(t, execution.EventID, continued.EventID)
	require.Empty(t, continued.HumanUserID, "consent owner cannot become execution human")
	require.Equal(t, mcpAuthEventKind+":attempt-test", continued.ContinuationEventID)
	require.Equal(t, *execution, *claims.Execution, "continuation does not mutate source envelope")
	require.ErrorIs(t, core.checkExecutionDispatch(t.Context(), assistant, thread, assistantThreadEventRecord{EventID: row.EventID, NormalizedPayloadJSON: row.NormalizedPayloadJson}), assistantidentity.ErrExecutionAdmissionRequired)

	paused := StatusPaused
	_, err = core.UpdateAssistant(t.Context(), project, assistant.ID, nil, nil, nil, nil, nil, nil, nil, &paused)
	require.NoError(t, err)
	retry, err := core.EnqueueTriggerTask(t.Context(), bgtriggers.Task{TargetKind: bgtriggers.TargetKindAssistant, TargetRef: assistant.ID.String(), EventID: row.EventID, EventJSON: []byte(`not-json`)})
	require.NoError(t, err)
	require.False(t, retry.ShouldSignal, "paused assistants cannot signal persisted retries")
	active := StatusActive
	_, err = core.UpdateAssistant(t.Context(), project, assistant.ID, nil, nil, nil, nil, nil, nil, nil, &active)
	require.NoError(t, err)
	retry, err = core.EnqueueTriggerTask(t.Context(), bgtriggers.Task{TargetKind: bgtriggers.TargetKindAssistant, TargetRef: assistant.ID.String(), EventID: row.EventID, EventJSON: []byte(`not-json`)})
	require.NoError(t, err)
	require.True(t, retry.ShouldSignal)
	require.Equal(t, thread.ID, retry.ThreadID, "active retry must signal original stored envelope despite changed authority")
	require.ErrorIs(t, core.checkExecutionDispatch(t.Context(), assistant, thread, event), assistantidentity.ErrExecutionAdmissionRequired, "temporary pause must not invalidate identity incarnation")
	require.NoError(t, core.DeleteAssistant(t.Context(), project, assistant.ID, urn.NewPrincipal(urn.PrincipalTypeUser, "user-1"), nil))
	require.ErrorIs(t, manager.ValidateExecutionMCPAuthFlow(t.Context(), claims), assistantidentity.ErrInvalidIdentity, "retired authority cannot resume old flow")
	require.ErrorIs(t, core.checkExecutionDispatch(t.Context(), assistant, thread, event), assistantidentity.ErrInvalidIdentity)
	_, err = manager.ValidateExecution(t.Context(), token, target)
	require.Error(t, err, "revocation must bypass legacy positive cache")
}

func executionTestIssuer(t *testing.T) *mcpauthz.Issuer {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	private, err := x509.MarshalPKCS8PrivateKey(key)
	require.NoError(t, err)
	public, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	require.NoError(t, err)
	issuer, err := mcpauthz.New(string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: private})), string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: public})), "https://platform.example.invalid", false)
	require.NoError(t, err)
	return issuer
}

func TestInvocationBusyPreservesDurableQueuedEventBudget(t *testing.T) {
	t.Parallel()
	require.ErrorIs(t, classifyTurnError(&runtimeResponseError{StatusCode: http.StatusTooManyRequests, Body: ErrRuntimeInvocationBusy.Error()}), ErrRuntimeInvocationBusy)
	require.ErrorIs(t, classifyTurnError(&runtimeResponseError{StatusCode: http.StatusTooManyRequests, Body: "different-error"}), ErrRuntimeUnhealthy)
	db, err := assistantsInfra.CloneTestDatabase(t, "execution_busy")
	require.NoError(t, err)
	project, assistant, _, _ := insertAssistantFixture(t, db)
	thread := seedThreadWithEvent(t, db, assistant, "busy-thread", "busy-thread", eventStatusPending)
	original, err := assistantrepo.New(db).GetLatestAssistantThreadEventByThreadID(t.Context(), assistantrepo.GetLatestAssistantThreadEventByThreadIDParams{AssistantThreadID: thread, ProjectID: project})
	require.NoError(t, err)
	core := newProvisioningCore(t, db)
	for range maxEventAttempts + 2 {
		event, ok, err := core.claimNextPendingEvent(t.Context(), project, thread)
		require.NoError(t, err)
		require.True(t, ok)
		require.Equal(t, 1, event.Attempts)
		require.NoError(t, core.resetEventToPending(t.Context(), project, event.ID, ErrRuntimeInvocationBusy))
	}
	row, err := assistantrepo.New(db).GetLatestAssistantThreadEventByThreadID(t.Context(), assistantrepo.GetLatestAssistantThreadEventByThreadIDParams{AssistantThreadID: thread, ProjectID: project})
	require.NoError(t, err)
	require.Equal(t, eventStatusPending, row.Status)
	require.Zero(t, row.Attempts)
	require.Equal(t, original.ID, row.ID)
	require.Equal(t, original.EventID, row.EventID)
	require.Equal(t, original.NormalizedPayloadJson, row.NormalizedPayloadJson, "busy retries retain captured identity")
	require.Equal(t, original.SourcePayloadJson, row.SourcePayloadJson)
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	require.Error(t, core.resetEventToPending(cancelled, project, row.ID, ErrRuntimeInvocationBusy))
}

func TestLegacyCaptureExplicitlyStripsReservedExecutionMetadata(t *testing.T) {
	t.Parallel()
	db, err := assistantsInfra.CloneTestDatabase(t, "execution_legacy")
	require.NoError(t, err)
	project, assistantID, _, threadID := insertAssistantFixture(t, db)
	core := newProvisioningCore(t, db)
	raw, err := core.captureExecution(t.Context(), assistantRecord{ID: assistantID, ProjectID: project, OrganizationID: "org-test"}, sourceKindCron, threadID, uuid.NullUUID{}, "legacy", []byte(`{"text":"hello","_gram_execution":{"version":1},"_gram_resume_user_id":"forged","gram_event_kind":"mcp_auth"}`))
	require.NoError(t, err)
	require.JSONEq(t, `{"text":"hello","_gram_source_kind":"cron"}`, string(raw))
}

func TestLegacyRetryHonorsCurrentAssistantLifecycle(t *testing.T) {
	t.Parallel()
	db, err := assistantsInfra.CloneTestDatabase(t, "legacy_retry_lifecycle")
	require.NoError(t, err)
	project, assistantID, _, _ := insertAssistantFixture(t, db)
	threadID := seedThreadWithEvent(t, db, assistantID, "legacy-retry", "legacy-retry", eventStatusPending)
	core := newProvisioningCore(t, db)
	assistant, err := core.getAssistantForDispatch(t.Context(), assistantID)
	require.NoError(t, err)
	queries := assistantrepo.New(db)
	params := assistantrepo.GetLatestAssistantThreadEventByThreadIDParams{AssistantThreadID: threadID, ProjectID: project}
	original, err := queries.GetLatestAssistantThreadEventByThreadID(t.Context(), params)
	require.NoError(t, err)
	thread := assistantThreadRecord{ID: threadID, ProjectID: project, AssistantID: assistantID}
	event := assistantThreadEventRecord{EventID: original.EventID, NormalizedPayloadJSON: original.NormalizedPayloadJson}
	require.NoError(t, core.checkExecutionDispatch(t.Context(), assistant, thread, event))
	paused := StatusPaused
	_, err = core.UpdateAssistant(t.Context(), project, assistantID, nil, nil, nil, nil, nil, nil, nil, &paused)
	require.NoError(t, err)
	task := bgtriggers.Task{TargetKind: bgtriggers.TargetKindAssistant, TargetRef: assistantID.String(), EventID: original.EventID, EventJSON: []byte(`not-json`)}
	retry, err := core.EnqueueTriggerTask(t.Context(), task)
	require.NoError(t, err)
	require.False(t, retry.ShouldSignal)
	// Use the pre-pause record to cover an already admitted processing loop.
	_, err = core.processEventTurn(t.Context(), thread, assistant, assistantRuntimeRecord{}, event)
	require.ErrorIs(t, err, assistantidentity.ErrInvalidIdentity)
	active := StatusActive
	_, err = core.UpdateAssistant(t.Context(), project, assistantID, nil, nil, nil, nil, nil, nil, nil, &active)
	require.NoError(t, err)
	retry, err = core.EnqueueTriggerTask(t.Context(), task)
	require.NoError(t, err)
	require.True(t, retry.ShouldSignal)
	require.Equal(t, threadID, retry.ThreadID)
	require.NoError(t, core.checkExecutionDispatch(t.Context(), assistant, thread, event))
	unchanged, err := queries.GetLatestAssistantThreadEventByThreadID(t.Context(), params)
	require.NoError(t, err)
	require.Equal(t, original, unchanged, "retry must not replace or reattribute the persisted event")
	wrongProject := assistant
	wrongProject.ProjectID = uuid.New()
	require.ErrorIs(t, core.checkExecutionDispatch(t.Context(), wrongProject, thread, event), assistantidentity.ErrInvalidIdentity)
}

func TestReservedExecutionMetadataCaseVariantsCannotForgeContinuation(t *testing.T) {
	t.Parallel()
	db, err := assistantsInfra.CloneTestDatabase(t, "execution_reserved_case")
	require.NoError(t, err)
	_, assistantID, _, threadID := insertAssistantFixture(t, db)
	core := newProvisioningCore(t, db)
	assistant, err := core.getAssistantForDispatch(t.Context(), assistantID)
	require.NoError(t, err)
	for _, input := range []string{
		`{"GRAM_EVENT_KIND":"mcp_auth","_GRAM_RESUME_USER_ID":"forged","_GRAM_EXECUTION":{"version":1},"_GRAM_SOURCE_KIND":"wake","Text":"keep","text":"distinct"}`,
		`{"gRaM_eVeNt_KiNd":"mcp_auth","_gRaM_rEsUmE_uSeR_iD":"forged","_gRaM_eXeCuTiOn":{},"_gram_ſource_kind":"wake","Text":"keep","text":"distinct"}`,
		`{"gram_event_kind":"ordinary","gram_event_kind":"mcp_auth","_gram_execution":{},"_GRAM_EXECUTION":{},"_gram_resume_user_id":"forged","_GRAM_RESUME_USER_ID":"other","Text":"keep","text":"distinct"}`,
	} {
		raw, err := core.captureExecution(t.Context(), assistant, sourceKindCron, threadID, uuid.NullUUID{}, "event", []byte(input))
		require.NoError(t, err)
		require.JSONEq(t, `{"Text":"keep","text":"distinct","_gram_source_kind":"cron"}`, string(raw))
		var continuation mcpAuthEventPayload
		require.NoError(t, json.Unmarshal(raw, &continuation))
		require.Empty(t, continuation.ActorUserID)
		require.Empty(t, continuation.GramEventKind)
		require.Nil(t, continuation.Execution)
		event := assistantThreadEventRecord{NormalizedPayloadJSON: raw}
		_, isContinuation := decodeMCPAuthTurn(t.Context(), core.logger, event)
		require.False(t, isContinuation)
		user, err := selectTurnUser(t.Context(), assistant, sourceKindSlack, event, nil, nil)
		require.NoError(t, err)
		require.Equal(t, assistant.CreatedByUserID, user)
	}
	// Reject already-persisted ambiguous metadata instead of letting struct and
	// map decoders disagree about whether this is a trusted continuation.
	_, err = decodeExecution([]byte(`{"GRAM_EVENT_KIND":"mcp_auth","_GRAM_RESUME_USER_ID":"forged"}`))
	require.ErrorIs(t, err, assistantidentity.ErrInvalidIdentity)
}

func TestExecutionDispatchErrorClassification(t *testing.T) {
	t.Parallel()
	for _, err := range []error{assistantidentity.ErrInvalidIdentity, assistantidentity.ErrActorIneligible, assistantidentity.ErrExecutionAdmissionRequired} {
		require.ErrorIs(t, classifyExecutionDispatchError(fmt.Errorf("wrapped: %w", err)), errExecutionDenied)
	}
	for _, err := range []error{context.Canceled, context.DeadlineExceeded, errors.New("database unavailable")} {
		classified := classifyExecutionDispatchError(err)
		require.ErrorIs(t, classified, err)
		require.NotErrorIs(t, classified, errExecutionDenied)
	}
}

func TestExecutionDenialRecordedOnceWithoutAdmissionRetry(t *testing.T) {
	t.Parallel()
	db, err := assistantsInfra.CloneTestDatabase(t, "execution_terminal")
	require.NoError(t, err)
	project, assistantID, _, threadID := insertAssistantFixture(t, db)
	core := newProvisioningCore(t, db)
	var ensures atomic.Int64
	core.runtime = testRuntimeBackend{backend: runtimeBackendFlyIO, ensureCalls: &ensures}
	// Invalid persisted envelope exercises the same permanent-denial boundary
	// as the explicitly closed AIM-411 admission gate, before any runner call.
	queries := assistantrepo.New(db)
	original, err := queries.GetLatestAssistantThreadEventByThreadID(t.Context(), assistantrepo.GetLatestAssistantThreadEventByThreadIDParams{AssistantThreadID: threadID, ProjectID: project})
	require.NoError(t, err)
	require.NoError(t, queries.SetAssistantThreadEventStatus(t.Context(), assistantrepo.SetAssistantThreadEventStatusParams{ID: original.ID, ProjectID: project, Status: eventStatusCompleted, UpdatedAt: original.UpdatedAt}))
	_, err = queries.InsertAssistantThreadEvent(t.Context(), assistantrepo.InsertAssistantThreadEventParams{AssistantThreadID: threadID, AssistantID: assistantID, ProjectID: project, EventID: "denied-event", CorrelationID: "denied-event", Status: eventStatusPending, NormalizedPayloadJson: []byte(`{"_gram_execution":{"version":99}}`), SourcePayloadJson: []byte(`{}`)})
	require.NoError(t, err)
	_, err = core.AdmitPendingThreads(t.Context(), assistantID)
	require.NoError(t, err)
	for range 1 {
		result, err := core.ProcessThreadEvents(t.Context(), project, threadID)
		require.NoError(t, err)
		require.True(t, result.RetryAdmission, "release cold reservation and wake pending siblings")
		row, err := assistantrepo.New(db).GetLatestAssistantThreadEventByThreadID(t.Context(), assistantrepo.GetLatestAssistantThreadEventByThreadIDParams{AssistantThreadID: threadID, ProjectID: project})
		require.NoError(t, err)
		require.Equal(t, eventStatusFailed, row.Status)
		require.Zero(t, row.Attempts)
		require.Contains(t, row.LastError.String, errExecutionDenied.Error())
	}
	require.Zero(t, ensures.Load(), "cold denial must not provision a runtime")
	_, claimable, err := core.claimNextPendingEvent(t.Context(), project, threadID)
	require.NoError(t, err)
	require.False(t, claimable, "terminal event must not be claimable again")
}

func TestExecutionAdmissionGateIsTerminal(t *testing.T) {
	t.Parallel()
	db, err := assistantsInfra.CloneTestDatabase(t, "execution_gate_terminal")
	require.NoError(t, err)
	project := newProvisioningProject(t, db, "execution-gate-terminal")
	core := newProvisioningCore(t, db)
	var ensures atomic.Int64
	core.runtime = testRuntimeBackend{backend: runtimeBackendFlyIO, ensureCalls: &ensures}
	assistant, err := core.CreateAssistant(t.Context(), "org-test", project, "user-1", "Execution gate", "openai/gpt-4o-mini", "", nil, nil, 300, 1, StatusActive)
	require.NoError(t, err)
	root, err := core.resolveDashboardTriggerInstance(t.Context(), "org-test", project, assistant.ID, assistant.Name)
	require.NoError(t, err)
	threadID := seedThreadWithEvent(t, db, assistant.ID, "gate-thread", "gate-thread", eventStatusPending)
	params := assistantrepo.GetLatestAssistantThreadEventByThreadIDParams{AssistantThreadID: threadID, ProjectID: project}
	row, err := assistantrepo.New(db).GetLatestAssistantThreadEventByThreadID(t.Context(), params)
	require.NoError(t, err)
	raw, err := core.captureExecution(t.Context(), assistant, sourceKindCron, threadID, uuid.NullUUID{UUID: root, Valid: true}, "denied-event", []byte(`{}`))
	require.NoError(t, err)
	require.NoError(t, assistantrepo.New(db).SetAssistantThreadEventStatus(t.Context(), assistantrepo.SetAssistantThreadEventStatusParams{ID: row.ID, ProjectID: project, Status: eventStatusCompleted, UpdatedAt: row.UpdatedAt}))
	_, err = assistantrepo.New(db).InsertAssistantThreadEvent(t.Context(), assistantrepo.InsertAssistantThreadEventParams{AssistantThreadID: threadID, AssistantID: assistant.ID, ProjectID: project, EventID: "denied-event", CorrelationID: "denied-event", Status: eventStatusPending, NormalizedPayloadJson: raw, SourcePayloadJson: []byte(`{}`)})
	require.NoError(t, err)
	_, err = core.AdmitPendingThreads(t.Context(), assistant.ID)
	require.NoError(t, err)
	sibling := seedThreadWithEvent(t, db, assistant.ID, "gate-sibling", "gate-sibling", eventStatusPending)
	for range 1 {
		result, err := core.ProcessThreadEvents(t.Context(), project, threadID)
		require.NoError(t, err)
		require.True(t, result.RetryAdmission, "release cold reservation and wake pending siblings")
		row, err = assistantrepo.New(db).GetLatestAssistantThreadEventByThreadID(t.Context(), params)
		require.NoError(t, err)
		require.Equal(t, eventStatusFailed, row.Status)
		require.Zero(t, row.Attempts)
		require.Contains(t, row.LastError.String, assistantidentity.ErrExecutionAdmissionRequired.Error())
	}
	require.Zero(t, ensures.Load(), "cold denial must not provision a runtime")
	_, claimable, err := core.claimNextPendingEvent(t.Context(), project, threadID)
	require.NoError(t, err)
	require.False(t, claimable, "terminal event must not be claimable again")
	admitted, err := core.AdmitPendingThreads(t.Context(), assistant.ID)
	require.NoError(t, err)
	require.Contains(t, admitted.ThreadIDs, sibling, "coordinator kick must admit sibling after denied starter releases reservation")

}

func TestColdAdmissionSkipsDeniedEventAndContinuesEligibleEvent(t *testing.T) {
	t.Parallel()
	db, err := assistantsInfra.CloneTestDatabase(t, "cold_mixed_execution")
	require.NoError(t, err)
	project, assistant, _, thread := insertAssistantFixture(t, db)
	core := newProvisioningCore(t, db)
	var ensures atomic.Int64
	core.runtime = testRuntimeBackend{backend: runtimeBackendFlyIO, ensureCalls: &ensures, ensureErr: errors.New("transient provisioning failure")}
	q := assistantrepo.New(db)
	params := assistantrepo.GetLatestAssistantThreadEventByThreadIDParams{ProjectID: project, AssistantThreadID: thread}
	original, err := q.GetLatestAssistantThreadEventByThreadID(t.Context(), params)
	require.NoError(t, err)
	require.NoError(t, q.SetAssistantThreadEventStatus(t.Context(), assistantrepo.SetAssistantThreadEventStatusParams{ID: original.ID, ProjectID: project, Status: eventStatusCompleted, UpdatedAt: original.UpdatedAt}))
	for _, event := range []struct{ id, payload string }{{"denied", `{"_gram_execution":{"version":99}}`}, {"eligible", `{}`}} {
		_, err = q.InsertAssistantThreadEvent(t.Context(), assistantrepo.InsertAssistantThreadEventParams{AssistantThreadID: thread, AssistantID: assistant, ProjectID: project, EventID: event.id, CorrelationID: "mixed", Status: eventStatusPending, NormalizedPayloadJson: []byte(event.payload), SourcePayloadJson: []byte(`{}`)})
		require.NoError(t, err)
	}
	_, err = core.AdmitPendingThreads(t.Context(), assistant)
	require.NoError(t, err)
	result, err := core.ProcessThreadEvents(t.Context(), project, thread)
	require.NoError(t, err)
	require.True(t, result.RetryAdmission, "provisioning errors remain retryable")
	require.EqualValues(t, 1, ensures.Load(), "eligible event after denial still provisions")
	latest, err := q.GetLatestAssistantThreadEventByThreadID(t.Context(), params)
	require.NoError(t, err)
	require.Equal(t, "eligible", latest.EventID)
	require.Equal(t, eventStatusPending, latest.Status)
	require.Zero(t, latest.Attempts, "preflight must not claim or consume eligible event")
}

func TestColdPreflightExcludesDeletedThread(t *testing.T) {
	t.Parallel()
	db, err := assistantsInfra.CloneTestDatabase(t, "cold_deleted_thread")
	require.NoError(t, err)
	project, _, _, thread := insertAssistantFixture(t, db)
	q := assistantrepo.New(db)
	params := assistantrepo.GetNextPendingExecutionEventParams{ProjectID: project, ThreadID: thread, PendingStatus: eventStatusPending}
	_, err = q.GetNextPendingExecutionEvent(t.Context(), params)
	require.NoError(t, err)
	require.NoError(t, q.SoftDeleteAssistantThread(t.Context(), assistantrepo.SoftDeleteAssistantThreadParams{ID: thread, ProjectID: project}))
	_, err = q.GetNextPendingExecutionEvent(t.Context(), params)
	require.ErrorIs(t, err, pgx.ErrNoRows)
}
