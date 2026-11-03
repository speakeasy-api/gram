package slackapp

import (
	"context"
	"errors"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/assistantidentity"
	identityrepo "github.com/speakeasy-api/gram/server/internal/assistantidentity/repo"
	assistantsrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/background/triggers"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/slackdirectoryconnections"
	slackrepo "github.com/speakeasy-api/gram/server/internal/slackdirectoryconnections/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	triggerrepo "github.com/speakeasy-api/gram/server/internal/triggers/repo"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
	workloadrepo "github.com/speakeasy-api/gram/server/internal/workloadpolicy/repo"
)

const identityTestOrg = "org-slackapp-identity"

var (
	slackappInfra       *testenv.Environment
	testIdentityService = assistantidentity.New("https://platform.example.invalid", audit.NewLogger())
)

func TestMain(m *testing.M) {
	infra, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{Postgres: true})
	if err != nil {
		log.Fatalf("launch slackapp test infrastructure: %v", err)
	}
	slackappInfra = infra
	code := m.Run()
	if err := cleanup(); err != nil {
		log.Fatalf("cleanup slackapp test infrastructure: %v", err)
	}
	os.Exit(code)
}

// capturingDispatcher records the trigger instance each routed event used.
type capturingDispatcher struct{ instances []triggerrepo.TriggerInstance }

func (d *capturingDispatcher) ProcessSpeakeasySlack(_ context.Context, instance triggerrepo.TriggerInstance, _ []byte, _ http.Header, _ string) (*triggers.WebhookIngressResult, error) {
	d.instances = append(d.instances, instance)
	return nil, nil
}

type identityFixture struct {
	db          *pgxpool.Pool
	projectID   uuid.UUID
	assistantID uuid.UUID
}

func newIdentityFixture(t *testing.T) identityFixture {
	t.Helper()
	ctx := t.Context()
	db, err := slackappInfra.CloneTestDatabase(t, "slackapp_identity")
	require.NoError(t, err)
	_, err = orgrepo.New(db).UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{ID: identityTestOrg, Name: "Slack app tests", Slug: "slack-app-tests", WorkosID: pgtype.Text{}, Whitelisted: pgtype.Bool{}, CreationSource: pgtype.Text{}})
	require.NoError(t, err)
	project, err := projectsrepo.New(db).CreateProject(ctx, projectsrepo.CreateProjectParams{Name: "Slack app identity", Slug: "slack-app-identity", OrganizationID: identityTestOrg})
	require.NoError(t, err)
	_, err = usersrepo.New(db).UpsertUser(ctx, usersrepo.UpsertUserParams{ID: "slack-owner", Email: "slack-owner@example.invalid", DisplayName: "Slack owner", PhotoUrl: pgtype.Text{}, Admin: false})
	require.NoError(t, err)
	_, err = orgrepo.New(db).UpsertOrganizationUserRelationship(ctx, orgrepo.UpsertOrganizationUserRelationshipParams{OrganizationID: identityTestOrg, UserID: pgtype.Text{String: "slack-owner", Valid: true}})
	require.NoError(t, err)

	tx, err := db.Begin(ctx) //nolint:glint // notestingrawsql: transaction boundary only; all fixture reads and writes use generated SQLc methods.
	require.NoError(t, err)
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })
	assistant, err := assistantsrepo.New(tx).CreateAssistant(ctx, assistantsrepo.CreateAssistantParams{ProjectID: project.ID, OrganizationID: identityTestOrg, CreatedByUserID: pgtype.Text{String: "slack-owner", Valid: true}, Name: "Managed assistant", Model: "openai/gpt-4o-mini", Instructions: "", WarmTtlSeconds: 300, MaxConcurrency: 1, Status: "active"})
	require.NoError(t, err)
	require.NoError(t, assistantsrepo.New(tx).CreateProjectManagedAssistant(ctx, assistantsrepo.CreateProjectManagedAssistantParams{ProjectID: project.ID, AssistantID: assistant.ID}))
	require.NoError(t, testIdentityService.Provision(ctx, tx, assistantidentity.ProvisionParams{OrganizationID: identityTestOrg, ProjectID: project.ID, AssistantID: assistant.ID, ActorUserID: "slack-owner", AgentID: uuid.Nil, AgentName: ""}))
	require.NoError(t, tx.Commit(ctx))
	return identityFixture{db: db, projectID: project.ID, assistantID: assistant.ID}
}

func (f identityFixture) events(t *testing.T, dispatcher *capturingDispatcher) *Events {
	t.Helper()
	router := &fakeRouter{workspaces: []slackdirectoryconnections.RoutingWorkspace{{ConnectionID: uuid.New(), OrganizationID: identityTestOrg, TeamID: "T1", ProjectID: f.projectID, Token: "xoxb-test"}}}
	events := NewEvents(testenv.NewLogger(t), f.db, router, dispatcher, &fakePoster{}, testSecret, testIdentityService)
	events.resolveSender = func(context.Context, slackrepo.ResolveSlackMappingUserParams) (string, error) {
		return "slack-owner", nil
	}
	return events
}

func (f identityFixture) deliver(t *testing.T, events *Events) int {
	t.Helper()
	rec := httptest.NewRecorder()
	events.ServeHTTP(rec, signedRequest(t, mentionBody, testSecret))
	return rec.Code
}

func (f identityFixture) resolve(t *testing.T, triggerID uuid.UUID) assistantidentity.Resolution {
	t.Helper()
	result, err := testIdentityService.Resolve(t.Context(), f.db, identityTestOrg, f.projectID, f.assistantID, triggerID)
	require.NoError(t, err)
	return result
}

func (f identityFixture) bound(t *testing.T, triggerID uuid.UUID) bool {
	t.Helper()
	_, err := identityrepo.New(f.db).GetTriggerBinding(t.Context(), identityrepo.GetTriggerBindingParams{ProjectID: f.projectID, TriggerID: triggerID})
	if errors.Is(err, pgx.ErrNoRows) {
		return false
	}
	require.NoError(t, err)
	return true
}

func (f identityFixture) slackTrigger(t *testing.T) (triggerrepo.TriggerInstance, bool) {
	t.Helper()
	instance, err := triggerrepo.New(f.db).GetSpeakeasySlackTriggerInstance(t.Context(), triggerrepo.GetSpeakeasySlackTriggerInstanceParams{ProjectID: f.projectID, TargetKind: triggers.TargetKindAssistant, TargetRef: f.assistantID.String()})
	if errors.Is(err, pgx.ErrNoRows) {
		return triggerrepo.TriggerInstance{}, false
	}
	require.NoError(t, err)
	return instance, true
}

func TestEventsCreatedTriggerResolvesActiveIdentity(t *testing.T) {
	t.Parallel()
	f := newIdentityFixture(t)
	dispatcher := &capturingDispatcher{}
	events := f.events(t, dispatcher)

	require.Equal(t, http.StatusOK, f.deliver(t, events))
	require.Len(t, dispatcher.instances, 1)
	first := dispatcher.instances[0]
	require.True(t, f.bound(t, first.ID), "a newly created Speakeasy Slack trigger is bound to a workload identity")
	resolved := f.resolve(t, first.ID)
	require.Equal(t, assistantidentity.Active, resolved.State)
	require.NotNil(t, resolved.Identity)
	require.Equal(t, first.ID, resolved.Identity.TriggerID)

	// A second event reuses the trigger and leaves the identity unchanged.
	require.Equal(t, http.StatusOK, f.deliver(t, events))
	require.Len(t, dispatcher.instances, 2)
	require.Equal(t, first.ID, dispatcher.instances[1].ID)
	again := f.resolve(t, first.ID)
	require.Equal(t, assistantidentity.Active, again.State)
	require.Equal(t, *resolved.Identity, *again.Identity)
	require.NoError(t, testIdentityService.Validate(t.Context(), f.db, *again.Identity))
}

func TestEventsRepairsPreExistingUnboundTrigger(t *testing.T) {
	t.Parallel()
	f := newIdentityFixture(t)
	// A trigger created before the fix was never bound.
	existing, err := triggerrepo.New(f.db).CreateSpeakeasySlackTriggerInstance(t.Context(), triggerrepo.CreateSpeakeasySlackTriggerInstanceParams{
		OrganizationID: identityTestOrg, ProjectID: f.projectID, Name: "Speakeasy Slack app",
		TargetKind: triggers.TargetKindAssistant, TargetRef: f.assistantID.String(), TargetDisplay: "Managed assistant",
		ConfigJson: triggers.SpeakeasySlackConfig,
	})
	require.NoError(t, err)
	require.False(t, f.bound(t, existing.ID))

	dispatcher := &capturingDispatcher{}
	require.Equal(t, http.StatusOK, f.deliver(t, f.events(t, dispatcher)))
	require.Len(t, dispatcher.instances, 1)
	require.Equal(t, existing.ID, dispatcher.instances[0].ID, "the existing trigger is reused")
	require.True(t, f.bound(t, existing.ID), "the existing trigger is repaired")
	require.Equal(t, assistantidentity.Active, f.resolve(t, existing.ID).State)
}

func TestEventsBindFailureRollsBackTriggerCreation(t *testing.T) {
	t.Parallel()
	f := newIdentityFixture(t)
	// Squat on the issuer name the binder registers, under another issuer URL,
	// so binding the new trigger fails.
	squatter, err := workloadrepo.New(f.db).CreateWorkloadIssuer(t.Context(), workloadrepo.CreateWorkloadIssuerParams{
		OrganizationID: identityTestOrg, ProjectID: uuid.NullUUID{UUID: f.projectID, Valid: true},
		Name: "Assistant triggers " + f.projectID.String(), Description: pgtype.Text{}, Tags: []string{},
		Issuer: "https://squatter.example.invalid", JwksUri: "https://squatter.example.invalid/jwks",
		AllowWildcardAdmission: false, AllowAssertionReuse: pgtype.Bool{},
	})
	require.NoError(t, err)

	dispatcher := &capturingDispatcher{}
	events := f.events(t, dispatcher)
	require.Equal(t, http.StatusInternalServerError, f.deliver(t, events))
	require.Empty(t, dispatcher.instances, "an event whose trigger cannot be bound is not dispatched")
	_, found := f.slackTrigger(t)
	require.False(t, found, "the trigger created in the failed transaction is rolled back")

	// Once the conflict is gone, Slack's retry creates and binds the trigger.
	_, err = workloadrepo.New(f.db).SoftDeleteWorkloadIssuer(t.Context(), workloadrepo.SoftDeleteWorkloadIssuerParams{OrganizationID: identityTestOrg, ProjectID: uuid.NullUUID{UUID: f.projectID, Valid: true}, ID: squatter.ID})
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, f.deliver(t, events))
	require.Len(t, dispatcher.instances, 1)
	require.Equal(t, assistantidentity.Active, f.resolve(t, dispatcher.instances[0].ID).State)
}
