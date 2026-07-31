package businessmemory

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	pgvector_go "github.com/pgvector/pgvector-go"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/businessmemory/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestContentScopeFiltersApplyBeforeLimits(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	infra, cleanup, err := testenv.Launch(ctx, testenv.LaunchOptions{Postgres: true})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, cleanup()) })
	conn, err := infra.CloneTestDatabase(t, "business_memory_scope_filters")
	require.NoError(t, err)
	organizationID := "org_test"
	_, err = orgrepo.New(conn).UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{
		ID:          organizationID,
		Name:        "Test Organization",
		Slug:        "test-organization",
		WorkosID:    conv.ToPGText(organizationID),
		Whitelisted: pgtype.Bool{},
	})
	require.NoError(t, err)
	project, err := projectsrepo.New(conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
		Name:           "Test Project",
		Slug:           "test-project",
		OrganizationID: organizationID,
	})
	require.NoError(t, err)
	queries := repo.New(conn)

	insertMemory := func(body string, scopes []string, embedding []float32, model string, candidateIndex int32) {
		t.Helper()
		contentScope, err := json.Marshal(scopes)
		require.NoError(t, err)
		require.NoError(t, queries.InsertBusinessMemory(ctx, repo.InsertBusinessMemoryParams{
			ProjectID:            conv.ToNullUUID(project.ID),
			OrganizationID:       organizationID,
			Body:                 body,
			MemoryType:           "result",
			StructuralScope:      "company.test.project.test",
			ContentScope:         contentScope,
			Embedding:            pgvector_go.NewHalfVector(embedding),
			EmbeddingModel:       model,
			ExtractionModel:      extractionModel,
			SourceEvaluationID:   uuid.NullUUID{},
			SourceCandidateIndex: candidateIndex,
			SourceChatID:         uuid.NullUUID{},
			SourceTurn:           pgtype.Int4{},
			SourceAuthorID:       pgtype.Text{},
		}))
	}

	queryEmbedding := make([]float32, embeddingDimensions)
	queryEmbedding[0] = 1
	nearestEmbedding := make([]float32, embeddingDimensions)
	nearestEmbedding[0] = 1
	matchingEmbedding := make([]float32, embeddingDimensions)
	matchingEmbedding[0] = 0.8
	matchingEmbedding[1] = 0.2

	insertMemory("matching", []string{"product:github", "topic:tool-usage"}, matchingEmbedding, embeddingModel, 0)
	insertMemory("unmatched and newer", []string{"product:gitlab"}, nearestEmbedding, embeddingModel, 1)
	insertMemory("stale embedding model", []string{"product:github"}, nearestEmbedding, "stale/model", 2)

	exactRows, err := queries.ListBusinessMemories(ctx, repo.ListBusinessMemoriesParams{
		ProjectID:             conv.ToNullUUID(project.ID),
		OrganizationID:        organizationID,
		ContentScope:          conv.ToPGText("topic:tool-usage"),
		ContentScopeNamespace: pgtype.Text{},
		CursorCreatedAt:       pgtype.Timestamptz{},
		CursorID:              uuid.NullUUID{},
		PageLimit:             1,
	})
	require.NoError(t, err)
	require.Len(t, exactRows, 1)
	require.Equal(t, "matching", exactRows[0].Body)

	namespaceRows, err := queries.SearchBusinessMemories(ctx, repo.SearchBusinessMemoriesParams{
		QueryEmbedding:        pgvector_go.NewHalfVector(queryEmbedding),
		EmbeddingModel:        embeddingModel,
		ProjectID:             conv.ToNullUUID(project.ID),
		OrganizationID:        organizationID,
		ContentScope:          pgtype.Text{},
		ContentScopeNamespace: conv.ToPGText("product"),
		ResultLimit:           1,
	})
	require.NoError(t, err)
	require.Len(t, namespaceRows, 1)
	require.Equal(t, "unmatched and newer", namespaceRows[0].Body)

	exactSearchRows, err := queries.SearchBusinessMemories(ctx, repo.SearchBusinessMemoriesParams{
		QueryEmbedding:        pgvector_go.NewHalfVector(queryEmbedding),
		EmbeddingModel:        embeddingModel,
		ProjectID:             conv.ToNullUUID(project.ID),
		OrganizationID:        organizationID,
		ContentScope:          conv.ToPGText("product:github"),
		ContentScopeNamespace: pgtype.Text{},
		ResultLimit:           1,
	})
	require.NoError(t, err)
	require.Len(t, exactSearchRows, 1)
	require.Equal(t, "matching", exactSearchRows[0].Body)

	combinedSearchRows, err := queries.SearchBusinessMemories(ctx, repo.SearchBusinessMemoriesParams{
		QueryEmbedding:        pgvector_go.NewHalfVector(queryEmbedding),
		EmbeddingModel:        embeddingModel,
		ProjectID:             conv.ToNullUUID(project.ID),
		OrganizationID:        organizationID,
		ContentScope:          conv.ToPGText("topic:tool-usage"),
		ContentScopeNamespace: conv.ToPGText("product"),
		ResultLimit:           10,
	})
	require.NoError(t, err)
	require.Len(t, combinedSearchRows, 1)
	require.Equal(t, "matching", combinedSearchRows[0].Body)

	nearest, err := queries.GetNearestActiveBusinessMemory(ctx, repo.GetNearestActiveBusinessMemoryParams{
		QueryEmbedding:       pgvector_go.NewHalfVector(queryEmbedding),
		EmbeddingModel:       embeddingModel,
		ProjectID:            conv.ToNullUUID(project.ID),
		OrganizationID:       organizationID,
		SourceEvaluationID:   uuid.NullUUID{},
		SourceCandidateIndex: 1,
	})
	require.NoError(t, err)
	require.Less(t, nearest.Similarity, 0.999)
}
