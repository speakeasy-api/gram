package skills

import (
	"context"
	"log"
	"os"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	projectrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	skillsrepo "github.com/speakeasy-api/gram/server/internal/skills/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

var infra *testenv.Environment

func TestMain(m *testing.M) {
	res, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{Postgres: true})
	if err != nil {
		log.Fatalf("Failed to launch test infrastructure: %v", err)
	}

	infra = res
	code := m.Run()

	if err := cleanup(); err != nil {
		log.Fatalf("Failed to cleanup test infrastructure: %v", err)
	}
	os.Exit(code)
}

// skillLoadFixture is one assistant in one project with one attached skill —
// the smallest estate a skills_load call can succeed against.
type skillLoadFixture struct {
	conn      *pgxpool.Pool
	projectID uuid.UUID
	version   skillsrepo.SkillVersion
}

func newSkillLoadFixture(t *testing.T, name string) (context.Context, *skillLoadFixture) {
	t.Helper()

	ctx := t.Context()
	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)

	organizationID := "platform-skills-org-" + uuid.NewString()
	_, err = orgrepo.New(conn).UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{
		ID:          organizationID,
		Name:        organizationID,
		Slug:        organizationID,
		WorkosID:    pgtype.Text{},
		Whitelisted: pgtype.Bool{},
	})
	require.NoError(t, err)

	projectSlug := "platform-skills-" + uuid.NewString()[:8]
	project, err := projectrepo.New(conn).CreateProject(ctx, projectrepo.CreateProjectParams{
		Name:           projectSlug,
		Slug:           projectSlug,
		OrganizationID: organizationID,
	})
	require.NoError(t, err)

	assistant, err := assistantrepo.New(conn).CreateAssistant(ctx, assistantrepo.CreateAssistantParams{
		ProjectID:       project.ID,
		OrganizationID:  organizationID,
		CreatedByUserID: pgtype.Text{String: "user-test", Valid: true},
		Name:            "Efficacy wake assistant",
		Model:           "test-model",
		Instructions:    "",
		WarmTtlSeconds:  60,
		MaxConcurrency:  1,
		Status:          "active",
	})
	require.NoError(t, err)

	queries := skillsrepo.New(conn)
	skill, err := queries.CreateSkill(ctx, skillsrepo.CreateSkillParams{
		ProjectID:   project.ID,
		Name:        name,
		DisplayName: name,
		Summary:     pgtype.Text{},
	})
	require.NoError(t, err)
	version, err := queries.CreateSkillVersion(ctx, skillsrepo.CreateSkillVersionParams{
		Content:          "---\nname: " + name + "\ndescription: first\n---\n\nbody\n",
		CanonicalSha256:  uuid.NewString(),
		RawSha256:        uuid.NewString(),
		Description:      pgtype.Text{String: "first", Valid: true},
		Metadata:         []byte(`{}`),
		SpecValid:        true,
		ValidationErrors: []byte(`[]`),
		CreatedByUserID:  "user-test",
		ProjectID:        project.ID,
		SkillID:          skill.ID,
	})
	require.NoError(t, err)
	_, err = queries.CreateSkillDistribution(ctx, skillsrepo.CreateSkillDistributionParams{
		PluginID:        uuid.NullUUID{},
		AssistantID:     uuid.NullUUID{UUID: assistant.ID, Valid: true},
		PinnedVersionID: uuid.NullUUID{},
		Channel:         "assistant",
		CreatedByUserID: "user-test",
		ProjectID:       project.ID,
		SkillID:         skill.ID,
	})
	require.NoError(t, err)

	ctx = contextvalues.SetAuthContext(ctx, &contextvalues.AuthContext{
		ActiveOrganizationID: organizationID,
		ProjectID:            &project.ID,
		ProjectSlug:          &project.Slug,
	})
	ctx = contextvalues.SetAssistantPrincipal(ctx, contextvalues.AssistantPrincipal{
		AssistantID: assistant.ID,
		ThreadID:    uuid.New(),
	})

	return ctx, &skillLoadFixture{conn: conn, projectID: project.ID, version: version}
}

func skillToolCallEnv(chatID string) toolconfig.ToolCallEnv {
	return toolconfig.ToolCallEnv{
		SystemEnv:  toolconfig.NewCaseInsensitiveEnv(),
		UserConfig: toolconfig.NewCaseInsensitiveEnv(),
		OAuthToken: "",
		GramEmail:  "",
		GramChatID: chatID,
	}
}

// recordingEfficacySignaler captures the wakes a tool call emits and can be
// told to refuse them.
type recordingEfficacySignaler struct {
	mu      sync.Mutex
	err     error
	signals []uuid.UUID
}

func (s *recordingEfficacySignaler) Signal(_ context.Context, projectID uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.signals = append(s.signals, projectID)
	return s.err
}

func (s *recordingEfficacySignaler) signaled() []uuid.UUID {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]uuid.UUID(nil), s.signals...)
}
