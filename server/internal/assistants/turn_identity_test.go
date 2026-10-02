package assistants

import (
	"context"
	"errors"
	"github.com/google/uuid"
	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"testing"

	slackrepo "github.com/speakeasy-api/gram/server/internal/slackdirectoryconnections/repo"
	"github.com/stretchr/testify/require"
)

func TestSelectTurnUser(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, source, payload, mapped, want string
		lookupErr                           error
		wantErr                             bool
	}{
		{name: "mapped Slack", source: sourceKindSlack, payload: `{"team_id":"workspace-a","user_id":"slack-sender"}`, mapped: "sender", want: "sender"},
		{name: "unmapped Slack", source: sourceKindSlack, payload: `{"team_id":"workspace-a","user_id":"slack-sender"}`, want: "owner"},
		{name: "mapping unavailable", source: sourceKindSlack, payload: `{"team_id":"workspace-a","user_id":"slack-sender"}`, lookupErr: errors.New("unavailable"), want: "owner"},
		{name: "ordinary trigger", source: sourceKindCron, payload: `{}`, want: "owner"},
		{name: "wake requester", source: sourceKindWake, payload: `{"requester_user_id":"requester"}`, want: "requester"},
		{name: "wake captured owner", source: sourceKindWake, payload: `{"requester_user_id":"original-owner"}`, want: "original-owner"},
		{name: "wake reuses Slack thread", source: sourceKindSlack, payload: `{"_gram_source_kind":"wake","requester_user_id":"requester"}`, want: "requester"},
		{name: "legacy wake retains owner", source: sourceKindWake, payload: `{}`, want: "owner"},
		{name: "legacy wake in Slack thread", source: sourceKindSlack, payload: `{"scheduled_at":"2026-01-01T00:00:00Z"}`, want: "owner"},
		{name: "new wake missing capture fails closed", source: sourceKindWake, payload: `{"identity_version":1}`, wantErr: true},
		{name: "unsupported wake version fails closed", source: sourceKindWake, payload: `{"identity_version":2,"requester_user_id":"requester"}`, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assistant := assistantRecord{OrganizationID: "org-a", CreatedByUserID: "owner"}
			event := assistantThreadEventRecord{NormalizedPayloadJSON: []byte(tc.payload)}
			user, err := selectTurnUser(t.Context(), assistant, tc.source, event, func(_ context.Context, p slackrepo.ResolveSlackMappingUserParams) (string, error) {
				require.Equal(t, "org-a", p.OrganizationID)
				require.Equal(t, "workspace-a", p.SlackTeamID)
				require.Equal(t, "slack-sender", p.SlackUserID)
				return tc.mapped, tc.lookupErr
			}, nil)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, user)
		})
	}
}

func TestTurnUserIneligibleRequesterDoesNotRetryAsOwner(t *testing.T) {
	t.Parallel()
	db, err := assistantsInfra.CloneTestDatabase(t, "turn_identity")
	require.NoError(t, err)
	seedTurnUser(t, db, "org-turn", "eligible-owner")
	core := &ServiceCore{db: db}
	assistant := assistantRecord{OrganizationID: "org-turn", CreatedByUserID: "eligible-owner"}
	thread := assistantThreadRecord{SourceKind: sourceKindWake}
	user, err := core.turnUserID(t.Context(), assistant, thread, assistantThreadEventRecord{NormalizedPayloadJSON: []byte(`{"requester_user_id":"ineligible-requester"}`)})
	require.ErrorContains(t, err, "not an active organization member")
	require.Empty(t, user)
	user, err = core.turnUserID(t.Context(), assistant, thread, assistantThreadEventRecord{NormalizedPayloadJSON: []byte(`{"requester_user_id":"eligible-owner"}`)})
	require.NoError(t, err)
	require.Equal(t, "eligible-owner", user)
}

func TestLegacyWakeUsesRecordedRequesterWithoutOwnerRetry(t *testing.T) {
	t.Parallel()
	db, err := assistantsInfra.CloneTestDatabase(t, "legacy_wake_identity")
	require.NoError(t, err)
	project, assistantID, _, _ := insertAssistantFixture(t, db)
	seedTurnUser(t, db, "org-test", "recorded-requester")
	triggerID := uuid.New()
	logger := audit.NewLogger()
	require.NoError(t, logger.LogWakeScheduled(t.Context(), db, audit.LogWakeEvent{
		OrganizationID: "org-test", ProjectID: project, Actor: urn.NewPrincipal(urn.PrincipalTypeUser, "recorded-requester"), TriggerInstanceURN: urn.NewTriggerInstance(triggerID), Name: "Wake", Correlation: "thread", FireAt: "2026-01-01T00:00:00Z",
	}))
	core := &ServiceCore{db: db}
	assistant := assistantRecord{ID: assistantID, ProjectID: project, OrganizationID: "org-test", CreatedByUserID: "fixture-owner"}
	event := assistantThreadEventRecord{TriggerInstanceID: uuid.NullUUID{UUID: triggerID, Valid: true}, NormalizedPayloadJSON: []byte(`{"scheduled_at":"2026-01-01T00:00:00Z"}`)}
	thread := assistantThreadRecord{SourceKind: sourceKindSlack}
	user, err := core.turnUserID(t.Context(), assistant, thread, event)
	require.NoError(t, err)
	require.Equal(t, "recorded-requester", user)
	// Audit lookup is tenant/project scoped, not a global trigger-ID lookup.
	q := assistantrepo.New(db)
	rows, err := q.FindLegacyWakeRequester(t.Context(), assistantrepo.FindLegacyWakeRequesterParams{OrganizationID: "other-org", ProjectID: project, TriggerID: triggerID.String()})
	require.NoError(t, err)
	require.Empty(t, rows)
	rows, err = q.FindLegacyWakeRequester(t.Context(), assistantrepo.FindLegacyWakeRequesterParams{OrganizationID: "org-test", ProjectID: uuid.New(), TriggerID: triggerID.String()})
	require.NoError(t, err)
	require.Empty(t, rows)
	require.NoError(t, slackrepo.New(db).DeactivateSlackMappingPersonForTest(t.Context(), slackrepo.DeactivateSlackMappingPersonForTestParams{OrganizationID: "org-test", UserID: conv.ToPGText("recorded-requester")}))
	user, err = core.turnUserID(t.Context(), assistant, thread, event)
	require.ErrorContains(t, err, "not an active organization member")
	require.Empty(t, user)
	// No scheduling audit retains legacy owner selection.
	event.TriggerInstanceID.UUID = uuid.New()
	user, err = core.turnUserID(t.Context(), assistant, thread, event)
	require.NoError(t, err)
	require.Equal(t, "fixture-owner", user)
}
