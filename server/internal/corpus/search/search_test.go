package search_test

import (
	"context"
	"encoding/json"
	"log"
	"math"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgvector/pgvector-go"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/corpus/repo"
	"github.com/speakeasy-api/gram/server/internal/corpus/search"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

var infra *testenv.Environment

func TestMain(m *testing.M) {
	res, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{
		Postgres: true,
		Redis:    true,
	})
	if err != nil {
		log.Fatalf("launch test infra: %v", err)
		os.Exit(1)
	}

	infra = res
	code := m.Run()

	if err := cleanup(); err != nil {
		log.Fatalf("cleanup test infra: %v", err)
		os.Exit(1)
	}

	os.Exit(code)
}

type testInstance struct {
	svc       *search.Service
	conn      *pgxpool.Pool
	queries   *repo.Queries
	projectID uuid.UUID
	orgID     string
}

func newTestService(t *testing.T) (context.Context, *testInstance) {
	t.Helper()

	conn, err := infra.CloneTestDatabase(t, "corpus_search")
	require.NoError(t, err)

	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)

	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)
	billingClient := billing.NewStubClient(logger, tracerProvider)
	sessionManager := testenv.NewTestManager(t, logger, conn, redisClient, cache.Suffix("gram-local"), billingClient)

	ctx := testenv.InitAuthContext(t, t.Context(), conn, sessionManager)

	authctx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authctx.ProjectID)

	svc := search.NewService(conn, logger)

	return ctx, &testInstance{
		svc:       svc,
		conn:      conn,
		queries:   repo.New(conn),
		projectID: *authctx.ProjectID,
		orgID:     authctx.ActiveOrganizationID,
	}
}

// seedChunk upserts a chunk with the given parameters.
func (ti *testInstance) seedChunk(t *testing.T, ctx context.Context, chunkID string, filePath string, contentText string, embedding pgvector.Vector, metadata map[string]string) {
	t.Helper()

	var metadataBytes []byte
	if metadata != nil {
		var err error
		metadataBytes, err = json.Marshal(metadata)
		require.NoError(t, err)
	}

	_, err := ti.queries.UpsertChunk(ctx, repo.UpsertChunkParams{
		ProjectID:           ti.projectID,
		OrganizationID:      ti.orgID,
		ChunkID:             chunkID,
		FilePath:            filePath,
		HeadingPath:         pgtype.Text{String: chunkID, Valid: true},
		Breadcrumb:          pgtype.Text{String: chunkID, Valid: true},
		Content:             "# " + chunkID + "\n\n" + contentText,
		ContentText:         contentText,
		Embedding:           embedding,
		Metadata:            metadataBytes,
		Strategy:            pgtype.Text{String: "h2", Valid: true},
		ManifestFingerprint: pgtype.Text{String: "test", Valid: true},
		ContentFingerprint:  "fp-" + chunkID,
	})
	require.NoError(t, err)
}

// makeEmbedding creates a 3072-dim vector with the given value at position 0
// and zeros elsewhere, then normalizes.
func makeEmbedding(val float32) pgvector.Vector {
	dims := 3072
	vec := make([]float32, dims)
	vec[0] = val
	// Normalize for cosine similarity.
	mag := float32(math.Sqrt(float64(val * val)))
	if mag > 0 {
		for i := range vec {
			vec[i] /= mag
		}
	}
	return pgvector.NewVector(vec)
}

// makeEmbeddingAt creates a normalized 3072-dim vector with a 1.0 at the given index.
func makeEmbeddingAt(idx int) pgvector.Vector {
	dims := 3072
	vec := make([]float32, dims)
	vec[idx] = 1.0
	return pgvector.NewVector(vec)
}

func TestSearch_FTSOnly(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	// Seed chunks with different content. No embeddings (zero vectors).
	ti.seedChunk(t, ctx, "chunk-go", "docs/go.md", "Go is a statically typed compiled programming language designed at Google", pgvector.NewVector(make([]float32, 3072)), nil)
	ti.seedChunk(t, ctx, "chunk-rust", "docs/rust.md", "Rust is a systems programming language focused on safety and performance", pgvector.NewVector(make([]float32, 3072)), nil)
	ti.seedChunk(t, ctx, "chunk-python", "docs/python.md", "Python is an interpreted high-level general-purpose programming language", pgvector.NewVector(make([]float32, 3072)), nil)

	resp, err := ti.svc.Search(ctx, search.SearchParams{
		ProjectID: ti.projectID.String(),
		Query:     "compiled language Google",
		Limit:     10,
		FTSWeight: 1.0,
		VecWeight: 0.0,
	})
	require.NoError(t, err)
	require.NotEmpty(t, resp.Results)

	// "Go" chunk should be the top result since it mentions both "compiled" and "Google".
	require.Equal(t, "chunk-go", resp.Results[0].ChunkID)
}

func TestSearch_VectorOnly(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	// Create embeddings with known similarity.
	// queryEmb is at index 0, similar chunk also at index 0, others at different indices.
	queryEmb := makeEmbeddingAt(0)
	similarEmb := makeEmbeddingAt(0)    // cosine distance = 0 (identical)
	differentEmb1 := makeEmbeddingAt(1) // cosine distance = 1 (orthogonal)
	differentEmb2 := makeEmbeddingAt(2) // cosine distance = 1 (orthogonal)

	ti.seedChunk(t, ctx, "chunk-similar", "docs/similar.md", "similar content here", similarEmb, nil)
	ti.seedChunk(t, ctx, "chunk-different1", "docs/diff1.md", "unrelated text one", differentEmb1, nil)
	ti.seedChunk(t, ctx, "chunk-different2", "docs/diff2.md", "unrelated text two", differentEmb2, nil)

	resp, err := ti.svc.Search(ctx, search.SearchParams{
		ProjectID: ti.projectID.String(),
		Embedding: queryEmb,
		Limit:     10,
		FTSWeight: 0.0,
		VecWeight: 1.0,
	})
	require.NoError(t, err)
	require.NotEmpty(t, resp.Results)

	// The similar embedding should rank first.
	require.Equal(t, "chunk-similar", resp.Results[0].ChunkID)
}

func TestSearch_HybridRRF(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	// Chunk A: good FTS match + good vector match → should rank highest
	// Chunk B: good FTS match only
	// Chunk C: good vector match only
	embA := makeEmbeddingAt(0)
	embB := makeEmbeddingAt(1)
	embC := makeEmbeddingAt(0) // same direction as query
	queryEmb := makeEmbeddingAt(0)

	ti.seedChunk(t, ctx, "chunk-a", "docs/a.md", "kubernetes container orchestration platform for deploying applications", embA, nil)
	ti.seedChunk(t, ctx, "chunk-b", "docs/b.md", "kubernetes cluster management and container scheduling", embB, nil)
	ti.seedChunk(t, ctx, "chunk-c", "docs/c.md", "unrelated content about cooking recipes", embC, nil)

	resp, err := ti.svc.Search(ctx, search.SearchParams{
		ProjectID: ti.projectID.String(),
		Query:     "kubernetes container orchestration",
		Embedding: queryEmb,
		Limit:     10,
		FTSWeight: 1.0,
		VecWeight: 1.0,
	})
	require.NoError(t, err)
	require.NotEmpty(t, resp.Results)

	// Chunk A appears in both FTS and vector lists → RRF should rank it highest.
	require.Equal(t, "chunk-a", resp.Results[0].ChunkID)
}

func TestSearch_MetadataFilter(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	ti.seedChunk(t, ctx, "chunk-eng", "docs/eng.md", "engineering practices and code review processes", makeEmbeddingAt(0), map[string]string{"department": "engineering"})
	ti.seedChunk(t, ctx, "chunk-mkt", "docs/mkt.md", "engineering marketing campaigns for product launches", makeEmbeddingAt(1), map[string]string{"department": "marketing"})
	ti.seedChunk(t, ctx, "chunk-eng2", "docs/eng2.md", "engineering architecture design documents", makeEmbeddingAt(2), map[string]string{"department": "engineering"})

	resp, err := ti.svc.Search(ctx, search.SearchParams{
		ProjectID: ti.projectID.String(),
		Query:     "engineering",
		Metadata:  map[string]string{"department": "engineering"},
		Limit:     10,
		FTSWeight: 1.0,
		VecWeight: 0.0,
	})
	require.NoError(t, err)
	require.Len(t, resp.Results, 2)

	// Only engineering department chunks should be returned.
	for _, r := range resp.Results {
		require.Contains(t, []string{"chunk-eng", "chunk-eng2"}, r.ChunkID)
	}
}

func TestSearch_PhraseProximity(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	// Chunk with terms close together should rank higher than chunk with terms far apart.
	ti.seedChunk(t, ctx, "chunk-close", "docs/close.md", "the quick brown fox jumped over the lazy dog near the riverbank", makeEmbeddingAt(0), nil)
	ti.seedChunk(t, ctx, "chunk-far", "docs/far.md", "the quick rabbit ran through the forest and many hours later encountered a brown colored fox in the meadow", makeEmbeddingAt(1), nil)

	resp, err := ti.svc.Search(ctx, search.SearchParams{
		ProjectID: ti.projectID.String(),
		Query:     "quick brown fox",
		Limit:     10,
		FTSWeight: 1.0,
		VecWeight: 0.0,
	})
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(resp.Results), 2)

	// Close-proximity chunk should rank higher.
	require.Equal(t, "chunk-close", resp.Results[0].ChunkID)
}

func TestSearch_Pagination(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	// Seed 20 chunks.
	for i := range 20 {
		chunkID := "chunk-" + string(rune('a'+i))
		if i >= 26 {
			chunkID = "chunk-z" + string(rune('a'+i-26))
		}
		ti.seedChunk(t, ctx, chunkID, "docs/page.md", "programming language concepts and software engineering "+chunkID, makeEmbeddingAt(i%3072), nil)
	}

	// Page 1: limit=5.
	resp1, err := ti.svc.Search(ctx, search.SearchParams{
		ProjectID: ti.projectID.String(),
		Query:     "programming language",
		Limit:     5,
		FTSWeight: 1.0,
		VecWeight: 0.0,
	})
	require.NoError(t, err)
	require.Len(t, resp1.Results, 5)
	require.NotEmpty(t, resp1.NextCursor)

	// Page 2: use cursor.
	resp2, err := ti.svc.Search(ctx, search.SearchParams{
		ProjectID: ti.projectID.String(),
		Query:     "programming language",
		Limit:     5,
		Cursor:    resp1.NextCursor,
		FTSWeight: 1.0,
		VecWeight: 0.0,
	})
	require.NoError(t, err)
	require.Len(t, resp2.Results, 5)

	// Ensure no overlap between pages.
	page1IDs := make(map[string]bool, len(resp1.Results))
	for _, r := range resp1.Results {
		page1IDs[r.ChunkID] = true
	}
	for _, r := range resp2.Results {
		require.False(t, page1IDs[r.ChunkID], "page 2 should not contain page 1 results")
	}
}

func TestGetChunk(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	// Seed three chunks in the same file to test neighbor detection.
	ti.seedChunk(t, ctx, "file1#section-a", "docs/file1.md", "Section A content", makeEmbeddingAt(0), nil)
	ti.seedChunk(t, ctx, "file1#section-b", "docs/file1.md", "Section B content", makeEmbeddingAt(1), nil)
	ti.seedChunk(t, ctx, "file1#section-c", "docs/file1.md", "Section C content", makeEmbeddingAt(2), nil)

	chunk, err := ti.svc.GetChunk(ctx, ti.projectID.String(), "file1#section-b")
	require.NoError(t, err)
	require.Equal(t, "file1#section-b", chunk.ChunkID)
	require.Equal(t, "docs/file1.md", chunk.FilePath)
	require.Contains(t, chunk.ContentText, "Section B content")

	// Should have neighbors.
	require.NotNil(t, chunk.Prev, "should have a previous neighbor")
	require.Equal(t, "file1#section-a", chunk.Prev.ChunkID)
	require.NotNil(t, chunk.Next, "should have a next neighbor")
	require.Equal(t, "file1#section-c", chunk.Next.ChunkID)
}
