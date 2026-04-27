package assistanttokens

import (
	"context"
	"log"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

var tokensInfra *testenv.Environment

func TestMain(m *testing.M) {
	res, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{Postgres: true})
	if err != nil {
		log.Fatalf("launch assistanttokens test infrastructure: %v", err)
	}
	tokensInfra = res

	code := m.Run()

	if err := cleanup(); err != nil {
		log.Fatalf("cleanup assistanttokens test infrastructure: %v", err)
	}
	os.Exit(code)
}

type fixture struct {
	conn        *pgxpool.Pool
	projectID   uuid.UUID
	assistantID uuid.UUID
	threadID    uuid.UUID
	chatID      uuid.UUID
}

func newFixture(t *testing.T, dbName string) fixture {
	t.Helper()

	conn, err := tokensInfra.CloneTestDatabase(t, dbName)
	require.NoError(t, err)

	f := fixture{
		conn:        conn,
		projectID:   uuid.New(),
		assistantID: uuid.New(),
		threadID:    uuid.New(),
		chatID:      uuid.New(),
	}

	_, err = conn.Exec(t.Context(), `
INSERT INTO projects (id, name, slug, organization_id)
VALUES ($1, 'Project', 'project', 'org-test')
`, f.projectID)
	require.NoError(t, err)

	_, err = conn.Exec(t.Context(), `
INSERT INTO assistants (id, project_id, organization_id, name, model, instructions, warm_ttl_seconds, max_concurrency, status)
VALUES ($1, $2, 'org-test', 'Assistant', 'openai/gpt-4o-mini', '', 300, 1, 'active')
`, f.assistantID, f.projectID)
	require.NoError(t, err)

	_, err = conn.Exec(t.Context(), `
INSERT INTO chats (id, project_id, organization_id)
VALUES ($1, $2, 'org-test')
`, f.chatID, f.projectID)
	require.NoError(t, err)

	_, err = conn.Exec(t.Context(), `
INSERT INTO assistant_threads (id, assistant_id, project_id, correlation_id, chat_id, source_kind, source_ref_json, last_event_at)
VALUES ($1, $2, $3, 'corr-1', $4, 'slack', '{}'::jsonb, clock_timestamp())
`, f.threadID, f.assistantID, f.projectID, f.chatID)
	require.NoError(t, err)

	return f
}

func TestCheckRevocation_active(t *testing.T) {
	t.Parallel()

	f := newFixture(t, "tokens_active")
	m := New("test-secret", f.conn, nil)

	require.NoError(t, m.checkRevocation(t.Context(), f.threadID, f.assistantID))
}

func TestCheckRevocation_threadDeleted(t *testing.T) {
	t.Parallel()

	f := newFixture(t, "tokens_thread_deleted")
	_, err := f.conn.Exec(t.Context(), `UPDATE assistant_threads SET deleted_at = clock_timestamp() WHERE id = $1`, f.threadID)
	require.NoError(t, err)

	m := New("test-secret", f.conn, nil)

	err = m.checkRevocation(t.Context(), f.threadID, f.assistantID)
	requireUnauthorized(t, err)
}

func TestCheckRevocation_assistantDeleted(t *testing.T) {
	t.Parallel()

	f := newFixture(t, "tokens_assistant_deleted")
	_, err := f.conn.Exec(t.Context(), `UPDATE assistants SET deleted_at = clock_timestamp() WHERE id = $1`, f.assistantID)
	require.NoError(t, err)

	m := New("test-secret", f.conn, nil)

	err = m.checkRevocation(t.Context(), f.threadID, f.assistantID)
	requireUnauthorized(t, err)
}

func TestCheckRevocation_assistantPaused(t *testing.T) {
	t.Parallel()

	f := newFixture(t, "tokens_assistant_paused")
	_, err := f.conn.Exec(t.Context(), `UPDATE assistants SET status = 'paused' WHERE id = $1`, f.assistantID)
	require.NoError(t, err)

	m := New("test-secret", f.conn, nil)

	err = m.checkRevocation(t.Context(), f.threadID, f.assistantID)
	requireUnauthorized(t, err)
}

func TestCheckRevocation_threadMissing(t *testing.T) {
	t.Parallel()

	f := newFixture(t, "tokens_thread_missing")
	m := New("test-secret", f.conn, nil)

	err := m.checkRevocation(t.Context(), uuid.New(), f.assistantID)
	requireUnauthorized(t, err)
}

func TestCheckRevocation_cacheHitSkipsDB(t *testing.T) {
	t.Parallel()

	f := newFixture(t, "tokens_cache_hit")
	m := New("test-secret", f.conn, nil)

	// Prime cache with the active path.
	require.NoError(t, m.checkRevocation(t.Context(), f.threadID, f.assistantID))

	// Pause the assistant out from under us; the cached "allowed" answer must
	// continue to be honoured until revocationCacheTTL expires.
	_, err := f.conn.Exec(t.Context(), `UPDATE assistants SET status = 'paused' WHERE id = $1`, f.assistantID)
	require.NoError(t, err)

	require.NoError(t, m.checkRevocation(t.Context(), f.threadID, f.assistantID))
}

func TestCheckRevocation_cacheRespectsTTL(t *testing.T) {
	t.Parallel()

	f := newFixture(t, "tokens_cache_ttl")
	m := New("test-secret", f.conn, nil)

	// Force a tiny TTL so we can observe expiry in the test.
	m.revocation = newRevocationCache(50 * time.Millisecond)

	require.NoError(t, m.checkRevocation(t.Context(), f.threadID, f.assistantID))

	_, err := f.conn.Exec(t.Context(), `UPDATE assistants SET deleted_at = clock_timestamp() WHERE id = $1`, f.assistantID)
	require.NoError(t, err)

	time.Sleep(75 * time.Millisecond)

	err = m.checkRevocation(t.Context(), f.threadID, f.assistantID)
	requireUnauthorized(t, err)
}

func requireUnauthorized(t *testing.T, err error) {
	t.Helper()
	require.Error(t, err)
	se := &oops.ShareableError{}
	require.ErrorAs(t, err, &se)
	require.Equal(t, oops.CodeUnauthorized, se.Code)
}
