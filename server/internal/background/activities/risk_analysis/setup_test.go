package risk_analysis_test

import (
	"context"
	"log"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	riskrepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

var infra *testenv.Environment

func TestMain(m *testing.M) {
	res, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{Postgres: true})
	if err != nil {
		log.Fatalf("launch test infrastructure: %v", err)
	}

	infra = res

	code := m.Run()

	if err := cleanup(); err != nil {
		log.Fatalf("cleanup test infrastructure: %v", err)
	}

	os.Exit(code)
}

func cloneDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)
	return conn
}

type testData struct {
	orgID         string
	projectID     uuid.UUID
	policyID      uuid.UUID
	policyVersion int64
	chatID        uuid.UUID
}

func seedTestData(t *testing.T, conn *pgxpool.Pool, enabled bool) testData {
	t.Helper()
	ctx := t.Context()

	orgID := "test-org-" + uuid.NewString()[:8]

	_, err := orgrepo.New(conn).UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{
		ID:          orgID,
		Name:        orgID,
		Slug:        orgID,
		WorkosID:    pgtype.Text{},
		Whitelisted: pgtype.Bool{},
	})
	require.NoError(t, err)

	project, err := projectsrepo.New(conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
		Name:           "test-project",
		Slug:           "test-" + uuid.NewString()[:8],
		OrganizationID: orgID,
	})
	require.NoError(t, err)
	projectID := project.ID

	policyID, err := uuid.NewV7()
	require.NoError(t, err)
	policy, err := riskrepo.New(conn).CreateRiskPolicy(ctx, riskrepo.CreateRiskPolicyParams{
		ID:             policyID,
		ProjectID:      projectID,
		OrganizationID: orgID,
		Name:           "test policy",
		Sources:        []string{"gitleaks"},
		Enabled:        enabled,
	})
	require.NoError(t, err)

	chatID, err := uuid.NewV7()
	require.NoError(t, err)
	_, err = chatrepo.New(conn).UpsertChat(ctx, chatrepo.UpsertChatParams{
		ID:             chatID,
		ProjectID:      projectID,
		OrganizationID: orgID,
		UserID:         pgtype.Text{},
		ExternalUserID: pgtype.Text{},
		Title:          pgtype.Text{String: "test chat", Valid: true},
	})
	require.NoError(t, err)

	return testData{
		orgID:         orgID,
		projectID:     projectID,
		policyID:      policyID,
		policyVersion: policy.Version,
		chatID:        chatID,
	}
}

func seedMessages(t *testing.T, conn *pgxpool.Pool, td testData, count int) []uuid.UUID {
	t.Helper()
	ctx := t.Context()

	chatQueries := chatrepo.New(conn)
	var ids []uuid.UUID
	for range count {
		msgID, err := chatQueries.InsertChatMessage(ctx, chatrepo.InsertChatMessageParams{
			ChatID:    td.chatID,
			ProjectID: uuid.NullUUID{UUID: td.projectID, Valid: true},
			Role:      "user",
			Content:   "test message",
		})
		require.NoError(t, err)
		ids = append(ids, msgID)
	}
	return ids
}
