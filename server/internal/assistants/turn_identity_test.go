package assistants

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"testing"
	"time"

	slackrepo "github.com/speakeasy-api/gram/server/internal/slackdirectoryconnections/repo"
	"github.com/stretchr/testify/require"
)

func TestSelectTurnUser(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, source, payload, mapped, want string
		legacyUsers                         []string
		lookupErr                           error
		wantErr                             bool
	}{
		{name: "mapped Slack", source: sourceKindSlack, payload: `{"team_id":"workspace-a","user_id":"slack-sender"}`, mapped: "sender", want: "sender"},
		{name: "unmapped Slack", source: sourceKindSlack, payload: `{"team_id":"workspace-a","user_id":"slack-sender"}`, want: "owner"},
		{name: "mapping unavailable", source: sourceKindSlack, payload: `{"team_id":"workspace-a","user_id":"slack-sender"}`, lookupErr: errors.New("unavailable"), want: "owner"},
		{name: "ordinary trigger", source: sourceKindCron, payload: `{}`, want: "owner"},
		{name: "wake requester", source: sourceKindWake, payload: `{"identity_version":1,"requester_user_id":"requester"}`, legacyUsers: []string{"scheduler"}, want: "requester"},
		{name: "wake captured owner", source: sourceKindWake, payload: `{"identity_version":1,"requester_user_id":"original-owner"}`, want: "original-owner"},
		{name: "wake reuses Slack thread", source: sourceKindSlack, payload: `{"_gram_source_kind":"wake","identity_version":1,"requester_user_id":"requester"}`, want: "requester"},
		{name: "unversioned requester ignored without audit", source: sourceKindWake, payload: `{"requester_user_id":"supplied-requester"}`, want: "owner"},
		{name: "version zero requester cannot override scheduler", source: sourceKindWake, payload: `{"identity_version":0,"requester_user_id":"supplied-requester"}`, legacyUsers: []string{"scheduler"}, want: "scheduler"},
		{name: "unversioned requester cannot disambiguate audit", source: sourceKindWake, payload: `{"requester_user_id":"supplied-requester"}`, legacyUsers: []string{"scheduler-a", "scheduler-b"}, want: "owner"},
		{name: "legacy wake recorded scheduler", source: sourceKindWake, payload: `{}`, legacyUsers: []string{"scheduler"}, want: "scheduler"},
		{name: "legacy wake retains owner", source: sourceKindWake, payload: `{}`, want: "owner"},
		{name: "legacy wake in Slack thread", source: sourceKindSlack, payload: `{"scheduled_at":"2026-01-01T00:00:00Z"}`, want: "owner"},
		{name: "new wake missing capture fails closed", source: sourceKindWake, payload: `{"identity_version":1}`, wantErr: true},
		{name: "unsupported wake version fails closed", source: sourceKindWake, payload: `{"identity_version":2,"requester_user_id":"requester"}`, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assistant := assistantRecord{OrganizationID: "org-a", CreatedByUserID: "owner"}
			event := assistantThreadEventRecord{TriggerInstanceID: uuid.NullUUID{UUID: uuid.New(), Valid: true}, NormalizedPayloadJSON: []byte(tc.payload)}
			user, err := selectTurnUser(t.Context(), assistant, tc.source, event, func(_ context.Context, p slackrepo.ResolveSlackMappingUserParams) (string, error) {
				require.Equal(t, "org-a", p.OrganizationID)
				require.Equal(t, "workspace-a", p.SlackTeamID)
				require.Equal(t, "slack-sender", p.SlackUserID)
				return tc.mapped, tc.lookupErr
			}, func(_ context.Context, p assistantrepo.FindLegacyWakeRequesterParams) ([]string, error) {
				require.Equal(t, "org-a", p.OrganizationID)
				require.Equal(t, event.TriggerInstanceID.UUID.String(), p.TriggerID)
				return tc.legacyUsers, nil
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

func TestTurnUserIneligibleRequesterDoesNotRetryAsOwner(t *testing.T) {
	t.Parallel()
	db, err := assistantsInfra.CloneTestDatabase(t, "turn_identity")
	require.NoError(t, err)
	seedTurnUser(t, db, "org-turn", "eligible-owner")
	project := uuid.New()
	seedTurnProjectAccess(t, db, "org-turn", "eligible-owner", project)
	core := &ServiceCore{db: db}
	assistant := assistantRecord{ProjectID: project, OrganizationID: "org-turn", CreatedByUserID: "eligible-owner"}
	thread := assistantThreadRecord{SourceKind: sourceKindWake}
	user, err := core.turnUserID(t.Context(), assistant, thread, assistantThreadEventRecord{NormalizedPayloadJSON: []byte(`{"identity_version":1,"requester_user_id":"ineligible-requester"}`)})
	require.ErrorContains(t, err, "not an active organization member")
	require.Empty(t, user)
	user, err = core.turnUserID(t.Context(), assistant, thread, assistantThreadEventRecord{NormalizedPayloadJSON: []byte(`{"identity_version":1,"requester_user_id":"eligible-owner"}`)})
	require.NoError(t, err)
	require.Equal(t, "eligible-owner", user)
}

func TestLegacyWakeUsesRecordedRequesterWithoutOwnerRetry(t *testing.T) {
	t.Parallel()
	db, err := assistantsInfra.CloneTestDatabase(t, "legacy_wake_identity")
	require.NoError(t, err)
	project, assistantID, _, _ := insertAssistantFixture(t, db)
	seedTurnUser(t, db, "org-test", "recorded-requester")
	seedTurnProjectAccess(t, db, "org-test", "recorded-requester", project)
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

func seedTurnProjectAccess(t *testing.T, db *pgxpool.Pool, org, user string, project uuid.UUID) uuid.UUID {
	t.Helper()
	selectors, err := authz.NewSelector(authz.ScopeProjectRead, project.String()).MarshalJSON()
	require.NoError(t, err)
	grant, err := accessrepo.New(db).UpsertPrincipalGrant(t.Context(), accessrepo.UpsertPrincipalGrantParams{
		OrganizationID: org, PrincipalUrn: urn.NewPrincipal(urn.PrincipalTypeUser, user),
		Scope: string(authz.ScopeProjectRead), Selectors: selectors,
	})
	require.NoError(t, err)
	return grant.ID
}

func TestTurnUserRequiresCurrentProjectAccess(t *testing.T) {
	t.Parallel()
	for _, source := range []string{sourceKindSlack, sourceKindWake} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			db, err := assistantsInfra.CloneTestDatabase(t, "turn_project_access_"+source)
			require.NoError(t, err)
			project, assistantID, _, _ := insertAssistantFixture(t, db)
			seedTurnUser(t, db, "org-test", "selected-user")
			if source == sourceKindSlack {
				q := slackrepo.New(db)
				_, err = q.CreateSlackDirectoryConnection(t.Context(), slackrepo.CreateSlackDirectoryConnectionParams{
					OrganizationID: "org-test", SlackTeamID: "workspace", SlackTeamName: conv.ToPGText("Test workspace"),
					CredentialsEncrypted: conv.ToPGTextEmpty(""), GrantedScopes: []string{}, Generation: uuid.New(),
				})
				require.NoError(t, err)
				require.NoError(t, q.UpsertSlackDirectoryMembershipBatch(t.Context(), slackrepo.UpsertSlackDirectoryMembershipBatchParams{
					OrganizationID: "org-test", SlackTeamID: "workspace", LastSeenAt: conv.ToPGTimestamptz(time.Now()),
					UserIds: []string{"sender"}, DisplayNames: []string{"Selected sender"}, Emails: []string{"sender@example.invalid"},
					Statuses: []string{"active"}, MemberTypes: []string{"person"}, ProviderUpdatedAts: []pgtype.Timestamptz{{}},
				}))
				_, err = q.CreateSlackMappingForTest(t.Context(), slackrepo.CreateSlackMappingForTestParams{
					OrganizationID: "org-test", SlackTeamID: "workspace", SlackUserID: "sender", UserID: "selected-user",
				})
				require.NoError(t, err)
			}
			core := &ServiceCore{db: db}
			assistant := assistantRecord{ID: assistantID, ProjectID: project, OrganizationID: "org-test", CreatedByUserID: "fixture-owner"}
			thread := assistantThreadRecord{SourceKind: source}
			payload := `{"team_id":"workspace","user_id":"sender"}`
			if source == sourceKindWake {
				payload = `{"identity_version":1,"requester_user_id":"selected-user"}`
			}
			event := assistantThreadEventRecord{NormalizedPayloadJSON: []byte(payload)}
			assertDenied := func() {
				t.Helper()
				user, err := core.turnUserID(t.Context(), assistant, thread, event)
				require.ErrorContains(t, err, "does not have access to assistant project")
				require.Empty(t, user, "denial must not retry as the authorized owner")
			}
			assertDenied()
			// Neither a different project nor another tenant's grant authorizes this turn.
			seedTurnProjectAccess(t, db, "org-test", "selected-user", uuid.New())
			seedTurnUser(t, db, "other-org", "selected-user")
			seedTurnProjectAccess(t, db, "other-org", "selected-user", project)
			assertDenied()
			grantID := seedTurnProjectAccess(t, db, "org-test", "selected-user", project)
			user, err := core.turnUserID(t.Context(), assistant, thread, event)
			require.NoError(t, err)
			require.Equal(t, "selected-user", user)
			deleted, err := accessrepo.New(db).DeletePrincipalGrant(t.Context(), accessrepo.DeletePrincipalGrantParams{ID: grantID, OrganizationID: "org-test"})
			require.NoError(t, err)
			require.EqualValues(t, 1, deleted)
			assertDenied()
		})
	}
}
