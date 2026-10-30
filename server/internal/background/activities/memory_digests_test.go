package activities_test

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/background/activities"
	"github.com/speakeasy-api/gram/server/internal/billing"
	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/memorydigest/repo"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

// digestCompletionStub answers every digest request with a fixed text and
// records each user prompt it saw.
type digestCompletionStub struct {
	mu      sync.Mutex
	prompts []string
}

func (s *digestCompletionStub) GetCompletion(_ context.Context, req openrouter.CompletionRequest) (*openrouter.CompletionResponse, error) {
	s.mu.Lock()
	s.prompts = append(s.prompts, openrouter.GetText(req.Messages[len(req.Messages)-1]))
	s.mu.Unlock()
	message := openrouter.CreateMessageAssistant("digest text")
	return &openrouter.CompletionResponse{
		StartTime: time.Now(), Message: &message, MessageID: "", Model: req.Model, Provider: "",
		Usage: openrouter.Usage{PromptTokens: 100, CompletionTokens: 20}, FinishReason: nil, ToolCalls: nil, Content: "digest text", Annotations: nil,
	}, nil
}

func (s *digestCompletionStub) GetCompletionStream(context.Context, openrouter.CompletionRequest) (openrouter.StreamReader, error) {
	return nil, fmt.Errorf("unexpected streaming completion")
}

func (s *digestCompletionStub) GetObjectCompletion(context.Context, openrouter.ObjectCompletionRequest) (*openrouter.CompletionResponse, error) {
	return nil, fmt.Errorf("unexpected object completion")
}

func (s *digestCompletionStub) CreateEmbeddings(context.Context, string, string, []string, ...openrouter.EmbeddingOption) ([][]float32, error) {
	return nil, fmt.Errorf("unexpected embeddings request")
}

func (s *digestCompletionStub) ResolveKey(context.Context, string, string, billing.ModelUsageSource, openrouter.KeyType) (openrouter.ResolvedKey, error) {
	return openrouter.PlatformKey(), nil
}

func (s *digestCompletionStub) allPrompts() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return strings.Join(s.prompts, "\n")
}

type digestProject struct {
	orgID     string
	projectID uuid.UUID
}

func newDigestProject(t *testing.T, conn *pgxpool.Pool) digestProject {
	t.Helper()

	ctx := t.Context()
	orgID := "org-" + uuid.NewString()[:8]
	_, err := orgrepo.New(conn).UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{
		ID: orgID, Name: "Test Org", Slug: orgID, WorkosID: pgtype.Text{}, Whitelisted: pgtype.Bool{},
	})
	require.NoError(t, err)
	project, err := projectsrepo.New(conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
		Name: "Test Project", Slug: "proj-" + uuid.NewString()[:8], OrganizationID: orgID,
	})
	require.NoError(t, err)
	return digestProject{orgID: orgID, projectID: project.ID}
}

// addChat seeds a chat whose summary was written at summarizedAt.
func (p digestProject) addChat(t *testing.T, conn *pgxpool.Pool, summary string, summarizedAt time.Time, redacted bool) uuid.UUID {
	t.Helper()

	chatID, err := chatrepo.New(conn).UpsertChat(t.Context(), chatrepo.UpsertChatParams{
		ID: uuid.New(), ProjectID: p.projectID, OrganizationID: p.orgID,
		Title: pgtype.Text{String: "title " + summary, Valid: true},
	})
	require.NoError(t, err)
	_, err = conn.Exec(t.Context(),
		`UPDATE chats SET summary = $1, summary_generated_at = $2, summary_redacted = $3 WHERE id = $4`,
		summary, summarizedAt, redacted, chatID)
	require.NoError(t, err)
	return chatID
}

func (p digestProject) digests(t *testing.T, conn *pgxpool.Pool, level string) []repo.ListChildDigestsRow {
	t.Helper()

	rows, err := repo.New(conn).ListChildDigests(t.Context(), repo.ListChildDigestsParams{
		ProjectID: p.projectID, OrganizationID: p.orgID, Level: level,
		Since: pgtype.Date{Time: time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC), Valid: true},
		Until: pgtype.Date{Time: time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC), Valid: true},
	})
	require.NoError(t, err)
	return rows
}

func digestDay(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

func TestMemoryDigests_DayDigestFoldsOnlyEligibleSources(t *testing.T) {
	t.Parallel()

	conn, err := infra.CloneTestDatabase(t, "memory_digests_day")
	require.NoError(t, err)
	ctx := t.Context()

	enabled := newDigestProject(t, conn)
	disabled := newDigestProject(t, conn)
	flags := new(feature.InMemory)
	flags.SetFlag(feature.FlagTeamMemory, enabled.orgID, true)

	day := digestDay(2026, 10, 27)
	at := day.Add(10 * time.Hour)
	kept1 := enabled.addChat(t, conn, "picked queue X", at, true)
	kept2 := enabled.addChat(t, conn, "shipped retries", at.Add(time.Hour), true)
	enabled.addChat(t, conn, "unmasked summary", at, false)
	excluded := enabled.addChat(t, conn, "excluded summary", at, true)
	_, err = conn.Exec(ctx, `UPDATE chats SET memory_excluded_at = now() WHERE id = $1`, excluded)
	require.NoError(t, err)
	personal := enabled.addChat(t, conn, "personal summary", at, true)
	_, err = conn.Exec(ctx, `
		WITH ua AS (
			INSERT INTO user_accounts (organization_id, external_account_uuid, account_type)
			VALUES ($1, $2, 'personal') RETURNING id
		)
		UPDATE chats SET user_account_id = (SELECT id FROM ua) WHERE id = $3`,
		enabled.orgID, uuid.NewString(), personal)
	require.NoError(t, err)
	// Written after the run's anchor day closed: belongs to an open day.
	enabled.addChat(t, conn, "today summary", day.AddDate(0, 0, 2).Add(30*time.Minute), true)
	disabled.addChat(t, conn, "other org summary", at, true)

	var memoID uuid.UUID
	err = conn.QueryRow(ctx, `
		INSERT INTO memory_memos (organization_id, project_id, content, created_at)
		VALUES ($1, $2, 'queue X because of ordering', $3) RETURNING id`,
		enabled.orgID, enabled.projectID, at).Scan(&memoID)
	require.NoError(t, err)

	stub := &digestCompletionStub{}
	job := activities.NewMemoryDigests(slog.New(slog.DiscardHandler), conn, stub, flags)
	now := day.AddDate(0, 0, 2).Add(time.Hour)

	result, err := job.Build(ctx, activities.BuildMemoryDigestsArgs{Level: "day", Now: now})
	require.NoError(t, err)
	require.Equal(t, 1, result.Built)
	require.Zero(t, result.Failed)
	require.False(t, result.More)

	prompts := stub.allPrompts()
	require.Contains(t, prompts, "picked queue X")
	require.Contains(t, prompts, "shipped retries")
	require.Contains(t, prompts, "queue X because of ordering")
	// Titles are not masked at rest, so they never reach the model.
	require.NotContains(t, prompts, "title ")
	for _, leaked := range []string{"unmasked summary", "excluded summary", "personal summary", "today summary", "other org summary"} {
		require.NotContains(t, prompts, leaked)
	}

	var chatIDs, memoIDs []uuid.UUID
	err = conn.QueryRow(ctx, `
		SELECT chat_ids, memo_ids FROM memory_digests
		WHERE project_id = $1 AND level = 'day' AND period_start = $2`,
		enabled.projectID, day).Scan(&chatIDs, &memoIDs)
	require.NoError(t, err)
	require.ElementsMatch(t, []uuid.UUID{kept1, kept2}, chatIDs)
	require.Equal(t, []uuid.UUID{memoID}, memoIDs)
	require.Empty(t, disabled.digests(t, conn, "day"))

	// Digests are written once: a second run finds nothing to do.
	result, err = job.Build(ctx, activities.BuildMemoryDigestsArgs{Level: "day", Now: now})
	require.NoError(t, err)
	require.Zero(t, result.Built)
	require.Len(t, enabled.digests(t, conn, "day"), 1)
}

func TestMemoryDigests_RollupsWaitForClosedPeriods(t *testing.T) {
	t.Parallel()

	conn, err := infra.CloneTestDatabase(t, "memory_digests_rollup")
	require.NoError(t, err)
	ctx := t.Context()

	p := newDigestProject(t, conn)
	flags := new(feature.InMemory)
	flags.SetFlag(feature.FlagTeamMemory, p.orgID, true)
	// The org needs memory activity in the lookback to be considered.
	p.addChat(t, conn, "recent", digestDay(2026, 10, 27), true)

	q := repo.New(conn)
	var dayIDs []uuid.UUID
	for _, d := range []time.Time{digestDay(2026, 10, 26), digestDay(2026, 10, 28)} {
		id, err := q.InsertDigest(ctx, repo.InsertDigestParams{
			OrganizationID: p.orgID, ProjectID: p.projectID, Level: "day",
			PeriodStart: pgtype.Date{Time: d, Valid: true}, PeriodEnd: pgtype.Date{Time: d.AddDate(0, 0, 1), Valid: true},
			Content: "day " + d.Format(time.DateOnly), ChatIds: []uuid.UUID{}, MemoIds: []uuid.UUID{}, ChildDigestIds: []uuid.UUID{},
		})
		require.NoError(t, err)
		dayIDs = append(dayIDs, id)
	}

	stub := &digestCompletionStub{}
	job := activities.NewMemoryDigests(slog.New(slog.DiscardHandler), conn, stub, flags)

	// Sunday 1 November: the week of 26 October is still open.
	result, err := job.Build(ctx, activities.BuildMemoryDigestsArgs{Level: "week", Now: time.Date(2026, 11, 1, 23, 0, 0, 0, time.UTC)})
	require.NoError(t, err)
	require.Zero(t, result.Built)

	result, err = job.Build(ctx, activities.BuildMemoryDigestsArgs{Level: "week", Now: time.Date(2026, 11, 2, 1, 0, 0, 0, time.UTC)})
	require.NoError(t, err)
	require.Equal(t, 1, result.Built)
	require.Contains(t, stub.allPrompts(), "day 2026-10-26")
	require.Contains(t, stub.allPrompts(), "day 2026-10-28")

	var children []uuid.UUID
	err = conn.QueryRow(ctx, `SELECT child_digest_ids FROM memory_digests WHERE project_id = $1 AND level = 'week'`, p.projectID).Scan(&children)
	require.NoError(t, err)
	require.Equal(t, dayIDs, children)

	// October's last week closed on 2 November, so October is ready then.
	result, err = job.Build(ctx, activities.BuildMemoryDigestsArgs{Level: "month", Now: time.Date(2026, 11, 2, 1, 0, 0, 0, time.UTC)})
	require.NoError(t, err)
	require.Equal(t, 1, result.Built)
	months := p.digests(t, conn, "month")
	require.Len(t, months, 1)
	require.Equal(t, digestDay(2026, 10, 1), months[0].PeriodStart.Time)
}
