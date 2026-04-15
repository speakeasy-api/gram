package corpus_test

import (
	"context"
	"log"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var infra *testenv.Environment

func TestMain(m *testing.M) {
	res, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{
		Postgres: true,
		Redis:    true,
	})
	if err != nil {
		log.Fatalf("Failed to launch test infrastructure: %v", err)
		os.Exit(1)
	}

	infra = res

	code := m.Run()

	if err := cleanup(); err != nil {
		log.Fatalf("Failed to cleanup test infrastructure: %v", err)
		os.Exit(1)
	}

	os.Exit(code)
}

func cloneDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	conn, err := infra.CloneTestDatabase(t, "corpus_schema")
	require.NoError(t, err)
	return conn
}

func existsQuery(t *testing.T, conn *pgxpool.Pool, query string, args ...any) bool {
	t.Helper()
	var exists bool
	err := conn.QueryRow(t.Context(), query, args...).Scan(&exists)
	require.NoError(t, err)
	return exists
}

func tableExists(t *testing.T, conn *pgxpool.Pool, table string) bool {
	t.Helper()
	return existsQuery(t, conn,
		"SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema = 'public' AND table_name = $1)", table)
}

func assertColumnsExist(t *testing.T, conn *pgxpool.Pool, table string, columns []string) {
	t.Helper()
	for _, col := range columns {
		assert.True(t, existsQuery(t, conn,
			"SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema = 'public' AND table_name = $1 AND column_name = $2)", table, col),
			"%s.%s should exist", table, col)
	}
}

func checkConstraintExists(t *testing.T, conn *pgxpool.Pool, constraintName string) bool {
	t.Helper()
	return existsQuery(t, conn,
		"SELECT EXISTS (SELECT 1 FROM information_schema.table_constraints WHERE constraint_type = 'CHECK' AND constraint_name = $1)", constraintName)
}

func uniqueConstraintExists(t *testing.T, conn *pgxpool.Pool, name string) bool {
	t.Helper()
	return existsQuery(t, conn,
		`SELECT EXISTS (
			SELECT 1 FROM information_schema.table_constraints WHERE constraint_type = 'UNIQUE' AND constraint_name = $1
			UNION
			SELECT 1 FROM pg_indexes WHERE indexname = $1
		)`, name)
}

func indexExists(t *testing.T, conn *pgxpool.Pool, name string) bool {
	t.Helper()
	return existsQuery(t, conn,
		"SELECT EXISTS (SELECT 1 FROM pg_indexes WHERE schemaname = 'public' AND indexname = $1)", name)
}

func TestCorpusTablesExist(t *testing.T) {
	t.Parallel()
	conn := cloneDB(t)

	tables := []string{
		"corpus_drafts",
		"corpus_publish_events",
		"corpus_chunks",
		"corpus_index_state",
		"corpus_feedback",
		"corpus_feedback_comments",
		"corpus_annotations",
		"corpus_auto_publish_configs",
	}

	for _, table := range tables {
		t.Run(table, func(t *testing.T) {
			t.Parallel()
			assert.True(t, tableExists(t, conn, table), "table %s should exist", table)
		})
	}
}

func TestCorpusDraftsSchema(t *testing.T) {
	t.Parallel()
	conn := cloneDB(t)

	assertColumnsExist(t, conn, "corpus_drafts", []string{
		"id", "project_id", "organization_id", "file_path", "title", "original_content",
		"author_user_id", "agent_name", "content", "operation", "status",
		"source", "author_type", "labels", "commit_sha", "created_at", "updated_at", "deleted_at", "deleted",
	})

	assert.True(t, checkConstraintExists(t, conn, "corpus_drafts_status_check"))
	assert.True(t, checkConstraintExists(t, conn, "corpus_drafts_operation_check"))
}

func TestCorpusChunksSchema(t *testing.T) {
	t.Parallel()
	conn := cloneDB(t)

	assertColumnsExist(t, conn, "corpus_chunks", []string{
		"id", "project_id", "organization_id", "chunk_id", "file_path", "heading_path",
		"breadcrumb", "content", "content_text", "content_tsvector",
		"embedding", "metadata", "strategy", "manifest_fingerprint",
		"content_fingerprint", "created_at", "updated_at",
	})

	assert.True(t, uniqueConstraintExists(t, conn, "corpus_chunks_project_id_chunk_id_key"))
}

func TestCorpusPublishEventsSchema(t *testing.T) {
	t.Parallel()
	conn := cloneDB(t)

	assertColumnsExist(t, conn, "corpus_publish_events", []string{
		"id", "project_id", "organization_id", "commit_sha", "status", "created_at", "updated_at",
	})

	assert.True(t, checkConstraintExists(t, conn, "corpus_publish_events_status_check"))
}

func TestCorpusIndexStateSchema(t *testing.T) {
	t.Parallel()
	conn := cloneDB(t)

	assertColumnsExist(t, conn, "corpus_index_state", []string{
		"id", "project_id", "organization_id", "last_indexed_sha", "embedding_model", "created_at", "updated_at",
	})
}

func TestCorpusFeedbackSchema(t *testing.T) {
	t.Parallel()
	conn := cloneDB(t)

	assertColumnsExist(t, conn, "corpus_feedback", []string{
		"id", "project_id", "organization_id", "file_path", "user_id", "direction", "labels",
		"created_at", "updated_at",
	})

	assert.True(t, indexExists(t, conn, "corpus_feedback_project_id_file_path_idx"))
	assert.True(t, indexExists(t, conn, "corpus_feedback_project_id_file_path_user_id_created_at_idx"))
}

func TestCorpusFeedbackCommentsSchema(t *testing.T) {
	t.Parallel()
	conn := cloneDB(t)

	assertColumnsExist(t, conn, "corpus_feedback_comments", []string{
		"id", "project_id", "organization_id", "file_path", "author_id", "author_type", "content",
		"created_at", "updated_at", "deleted_at", "deleted",
	})
}

func TestCorpusAnnotationsSchema(t *testing.T) {
	t.Parallel()
	conn := cloneDB(t)

	assertColumnsExist(t, conn, "corpus_annotations", []string{
		"id", "project_id", "organization_id", "file_path", "author_id", "author_type", "content",
		"line_start", "line_end", "created_at", "updated_at", "deleted_at", "deleted",
	})
}

func TestCorpusAutoPublishConfigsSchema(t *testing.T) {
	t.Parallel()
	conn := cloneDB(t)

	assertColumnsExist(t, conn, "corpus_auto_publish_configs", []string{
		"id", "project_id", "organization_id", "enabled", "interval_minutes",
		"min_upvotes", "author_type_filter", "label_filter", "min_age_hours",
		"created_at", "updated_at",
	})

	assert.True(t, uniqueConstraintExists(t, conn, "corpus_auto_publish_configs_project_id_key"))
}
