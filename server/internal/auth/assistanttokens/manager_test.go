package assistanttokens

import (
	"context"
	"log"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	assistantsrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
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

	ctx := t.Context()

	proj, err := projectsrepo.New(conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
		Name:           "Project",
		Slug:           "project",
		OrganizationID: "org-test",
	})
	require.NoError(t, err)

	assistant, err := assistantsrepo.New(conn).CreateAssistant(ctx, assistantsrepo.CreateAssistantParams{
		ProjectID:       proj.ID,
		OrganizationID:  "org-test",
		CreatedByUserID: pgtype.Text{},
		Name:            "Assistant",
		Model:           "openai/gpt-4o-mini",
		Instructions:    "",
		WarmTtlSeconds:  300,
		MaxConcurrency:  1,
		Status:          "active",
	})
	require.NoError(t, err)

	chatID := uuid.New()
	err = assistantsrepo.New(conn).UpsertAssistantChat(ctx, assistantsrepo.UpsertAssistantChatParams{
		ChatID:         chatID,
		ProjectID:      proj.ID,
		OrganizationID: "org-test",
		Title:          pgtype.Text{},
	})
	require.NoError(t, err)

	threadID, err := assistantsrepo.New(conn).UpsertAssistantThread(ctx, assistantsrepo.UpsertAssistantThreadParams{
		AssistantID:   assistant.ID,
		ProjectID:     proj.ID,
		CorrelationID: "corr-1",
		ChatID:        chatID,
		SourceKind:    "slack",
		SourceRefJson: []byte("{}"),
	})
	require.NoError(t, err)

	return fixture{
		conn:        conn,
		projectID:   proj.ID,
		assistantID: assistant.ID,
		threadID:    threadID,
		chatID:      chatID,
	}
}

func TestCheckRevocation_active(t *testing.T) {
	t.Parallel()

	f := newFixture(t, "tokens_active")
	m := New("test-secret", f.conn, nil)

	require.NoError(t, m.checkRevocation(t.Context(), f.projectID, f.assistantID, f.threadID))
}

func TestCheckRevocation_threadDeleted(t *testing.T) {
	t.Parallel()

	f := newFixture(t, "tokens_thread_deleted")
	err := assistantsrepo.New(f.conn).SoftDeleteAssistantThread(t.Context(), assistantsrepo.SoftDeleteAssistantThreadParams{
		ID:        f.threadID,
		ProjectID: f.projectID,
	})
	require.NoError(t, err)

	m := New("test-secret", f.conn, nil)

	err = m.checkRevocation(t.Context(), f.projectID, f.assistantID, f.threadID)
	requireUnauthorized(t, err)
}

func TestCheckRevocation_assistantDeleted(t *testing.T) {
	t.Parallel()

	f := newFixture(t, "tokens_assistant_deleted")
	err := assistantsrepo.New(f.conn).DeleteAssistant(t.Context(), assistantsrepo.DeleteAssistantParams{
		AssistantID: f.assistantID,
		ProjectID:   f.projectID,
	})
	require.NoError(t, err)

	m := New("test-secret", f.conn, nil)

	err = m.checkRevocation(t.Context(), f.projectID, f.assistantID, f.threadID)
	requireUnauthorized(t, err)
}

func TestCheckRevocation_assistantPaused(t *testing.T) {
	t.Parallel()

	f := newFixture(t, "tokens_assistant_paused")
	err := assistantsrepo.New(f.conn).SetAssistantStatus(t.Context(), assistantsrepo.SetAssistantStatusParams{
		Status:    "paused",
		ID:        f.assistantID,
		ProjectID: f.projectID,
	})
	require.NoError(t, err)

	m := New("test-secret", f.conn, nil)

	err = m.checkRevocation(t.Context(), f.projectID, f.assistantID, f.threadID)
	requireUnauthorized(t, err)
}

func TestCheckRevocation_threadMissing(t *testing.T) {
	t.Parallel()

	f := newFixture(t, "tokens_thread_missing")
	m := New("test-secret", f.conn, nil)

	err := m.checkRevocation(t.Context(), f.projectID, f.assistantID, uuid.New())
	requireUnauthorized(t, err)
}

func TestCheckRevocation_assistantScoped(t *testing.T) {
	t.Parallel()

	// v2 tokens omit ThreadID — revocation must fall back to an
	// assistant-only lookup and pass when the assistant is active.
	f := newFixture(t, "tokens_assistant_scoped")
	m := New("test-secret", f.conn, nil)

	require.NoError(t, m.checkRevocation(t.Context(), f.projectID, f.assistantID, uuid.Nil))
}

func TestCheckRevocation_assistantScoped_assistantPaused(t *testing.T) {
	t.Parallel()

	f := newFixture(t, "tokens_assistant_scoped_paused")
	err := assistantsrepo.New(f.conn).SetAssistantStatus(t.Context(), assistantsrepo.SetAssistantStatusParams{
		Status:    "paused",
		ID:        f.assistantID,
		ProjectID: f.projectID,
	})
	require.NoError(t, err)

	m := New("test-secret", f.conn, nil)

	err = m.checkRevocation(t.Context(), f.projectID, f.assistantID, uuid.Nil)
	requireUnauthorized(t, err)
}

func TestGenerate_assistantScopedOmitsThreadClaim(t *testing.T) {
	t.Parallel()

	f := newFixture(t, "tokens_generate_v2")
	m := New("test-secret", f.conn, nil)

	token, err := m.Generate(GenerateInput{
		OrgID:       "org",
		ProjectID:   f.projectID,
		UserID:      "user",
		AssistantID: f.assistantID,
		ThreadID:    uuid.Nil,
		TTL:         time.Minute,
	})
	require.NoError(t, err)
	claims, err := m.Validate(token)
	require.NoError(t, err)
	require.Empty(t, claims.ThreadID, "v2 tokens must omit the ThreadID claim")
}

func TestCheckRevocation_cacheHitSkipsDB(t *testing.T) {
	t.Parallel()

	f := newFixture(t, "tokens_cache_hit")
	m := New("test-secret", f.conn, nil)

	// Prime cache with the active path.
	require.NoError(t, m.checkRevocation(t.Context(), f.projectID, f.assistantID, f.threadID))

	// Pause the assistant out from under us; the cached "allowed" answer must
	// continue to be honoured until revocationCacheTTL expires.
	err := assistantsrepo.New(f.conn).SetAssistantStatus(t.Context(), assistantsrepo.SetAssistantStatusParams{
		Status:    "paused",
		ID:        f.assistantID,
		ProjectID: f.projectID,
	})
	require.NoError(t, err)

	require.NoError(t, m.checkRevocation(t.Context(), f.projectID, f.assistantID, f.threadID))
}

func TestCheckRevocation_cacheRespectsTTL(t *testing.T) {
	t.Parallel()

	f := newFixture(t, "tokens_cache_ttl")
	m := New("test-secret", f.conn, nil)

	// Force a tiny TTL so we can observe expiry in the test.
	m.revocation = newRevocationCache(50 * time.Millisecond)

	require.NoError(t, m.checkRevocation(t.Context(), f.projectID, f.assistantID, f.threadID))

	err := assistantsrepo.New(f.conn).DeleteAssistant(t.Context(), assistantsrepo.DeleteAssistantParams{
		AssistantID: f.assistantID,
		ProjectID:   f.projectID,
	})
	require.NoError(t, err)

	time.Sleep(75 * time.Millisecond)

	err = m.checkRevocation(t.Context(), f.projectID, f.assistantID, f.threadID)
	requireUnauthorized(t, err)
}

func requireUnauthorized(t *testing.T, err error) {
	t.Helper()
	require.Error(t, err)
	se := &oops.ShareableError{}
	require.ErrorAs(t, err, &se)
	require.Equal(t, oops.CodeUnauthorized, se.Code)
}
