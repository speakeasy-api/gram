package triggers_test

import (
	"context"
	"log"
	"net/url"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"encoding/json"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/assistantidentity"
	assistantsrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/background/triggers"
	"github.com/speakeasy-api/gram/server/internal/cache"
	envrepo "github.com/speakeasy-api/gram/server/internal/environments/repo"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

var triggerInfra *testenv.Environment

func TestMain(m *testing.M) {
	infra, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{Postgres: true})
	if err != nil {
		log.Fatalf("launch trigger test infrastructure: %v", err)
	}
	triggerInfra = infra
	code := m.Run()
	if err := cleanup(); err != nil {
		log.Fatalf("cleanup trigger test infrastructure: %v", err)
	}
	os.Exit(code)
}

type identityEnvironmentLoader struct{}

func (identityEnvironmentLoader) Load(context.Context, uuid.UUID, toolconfig.SlugOrID) (map[string]string, error) {
	return map[string]string{"GITHUB_WEBHOOK_SECRET": "test-secret"}, nil
}

type identityFixture struct {
	app           *triggers.App
	db            *pgxpool.Pool
	projectID     uuid.UUID
	assistantID   uuid.UUID
	environmentID uuid.UUID
}

func newIdentityFixture(t *testing.T) identityFixture {
	t.Helper()
	ctx := t.Context()
	db, err := triggerInfra.CloneTestDatabase(t, "trigger_identity")
	require.NoError(t, err)
	_, err = orgrepo.New(db).UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{ID: "org-trigger-test", Name: "Trigger tests", Slug: "trigger-tests", WorkosID: pgtype.Text{}, Whitelisted: pgtype.Bool{}, CreationSource: pgtype.Text{}})
	require.NoError(t, err)
	project, err := projectsrepo.New(db).CreateProject(ctx, projectsrepo.CreateProjectParams{Name: "Identity tests", Slug: "identity-tests", OrganizationID: "org-trigger-test"})
	require.NoError(t, err)
	_, err = usersrepo.New(db).UpsertUser(ctx, usersrepo.UpsertUserParams{ID: "trigger-owner", Email: "trigger-owner@example.invalid", DisplayName: "Trigger owner", PhotoUrl: pgtype.Text{}, Admin: false})
	require.NoError(t, err)
	_, err = orgrepo.New(db).UpsertOrganizationUserRelationship(ctx, orgrepo.UpsertOrganizationUserRelationshipParams{OrganizationID: "org-trigger-test", UserID: pgtype.Text{String: "trigger-owner", Valid: true}})
	require.NoError(t, err)
	environment, err := envrepo.New(db).CreateEnvironment(ctx, envrepo.CreateEnvironmentParams{OrganizationID: "org-trigger-test", ProjectID: project.ID, Name: "Identity", Slug: "identity", Description: pgtype.Text{}})
	require.NoError(t, err)
	fixture := identityFixture{app: nil, db: db, projectID: project.ID, assistantID: uuid.Nil, environmentID: environment.ID}
	fixture.assistantID = fixture.createAssistant(t, true)
	baseURL, err := url.Parse("https://example.invalid")
	require.NoError(t, err)
	fixture.app = triggers.NewApp(testenv.NewLogger(t), db, nil, identityEnvironmentLoader{}, nil, nil, baseURL, baseURL, nil, nil, cache.NoopCache).SetIdentityService(testIdentityService)
	return fixture
}

func (f identityFixture) createAssistant(t *testing.T, bound bool) uuid.UUID {
	t.Helper()
	ctx := t.Context()
	tx, err := f.db.Begin(ctx) //nolint:glint // notestingrawsql: transaction boundary only; all fixture reads and writes use generated SQLc methods.
	require.NoError(t, err)
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })
	assistant, err := assistantsrepo.New(tx).CreateAssistant(ctx, assistantsrepo.CreateAssistantParams{ProjectID: f.projectID, OrganizationID: "org-trigger-test", CreatedByUserID: pgtype.Text{String: "trigger-owner", Valid: true}, Name: "Identity assistant " + uuid.NewString(), Model: "openai/gpt-4o-mini", Instructions: "", WarmTtlSeconds: 300, MaxConcurrency: 1, Status: "active"})
	require.NoError(t, err)
	if bound {
		selector, err := json.Marshal(authz.NewSelector(authz.ScopeProjectWrite, f.projectID.String()))
		require.NoError(t, err)
		_, err = accessrepo.New(tx).InsertPrincipalGrantIfAbsent(ctx, accessrepo.InsertPrincipalGrantIfAbsentParams{OrganizationID: "org-trigger-test", PrincipalUrn: urn.NewPrincipal(urn.PrincipalTypeUser, "trigger-owner"), Scope: string(authz.ScopeProjectWrite), Selectors: selector})
		require.NoError(t, err)
		_, err = testIdentityService.Provision(ctx, tx, assistantidentity.ProvisionParams{OrganizationID: "org-trigger-test", ProjectID: f.projectID, AssistantID: assistant.ID, ActorUserID: "trigger-owner"})
		require.NoError(t, err)
	}
	require.NoError(t, tx.Commit(ctx))
	return assistant.ID
}

func (f identityFixture) createParams() triggers.CreateParams {
	return triggers.CreateParams{OrganizationID: "org-trigger-test", ProjectID: f.projectID, DefinitionSlug: triggers.DefinitionSlugGithub, Name: "Root trigger", EnvironmentID: uuid.NullUUID{UUID: f.environmentID, Valid: true}, TargetKind: triggers.TargetKindAssistant, TargetRef: f.assistantID.String(), TargetDisplay: "Identity assistant", Config: map[string]any{}, Status: triggers.StatusActive}
}

var testIdentityService = func() *assistantidentity.Service {
	service, err := assistantidentity.New("https://platform.example.invalid", false)
	if err != nil {
		panic(err)
	}
	return service
}()
