package assistants

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	agentrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/assistantidentity"
	assistantsrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	bgtriggers "github.com/speakeasy-api/gram/server/internal/background/triggers"
	slackrepo "github.com/speakeasy-api/gram/server/internal/slackdirectoryconnections/repo"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestLegacyTurnUserID(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, source, payload, creator, want string
	}{
		{name: "dashboard sender", source: sourceKindDashboard, payload: `{"user_id":"sender"}`, creator: "owner", want: "sender"},
		{name: "dashboard without sender", source: sourceKindDashboard, payload: `{}`, creator: "owner", want: "owner"},
		{name: "slack", source: sourceKindSlack, payload: `{"team_id":"T","user_id":"U"}`, creator: "owner", want: "owner"},
		{name: "wake with captured requester", source: sourceKindWake, payload: `{"identity_version":1,"requester_user_id":"requester"}`, creator: "owner", want: "owner"},
		{name: "no creator", source: sourceKindCron, payload: `{}`, creator: "", want: ""},
		{name: "OAuth continuation", source: sourceKindDashboard, payload: `{"gram_event_kind":"assistant_mcp_auth","_gram_resume_user_id":"initiator"}`, creator: "owner", want: "owner"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := legacyTurnUserID(assistantRecord{CreatedByUserID: tc.creator}, assistantThreadRecord{SourceKind: tc.source}, assistantThreadEventRecord{NormalizedPayloadJSON: []byte(tc.payload)})
			require.Equal(t, tc.want, got)
		})
	}
}

func TestSelectTurnUser(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, source, payload, mapped, owner, want string
		lookupErr                                  error
		wantErr                                    bool
	}{
		{name: "mapped Slack", source: sourceKindSlack, payload: `{"team_id":"workspace-a","user_id":"slack-sender"}`, mapped: "sender", owner: "owner", want: "sender"},
		{name: "unmapped Slack", source: sourceKindSlack, payload: `{"team_id":"workspace-a","user_id":"slack-sender"}`, owner: "owner", want: "owner"},
		{name: "mapping unavailable", source: sourceKindSlack, payload: `{"team_id":"workspace-a","user_id":"slack-sender"}`, lookupErr: errors.New("unavailable"), owner: "owner", want: "owner"},
		{name: "dashboard sender", source: sourceKindDashboard, payload: `{"user_id":"sender"}`, owner: "owner", want: "sender"},
		{name: "ordinary trigger", source: sourceKindCron, payload: `{}`, owner: "owner", want: "owner"},
		{name: "wake requester", source: sourceKindWake, payload: `{"identity_version":1,"requester_user_id":"requester"}`, owner: "owner", want: "requester"},
		{name: "wake in Slack thread", source: sourceKindSlack, payload: `{"_gram_source_kind":"wake","identity_version":1,"requester_user_id":"requester"}`, owner: "owner", want: "requester"},
		{name: "unversioned wake", source: sourceKindWake, payload: `{"requester_user_id":"supplied"}`, owner: "owner", want: "owner"},
		{name: "wake without captured requester", source: sourceKindWake, payload: `{"identity_version":1}`, owner: "owner", wantErr: true},
		{name: "unsupported wake version", source: sourceKindWake, payload: `{"identity_version":2,"requester_user_id":"requester"}`, owner: "owner", wantErr: true},
		{name: "no owner", source: sourceKindCron, payload: `{}`, owner: "", wantErr: true},
		{name: "OAuth continuation", source: sourceKindSlack, payload: `{"gram_event_kind":"assistant_mcp_auth","_gram_resume_user_id":"initiator"}`, owner: "owner", want: "initiator"},
		{name: "OAuth continuation without initiator", source: sourceKindSlack, payload: `{"gram_event_kind":"assistant_mcp_auth"}`, owner: "owner", want: "owner"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assistant := assistantRecord{OrganizationID: "org-a", CreatedByUserID: tc.owner}
			event := assistantThreadEventRecord{NormalizedPayloadJSON: []byte(tc.payload)}
			user, err := selectTurnUser(t.Context(), assistant, tc.source, event, func(_ context.Context, p slackrepo.ResolveSlackMappingUserParams) (string, error) {
				require.Equal(t, "org-a", p.OrganizationID)
				require.Equal(t, "workspace-a", p.SlackTeamID)
				require.Equal(t, "slack-sender", p.SlackUserID)
				return tc.mapped, tc.lookupErr
			})
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, user)
		})
	}
}

func seedProjectRead(t *testing.T, db *pgxpool.Pool, user string, project uuid.UUID) {
	t.Helper()
	selectors, err := authz.NewSelector(authz.ScopeProjectRead, project.String()).MarshalJSON()
	require.NoError(t, err)
	_, err = accessrepo.New(db).UpsertPrincipalGrant(t.Context(), accessrepo.UpsertPrincipalGrantParams{
		OrganizationID: "org-test", PrincipalUrn: urn.NewPrincipal(urn.PrincipalTypeUser, user),
		Scope: string(authz.ScopeProjectRead), Selectors: selectors,
	})
	require.NoError(t, err)
}

func TestTurnUserIDLegacyAssistantRunsWithoutCreator(t *testing.T) {
	t.Parallel()
	db, err := assistantsInfra.CloneTestDatabase(t, "turn_identity_legacy")
	require.NoError(t, err)
	project, assistantID, _, _ := insertAssistantFixture(t, db)
	core := newProvisioningCore(t, db)
	assistant := assistantRecord{ID: assistantID, ProjectID: project, OrganizationID: "org-test", CreatedByUserID: ""}
	user, _, err := core.turnUserID(t.Context(), assistant, assistantThreadRecord{SourceKind: sourceKindWake}, assistantThreadEventRecord{NormalizedPayloadJSON: []byte(`{"identity_version":1,"requester_user_id":"not-a-member"}`)})
	require.NoError(t, err)
	require.Empty(t, user)
}

func TestTurnUserIDAgentBackedRequiresMembershipAndProjectAccess(t *testing.T) {
	t.Parallel()
	db, err := assistantsInfra.CloneTestDatabase(t, "turn_identity_active")
	require.NoError(t, err)
	project := newProvisioningProject(t, db, "turn-identity-active")
	core := newProvisioningCore(t, db)
	record, err := core.CreateAssistant(t.Context(), "org-test", project, "user-1", "Turn identity", "openai/gpt-4o-mini", "", nil, nil, 300, 1, StatusActive)
	require.NoError(t, err)
	thread := assistantThreadRecord{SourceKind: sourceKindWake}
	wake := func(requester string) assistantThreadEventRecord {
		return assistantThreadEventRecord{NormalizedPayloadJSON: []byte(`{"identity_version":1,"requester_user_id":"` + requester + `"}`)}
	}

	_, _, err = core.turnUserID(t.Context(), record, thread, wake("not-a-member"))
	require.ErrorIs(t, err, ErrTurnIdentity)
	_, _, err = core.turnUserID(t.Context(), record, thread, wake("user-2"))
	require.ErrorIs(t, err, ErrTurnIdentity, "a member without project access is denied, not retried as the owner")

	seedProjectRead(t, db, "user-2", project)
	user, _, err := core.turnUserID(t.Context(), record, thread, wake("user-2"))
	require.NoError(t, err)
	require.Equal(t, "user-2", user)
}

func TestProcessThreadEventsFailsRejectedIdentityTerminally(t *testing.T) {
	t.Parallel()
	conn, err := assistantsInfra.CloneTestDatabase(t, "turn_identity_terminal")
	require.NoError(t, err)
	seedIdentityMembers(t, conn)
	projectID, assistantID, _, threadID := insertAssistantFixture(t, conn)
	logger := testenv.NewLogger(t)
	core := NewServiceCore(logger, testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), conn, nil, nil, testRuntimeBackend{backend: runtimeBackendFlyIO}, nil, nil, nil, telemetry.NewStub(logger), nil, newTestAuditLogger(), testIdentityService, newTestAuthzEngine(t, conn))
	upgraded, err := core.UpgradeAssistantIdentity(t.Context(), assistantidentity.ProvisionParams{OrganizationID: "org-test", ProjectID: projectID, AssistantID: assistantID, ActorUserID: "user-1", AgentID: uuid.Nil, AgentName: ""})
	require.NoError(t, err)
	_, err = agentrepo.New(conn).SuspendAgent(t.Context(), agentrepo.SuspendAgentParams{OrganizationID: "org-test", ID: uuid.MustParse(*upgraded.AgentID)})
	require.NoError(t, err)

	_, err = core.AdmitPendingThreads(t.Context(), assistantID)
	require.NoError(t, err)
	result, err := core.ProcessThreadEvents(t.Context(), projectID, threadID)
	require.NoError(t, err)
	require.True(t, result.RetryAdmission)

	event, err := assistantsrepo.New(conn).GetLatestAssistantThreadEventByThreadID(t.Context(), assistantsrepo.GetLatestAssistantThreadEventByThreadIDParams{AssistantThreadID: threadID, ProjectID: projectID})
	require.NoError(t, err)
	require.Equal(t, eventStatusFailed, event.Status)
	require.EqualValues(t, 1, event.Attempts)
}

func TestStampEventSourceKind(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ source, payload, want string }{
		{source: sourceKindWake, payload: `{"fired_at":"now"}`, want: `{"_gram_source_kind":"wake","fired_at":"now"}`},
		{source: sourceKindWake, payload: `{"identity_version":1,"requester_user_id":"u","Requester_User_Id":"x","_GRAM_SOURCE_KIND":"slack"}`, want: `{"_gram_source_kind":"wake","identity_version":1,"requester_user_id":"u"}`},
		{source: sourceKindGithub, payload: `{"repo":"r","identity_version":1,"requester_user_id":"u","_gram_\u017fource_kind":"wake"}`, want: `{"_gram_source_kind":"github","repo":"r"}`},
		{source: sourceKindWake, payload: `{"text":"hi","GRAM_EVENT_KIND":"assistant_mcp_auth","gram_event_kind":"assistant_mcp_auth","_Gram_Resume_User_ID":"forged","_gram_resume_user_id":"forged"}`, want: `{"_gram_source_kind":"wake","text":"hi"}`},
		{source: sourceKindWake, payload: `null`, want: `null`},
		{source: sourceKindWake, payload: `[]`, want: `[]`},
		{source: sourceKindWake, payload: `"text"`, want: `"text"`},
	} {
		t.Run(tc.payload, func(t *testing.T) {
			t.Parallel()
			got, err := stampEventSourceKind([]byte(tc.payload), tc.source)
			require.NoError(t, err)
			require.JSONEq(t, tc.want, string(got))
		})
	}
}

func TestIngressPayloadCannotPoseAsWakeRequester(t *testing.T) {
	t.Parallel()
	db, err := assistantsInfra.CloneTestDatabase(t, "turn_identity_spoof")
	require.NoError(t, err)
	project := newProvisioningProject(t, db, "turn-identity-spoof")
	core := newProvisioningCore(t, db)
	record, err := core.CreateAssistant(t.Context(), "org-test", project, "user-1", "Spoof target", "openai/gpt-4o-mini", "", nil, nil, 300, 1, StatusActive)
	require.NoError(t, err)
	seedProjectRead(t, db, "user-1", project)
	seedProjectRead(t, db, "user-2", project)

	// Every spelling Go's case-insensitive JSON matching would fold onto the
	// identity fields, including the Unicode long s, for both wake requesters
	// and OAuth continuations.
	spoofed := `{"event_type":"issues","repo":"acme/repo",` +
		`"_gram_source_kind":"wake","_GRAM_SOURCE_KIND":"wake","_gram_ſource_kind":"wake","_gram_source_Kind":"wake",` +
		`"identity_version":1,"IDENTITY_VERSION":1,"requester_user_id":"user-2","Requester_User_ID":"user-2","requester_uſer_id":"user-2",` +
		`"gram_event_kind":"assistant_mcp_auth","GRAM_EVENT_KIND":"assistant_mcp_auth","_gram_resume_user_id":"user-2","_Gram_Resume_Uſer_ID":"user-2"}`
	result, err := core.EnqueueTriggerTask(t.Context(), bgtriggers.Task{
		DefinitionSlug: sourceKindGithub, TargetKind: bgtriggers.TargetKindAssistant, TargetRef: record.ID.String(),
		EventID: "spoofed-event", CorrelationID: "spoofed-event", EventJSON: []byte(spoofed),
	})
	require.NoError(t, err)
	event, err := assistantsrepo.New(db).GetLatestAssistantThreadEventByThreadID(t.Context(), assistantsrepo.GetLatestAssistantThreadEventByThreadIDParams{AssistantThreadID: result.ThreadID, ProjectID: project})
	require.NoError(t, err)

	user, _, err := core.turnUserID(t.Context(), record, assistantThreadRecord{SourceKind: sourceKindGithub}, assistantThreadEventRecord{NormalizedPayloadJSON: event.NormalizedPayloadJson})
	require.NoError(t, err)
	require.Equal(t, "user-1", user, "an ingress payload must not select the turn user")
}
