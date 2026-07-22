package efficacy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/chat"
	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	hooksrepo "github.com/speakeasy-api/gram/server/internal/hooks/repo"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/skills"
	"github.com/speakeasy-api/gram/server/internal/skills/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

var infra *testenv.Environment

func TestMain(m *testing.M) {
	res, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{Postgres: true, ClickHouse: true, Redis: false})
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

const skillContent = "---\nname: efficacy-skill\ndescription: Efficacy skill.\n---\n\nBody\n"

type efficacyFixture struct {
	db             *pgxpool.Pool
	organizationID string
	projectID      uuid.UUID
	skillVersionID uuid.UUID
}

func newEfficacyFixture(t *testing.T, name string) efficacyFixture {
	t.Helper()
	ctx := t.Context()

	db, err := infra.CloneTestDatabase(t, name)
	require.NoError(t, err)

	organizationID := name + "-" + uuid.NewString()
	_, err = orgrepo.New(db).UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{
		ID:          organizationID,
		Name:        organizationID,
		Slug:        organizationID,
		WorkosID:    pgtype.Text{String: "", Valid: false},
		Whitelisted: pgtype.Bool{Bool: false, Valid: false},
	})
	require.NoError(t, err)

	project, err := projectsrepo.New(db).CreateProject(ctx, projectsrepo.CreateProjectParams{
		Name:           name,
		Slug:           "efficacy-" + uuid.NewString()[:8],
		OrganizationID: organizationID,
	})
	require.NoError(t, err)

	captured, err := skills.CaptureSkillContent(ctx, db, project.ID, skillContent)
	require.NoError(t, err)

	return efficacyFixture{
		db:             db,
		organizationID: organizationID,
		projectID:      project.ID,
		skillVersionID: captured.SkillVersionID,
	}
}

// actor is the human a session's chat and its activations are attributed to.
// The capture paths write the id to chats.user_id and the email to
// chats.external_user_id, and the zero value is a session captured with no
// identity at all — which is what an assistant activation carries.
type actor struct {
	userID string
	email  string
}

var defaultActor = actor{userID: "user", email: "user@example.test"}

// seedChat creates the chat a session's activations belong to exactly the way
// the hook capture paths do: the chat id is the session id mapped through
// chat.SessionIDToChatID and external_chat_id is never set. lastMessageAge is
// how long ago the newest message was written; passing zero messages leaves the
// chat empty.
func (f efficacyFixture) seedChat(t *testing.T, sessionID string, messages int, lastMessageAge time.Duration) uuid.UUID {
	t.Helper()

	return f.seedChatFor(t, sessionID, defaultActor, messages, lastMessageAge)
}

// seedChatFor is seedChat for a named owner, which is what a test needs when
// more than one actor is in play.
func (f efficacyFixture) seedChatFor(t *testing.T, sessionID string, owner actor, messages int, lastMessageAge time.Duration) uuid.UUID {
	t.Helper()
	ctx := t.Context()

	chatID := chat.SessionIDToChatID(sessionID)

	createdAt := conv.ToPGTimestamptz(time.Now().UTC().Add(-24 * time.Hour))
	queries := chatrepo.New(f.db)
	_, err := queries.UpsertExternalChat(ctx, chatrepo.UpsertExternalChatParams{
		ID:             chatID,
		ProjectID:      f.projectID,
		OrganizationID: f.organizationID,
		UserID:         conv.ToPGTextEmpty(owner.userID),
		ExternalUserID: conv.ToPGTextEmpty(owner.email),
		ExternalChatID: pgtype.Text{String: "", Valid: false},
		Title:          conv.ToPGText("chat"),
		CreatedAt:      createdAt,
		UpdatedAt:      createdAt,
	})
	require.NoError(t, err)

	for i := range messages {
		age := lastMessageAge + time.Duration(messages-1-i)*time.Minute
		_, err := queries.SeedChatMessage(ctx, chatrepo.SeedChatMessageParams{
			ChatID:    chatID,
			ProjectID: uuid.NullUUID{UUID: f.projectID, Valid: true},
			CreatedAt: conv.ToPGTimestamptz(time.Now().UTC().Add(-age)),
		})
		require.NoError(t, err)
	}

	return chatID
}

// observe records one activation of the fixture skill and reconciles it, which
// is what makes it an efficacy candidate.
func (f efficacyFixture) observe(t *testing.T, sessionID string, provider string, seenAt time.Time) {
	t.Helper()

	f.observeAs(t, sessionID, provider, defaultActor, seenAt)
}

// observeAs is observe attributed to a named actor. A zero actor is an
// activation captured with no identity, which is what the assistant path
// records.
func (f efficacyFixture) observeAs(t *testing.T, sessionID string, provider string, by actor, seenAt time.Time) {
	t.Helper()
	ctx := t.Context()

	rawHash := sha256.Sum256([]byte(skillContent))
	_, err := hooksrepo.New(f.db).InsertSkillObservation(ctx, hooksrepo.InsertSkillObservationParams{
		ProjectID:      f.projectID,
		IdempotencyKey: conv.ToPGText(uuid.NewString()),
		Provider:       provider,
		UserID:         conv.ToPGTextEmpty(by.userID),
		UserEmail:      conv.ToPGTextEmpty(by.email),
		Hostname:       pgtype.Text{String: "", Valid: false},
		SessionID:      conv.ToPGText(sessionID),
		SkillName:      "efficacy-skill",
		Source:         pgtype.Text{String: "", Valid: false},
		SourceLevel:    conv.ToPGText("project"),
		SourcePath:     pgtype.Text{String: "", Valid: false},
		RawSha256:      conv.ToPGText(hex.EncodeToString(rawHash[:])),
		SeenAt:         conv.ToPGTimestamptz(seenAt),
	})
	require.NoError(t, err)

	_, err = skills.ReconcileSkillObservations(ctx, f.db, f.projectID, 50)
	require.NoError(t, err)
}

// pendingEvaluations reads the whole queue by walking the keyset pages to
// exhaustion, so a test measures every pending row rather than one page of them.
func (f efficacyFixture) pendingEvaluations(t *testing.T) []repo.SkillEfficacyEvaluation {
	t.Helper()

	queries := repo.New(f.db)
	page := repo.ListPendingSkillEfficacyEvaluationsParams{
		ProjectID:        f.projectID,
		CursorObservedAt: pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false},
		CursorID:         uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		PageSize:         PendingCandidatePage,
		Inactivity:       pgtype.Interval{Microseconds: InactivityWindow.Microseconds(), Days: 0, Months: 0, Valid: true},
	}

	var rows []repo.SkillEfficacyEvaluation
	for {
		batch, err := queries.ListPendingSkillEfficacyEvaluations(t.Context(), page)
		require.NoError(t, err)
		rows = append(rows, batch...)
		if len(batch) < int(page.PageSize) {
			return rows
		}

		last := batch[len(batch)-1]
		page.CursorObservedAt = last.ObservedAt
		page.CursorID = uuid.NullUUID{UUID: last.ID, Valid: true}
	}
}

// enqueueCounts is the part of a page result that does not depend on which
// activation the page happened to stop on.
type enqueueCounts struct {
	Scanned   int
	Units     int
	Confirmed int
	Stamped   int
}

func countsOf(result EnqueuePageResult) enqueueCounts {
	return enqueueCounts{
		Scanned:   result.Scanned,
		Units:     result.Units,
		Confirmed: result.Confirmed,
		Stamped:   result.Stamped,
	}
}

// firstPage runs the one page a walk starts with.
func (f efficacyFixture) firstPage(t *testing.T, pageSize int32) EnqueuePageResult {
	t.Helper()

	result, err := EnqueuePage(t.Context(), f.db, &stubFeatures{enabled: true}, f.projectID, EnqueueCursor{}, pageSize)
	require.NoError(t, err)
	require.LessOrEqual(t, result.Scanned, int(pageSize), "a page never scans past its size")

	return result
}

// drain chains the returned cursor the way a coordinator does, summing what the
// pages did. The page budget is what turns a cursor that fails to advance into
// a failure instead of a hang.
func (f efficacyFixture) drain(t *testing.T, pageSize int32, maxPages int) enqueueCounts {
	t.Helper()

	var total enqueueCounts
	cursor := EnqueueCursor{SeenAt: time.Time{}, ID: uuid.Nil}
	for range maxPages {
		page, err := EnqueuePage(t.Context(), f.db, &stubFeatures{enabled: true}, f.projectID, cursor, pageSize)
		require.NoError(t, err)
		require.LessOrEqual(t, page.Scanned, int(pageSize), "a page never scans past its size")

		total.Scanned += page.Scanned
		total.Units += page.Units
		total.Confirmed += page.Confirmed
		total.Stamped += page.Stamped

		if page.Exhausted {
			return total
		}
		require.NotEqual(t, cursor, page.NextCursor, "the cursor advances on every page that read something")
		cursor = page.NextCursor
	}

	t.Fatalf("walk did not finish within %d pages", maxPages)

	return total
}

func TestEnqueuePageRejectsPageSizeOutsideBounds(t *testing.T) {
	t.Parallel()
	fixture := newEfficacyFixture(t, "skill_efficacy_enqueue_page_size")

	for _, pageSize := range []int32{0, -1, MaxEnqueuePageSize + 1} {
		_, err := EnqueuePage(t.Context(), fixture.db, &stubFeatures{enabled: true}, fixture.projectID, EnqueueCursor{SeenAt: time.Time{}, ID: uuid.Nil}, pageSize)
		require.ErrorContains(t, err, "page size must be between")
	}
}

func TestEnqueuePageFoldsActivationsIntoOneUnitAndIsIdempotent(t *testing.T) {
	t.Parallel()
	fixture := newEfficacyFixture(t, "skill_efficacy_enqueue_idempotent")

	sessionID := "claude-session-idempotent"
	chatID := fixture.seedChat(t, sessionID, 2, 90*time.Minute)
	older := time.Now().UTC().Add(-3 * time.Hour)
	newer := time.Now().UTC().Add(-2 * time.Hour)
	fixture.observe(t, sessionID, "claude-code", older)
	fixture.observe(t, sessionID, "claude-code", newer)

	result := fixture.firstPage(t, 50)
	require.Equal(t, enqueueCounts{Scanned: 2, Units: 1, Confirmed: 1, Stamped: 2}, countsOf(result))
	require.True(t, result.Exhausted)

	rows := fixture.pendingEvaluations(t)
	require.Len(t, rows, 1)
	require.Equal(t, StatePending, rows[0].State)
	require.Equal(t, SurfaceDev, rows[0].Surface)
	require.Equal(t, sessionID, rows[0].SessionID)
	require.Equal(t, chatID, rows[0].ChatID, "the chat is derived from the session id the capture path wrote under")
	require.Equal(t, fixture.skillVersionID, rows[0].SkillVersionID)
	require.WithinDuration(t, newer, rows[0].ObservedAt.Time, time.Second, "observed_at is the latest activation")

	// Replaying the same cursor finds nothing to scan because the first page
	// stamped every activation, and the unit stays single either way.
	replay := fixture.firstPage(t, 50)
	require.Equal(t, enqueueCounts{Scanned: 0, Units: 0, Confirmed: 0, Stamped: 0}, countsOf(replay))
	require.True(t, replay.Exhausted)
	require.Len(t, fixture.pendingEvaluations(t), 1)
}

func TestEnqueuePageDeduplicatesUserIDAndEmailBindingsForSameScoringUnit(t *testing.T) {
	t.Parallel()
	fixture := newEfficacyFixture(t, "efficacy_enqueue_actor_dedup")

	sessionID := "claude-session-actor-deduplication"
	fixture.seedChat(t, sessionID, 2, 90*time.Minute)
	older := time.Now().UTC().Add(-3 * time.Hour)
	newer := time.Now().UTC().Add(-2 * time.Hour)
	fixture.observeAs(t, sessionID, "claude-code", actor{userID: defaultActor.userID, email: ""}, older)
	fixture.observeAs(t, sessionID, "claude-code", actor{userID: "", email: defaultActor.email}, newer)

	result := fixture.firstPage(t, 50)
	require.Equal(t, enqueueCounts{Scanned: 2, Units: 2, Confirmed: 2, Stamped: 2}, countsOf(result))

	rows := fixture.pendingEvaluations(t)
	require.Len(t, rows, 1)
	require.WithinDuration(t, newer, rows[0].ObservedAt.Time, time.Second)
}

func TestEnqueuePageSkipsSessionWithRecentMessage(t *testing.T) {
	t.Parallel()
	fixture := newEfficacyFixture(t, "skill_efficacy_enqueue_recent")

	sessionID := "claude-session-recent"
	fixture.seedChat(t, sessionID, 2, time.Minute)
	fixture.observe(t, sessionID, "claude-code", time.Now().UTC().Add(-2*time.Hour))

	result := fixture.firstPage(t, 50)
	require.Equal(t, enqueueCounts{Scanned: 1, Units: 1, Confirmed: 0, Stamped: 0}, countsOf(result))
	require.Empty(t, fixture.pendingEvaluations(t))

	// The activation stays unstamped, so it is still pending for a later walk to
	// pick up once the chat quiets.
	remaining, err := repo.New(fixture.db).ListPendingSkillObservations(t.Context(), repo.ListPendingSkillObservationsParams{
		ProjectID:   fixture.projectID,
		BatchSize:   50,
		AfterSeenAt: pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false},
		AfterID:     uuid.NullUUID{UUID: uuid.Nil, Valid: false},
	})
	require.NoError(t, err)
	require.Len(t, remaining, 1)
}

func TestEnqueuePageSkipsRecentActivationWithOldTranscript(t *testing.T) {
	t.Parallel()
	fixture := newEfficacyFixture(t, "skill_efficacy_enqueue_recent_activation")

	sessionID := "claude-session-recent-activation"
	fixture.seedChat(t, sessionID, 2, 90*time.Minute)
	fixture.observe(t, sessionID, "claude-code", time.Now().UTC())

	result := fixture.firstPage(t, 50)
	require.Equal(t, enqueueCounts{Scanned: 1, Units: 1, Confirmed: 0, Stamped: 0}, countsOf(result))
	require.Empty(t, fixture.pendingEvaluations(t), "the current activation cannot score an old visible transcript prefix")
}

func TestEnqueuePageRefreshesExistingPendingUnitForRecentActivation(t *testing.T) {
	t.Parallel()
	fixture := newEfficacyFixture(t, "efficacy_enqueue_resumed_session")

	sessionID := "claude-session-resumed"
	fixture.seedChat(t, sessionID, 2, 90*time.Minute)
	fixture.observe(t, sessionID, "claude-code", time.Now().UTC().Add(-2*time.Hour))
	first := fixture.firstPage(t, 50)
	require.Equal(t, enqueueCounts{Scanned: 1, Units: 1, Confirmed: 1, Stamped: 1}, countsOf(first))

	fixture.observe(t, sessionID, "claude-code", time.Now().UTC())
	second := fixture.firstPage(t, 50)
	require.Equal(t, enqueueCounts{Scanned: 1, Units: 1, Confirmed: 0, Stamped: 0}, countsOf(second))
	require.Empty(t, fixture.pendingEvaluations(t), "the refreshed unit cannot reserve until the resumed session is quiet")
}

func TestEnqueuePageSkipsSessionWithoutMessages(t *testing.T) {
	t.Parallel()
	fixture := newEfficacyFixture(t, "skill_efficacy_enqueue_empty")

	sessionID := "claude-session-empty"
	fixture.seedChat(t, sessionID, 0, 0)
	fixture.observe(t, sessionID, "claude-code", time.Now().UTC().Add(-2*time.Hour))

	result := fixture.firstPage(t, 50)
	require.Equal(t, enqueueCounts{Scanned: 1, Units: 1, Confirmed: 0, Stamped: 0}, countsOf(result))
	require.Empty(t, fixture.pendingEvaluations(t))
}

// A page reads at most what it was asked for, and it hands back a cursor past
// everything it read even when it could confirm none of it — that is what a
// coordinator persists to make progress across calls.
func TestEnqueuePageBoundsScanAndAdvancesCursorWithoutConfirmations(t *testing.T) {
	t.Parallel()
	fixture := newEfficacyFixture(t, "skill_efficacy_enqueue_cursor")

	seenAt := time.Now().UTC().Add(-4 * time.Hour)
	for i := range 4 {
		session := fmt.Sprintf("claude-session-cursor-%d", i)
		fixture.seedChat(t, session, 0, 0)
		fixture.observe(t, session, "claude-code", seenAt.Add(time.Duration(i)*time.Minute))
	}

	first := fixture.firstPage(t, 2)
	require.Equal(t, enqueueCounts{Scanned: 2, Units: 2, Confirmed: 0, Stamped: 0}, countsOf(first))
	require.False(t, first.Exhausted)
	require.NotEqual(t, uuid.Nil, first.NextCursor.ID)

	second, err := EnqueuePage(t.Context(), fixture.db, &stubFeatures{enabled: true}, fixture.projectID, first.NextCursor, 2)
	require.NoError(t, err)
	require.Equal(t, enqueueCounts{Scanned: 2, Units: 2, Confirmed: 0, Stamped: 0}, countsOf(second))
	require.True(t, second.NextCursor.SeenAt.After(first.NextCursor.SeenAt), "the cursor is strictly past the previous page")

	// Nothing was scoreable, so nothing was stamped and the pending set is intact.
	require.Empty(t, fixture.pendingEvaluations(t))
	third, err := EnqueuePage(t.Context(), fixture.db, &stubFeatures{enabled: true}, fixture.projectID, second.NextCursor, 2)
	require.NoError(t, err)
	require.Equal(t, enqueueCounts{Scanned: 0, Units: 0, Confirmed: 0, Stamped: 0}, countsOf(third))
	require.True(t, third.Exhausted)
	require.Equal(t, second.NextCursor, third.NextCursor, "an empty page leaves the cursor where it was")
}

// A chat that never receives a message can never be scored and can never be
// stamped, so it sits at the head of the pending queue forever. A caller
// chaining the returned cursor has to page past arbitrarily many of them and
// still reach the scoreable activation behind them.
func TestEnqueuePageChainedCursorReachesEligibleRowPastIneligibleOnes(t *testing.T) {
	t.Parallel()
	fixture := newEfficacyFixture(t, "skill_efficacy_enqueue_starvation")

	const abandoned = 7
	const pageSize int32 = 2
	seenAt := time.Now().UTC().Add(-4 * time.Hour)
	for i := range abandoned {
		session := fmt.Sprintf("claude-session-abandoned-%d", i)
		fixture.seedChat(t, session, 0, 0)
		fixture.observe(t, session, "claude-code", seenAt.Add(time.Duration(i)*time.Minute))
	}

	sessionID := "claude-session-live"
	fixture.seedChat(t, sessionID, 1, 90*time.Minute)
	fixture.observe(t, sessionID, "claude-code", time.Now().UTC().Add(-time.Hour))

	total := fixture.drain(t, pageSize, abandoned+1)
	require.Equal(t, enqueueCounts{Scanned: abandoned + 1, Units: abandoned + 1, Confirmed: 1, Stamped: 1}, total)

	rows := fixture.pendingEvaluations(t)
	require.Len(t, rows, 1)
	require.Equal(t, sessionID, rows[0].SessionID)
}

// A page never inserts more units than the activations it read, and the rest of
// the queue is reached by chaining rather than by widening.
func TestEnqueuePageStopsAtPageSize(t *testing.T) {
	t.Parallel()
	fixture := newEfficacyFixture(t, "skill_efficacy_enqueue_target")

	seenAt := time.Now().UTC().Add(-4 * time.Hour)
	for i := range 5 {
		session := fmt.Sprintf("claude-session-target-%d", i)
		fixture.seedChat(t, session, 1, 90*time.Minute)
		fixture.observe(t, session, "claude-code", seenAt.Add(time.Duration(i)*time.Minute))
	}

	result := fixture.firstPage(t, 2)
	require.Equal(t, enqueueCounts{Scanned: 2, Units: 2, Confirmed: 2, Stamped: 2}, countsOf(result))
	require.False(t, result.Exhausted)
	require.Len(t, fixture.pendingEvaluations(t), 2)

	// The rest are still pending, and a walk restarted at the head takes them
	// because the stamped ones have left the pending set.
	next := fixture.firstPage(t, 50)
	require.Equal(t, enqueueCounts{Scanned: 3, Units: 3, Confirmed: 3, Stamped: 3}, countsOf(next))
	require.Len(t, fixture.pendingEvaluations(t), 5)
}

// A deleted chat has no transcript to score, and a deleted project must not
// accrue evaluations at all.
func TestEnqueuePageSkipsDeletedChatsAndProjects(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	fixture := newEfficacyFixture(t, "skill_efficacy_enqueue_deleted")

	deletedChatSession := "claude-session-deleted-chat"
	chatID := fixture.seedChat(t, deletedChatSession, 1, 90*time.Minute)
	fixture.observe(t, deletedChatSession, "claude-code", time.Now().UTC().Add(-2*time.Hour))

	deleted, err := chatrepo.New(fixture.db).SoftDeleteChat(ctx, chatrepo.SoftDeleteChatParams{
		ProjectID: fixture.projectID,
		ID:        chatID,
	})
	require.NoError(t, err)
	require.True(t, deleted.Deleted)

	result := fixture.firstPage(t, 50)
	require.Equal(t, enqueueCounts{Scanned: 1, Units: 1, Confirmed: 0, Stamped: 0}, countsOf(result))
	require.Empty(t, fixture.pendingEvaluations(t))
	remaining, err := repo.New(fixture.db).ListPendingSkillObservations(ctx, repo.ListPendingSkillObservationsParams{
		ProjectID:   fixture.projectID,
		BatchSize:   50,
		AfterSeenAt: pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false},
		AfterID:     uuid.NullUUID{UUID: uuid.Nil, Valid: false},
	})
	require.NoError(t, err)
	require.Empty(t, remaining, "a deleted chat permanently retires its observation")
	projects, err := PendingWorkProjects(ctx, fixture.db, uuid.Nil, StaleReservationAfter, MaxSweepProjectPage)
	require.NoError(t, err)
	require.Empty(t, projects, "a deleted chat's retired observation does not keep waking the project")

	// A live session in a project that is then deleted is refused too.
	liveSession := "claude-session-deleted-project"
	fixture.seedChat(t, liveSession, 1, 90*time.Minute)
	fixture.observe(t, liveSession, "claude-code", time.Now().UTC().Add(-2*time.Hour))

	_, err = projectsrepo.New(fixture.db).DeleteProject(ctx, fixture.projectID)
	require.NoError(t, err)

	// A deleted project resolves no settings row, exactly as Reserve already
	// treats it: nothing to admit, so nothing is even scanned. The sweep excludes
	// deleted projects rather than repeatedly signalling permanently dead work.
	result = fixture.firstPage(t, 50)
	require.Equal(t, enqueueCounts{Scanned: 0, Units: 0, Confirmed: 0, Stamped: 0}, countsOf(result))
	require.Empty(t, fixture.pendingEvaluations(t))
	projects, err = PendingWorkProjects(ctx, fixture.db, uuid.Nil, StaleReservationAfter, MaxSweepProjectPage)
	require.NoError(t, err)
	require.Empty(t, projects, "a deleted project's observation does not keep waking the project")
}

func TestEnqueueConfirmationRejectsAnExistingUnitAfterChatDeletion(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	fixture := newEfficacyFixture(t, "efficacy_enqueue_deleted_confirm")

	sessionID := "claude-session-deleted-confirmation"
	chatID := fixture.seedChat(t, sessionID, 1, 90*time.Minute)
	fixture.observe(t, sessionID, "claude-code", time.Now().UTC().Add(-2*time.Hour))
	first := fixture.firstPage(t, 50)
	require.Equal(t, enqueueCounts{Scanned: 1, Units: 1, Confirmed: 1, Stamped: 1}, countsOf(first))

	fixture.observe(t, sessionID, "claude-code", time.Now().UTC().Add(-time.Hour))
	deleted, err := chatrepo.New(fixture.db).SoftDeleteChat(ctx, chatrepo.SoftDeleteChatParams{
		ProjectID: fixture.projectID,
		ID:        chatID,
	})
	require.NoError(t, err)
	require.True(t, deleted.Deleted)

	second := fixture.firstPage(t, 50)
	require.Equal(t, enqueueCounts{Scanned: 1, Units: 1, Confirmed: 0, Stamped: 0}, countsOf(second))

	remaining, err := repo.New(fixture.db).ListPendingSkillObservations(ctx, repo.ListPendingSkillObservationsParams{
		ProjectID:   fixture.projectID,
		BatchSize:   50,
		AfterSeenAt: pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false},
		AfterID:     uuid.NullUUID{UUID: uuid.Nil, Valid: false},
	})
	require.NoError(t, err)
	require.Empty(t, remaining, "the deleted chat permanently retires the observation from the safety sweep")
	projects, err := PendingWorkProjects(ctx, fixture.db, uuid.Nil, StaleReservationAfter, MaxSweepProjectPage)
	require.NoError(t, err)
	require.Empty(t, projects)
}

// The candidate scan and the insert are separated by the confirmation read, so
// the insert rechecks quietness against the chat ids it was handed. A message
// that lands in between withdraws the unit and leaves the activation unstamped.
func TestEnqueueInsertRechecksQuietnessAgainstPassedChats(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	fixture := newEfficacyFixture(t, "skill_efficacy_enqueue_race")

	sessionID := "claude-session-race"
	chatID := fixture.seedChat(t, sessionID, 1, 90*time.Minute)
	fixture.observe(t, sessionID, "claude-code", time.Now().UTC().Add(-2*time.Hour))

	queries := repo.New(fixture.db)
	observations, err := queries.ListPendingSkillObservations(ctx, repo.ListPendingSkillObservationsParams{
		ProjectID:   fixture.projectID,
		BatchSize:   50,
		AfterSeenAt: pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false},
		AfterID:     uuid.NullUUID{UUID: uuid.Nil, Valid: false},
	})
	require.NoError(t, err)
	require.Len(t, observations, 1)
	require.Equal(t, chatID, chat.SessionIDToChatID(observations[0].SessionID))

	// The session resumes after it was scanned.
	_, err = chatrepo.New(fixture.db).SeedChatMessage(ctx, chatrepo.SeedChatMessageParams{
		ChatID:    chatID,
		ProjectID: uuid.NullUUID{UUID: fixture.projectID, Valid: true},
		CreatedAt: conv.ToPGTimestamptz(time.Now().UTC()),
	})
	require.NoError(t, err)

	require.NoError(t, queries.EnqueueSkillEfficacyEvaluations(ctx, repo.EnqueueSkillEfficacyEvaluationsParams{
		ProjectID:        fixture.projectID,
		Inactivity:       pgtype.Interval{Microseconds: InactivityWindow.Microseconds(), Days: 0, Months: 0, Valid: true},
		SessionIds:       []string{observations[0].SessionID},
		Surfaces:         []string{observations[0].Surface},
		ChatIds:          []uuid.UUID{chatID},
		SkillIds:         []uuid.UUID{observations[0].SkillID},
		SkillVersionIds:  []uuid.UUID{observations[0].SkillVersionID},
		CanonicalSha256s: []string{observations[0].CanonicalSha256},
		ObservedAts:      []pgtype.Timestamptz{observations[0].SeenAt},
		UserIds:          []string{observations[0].UserID},
		UserEmails:       []string{observations[0].UserEmail},
	}))

	units, err := queries.ListSkillEfficacyEvaluationUnits(ctx, repo.ListSkillEfficacyEvaluationUnitsParams{
		ProjectID:       fixture.projectID,
		SessionIds:      []string{observations[0].SessionID},
		Surfaces:        []string{observations[0].Surface},
		SkillVersionIds: []uuid.UUID{observations[0].SkillVersionID},
		UserIds:         []string{observations[0].UserID},
		UserEmails:      []string{observations[0].UserEmail},
	})
	require.NoError(t, err)
	require.Empty(t, units, "the resumed session is not confirmed, so nothing authorises the stamp")
	require.Empty(t, fixture.pendingEvaluations(t))
}

// A dev session id arrives from the client, so one actor can name another's
// session — including a chat id verbatim. The unit is only ever bound to the
// chat when the activation's own actor owns it, and the owner's activations for
// the same session are unaffected by the attempt.
func TestEnqueuePageRefusesDevSessionOwnedByAnotherActor(t *testing.T) {
	t.Parallel()
	fixture := newEfficacyFixture(t, "skill_efficacy_enqueue_cross_actor")

	owner := actor{userID: "owner-user", email: "owner@example.test"}
	intruder := actor{userID: "intruder-user", email: "intruder@example.test"}

	// The session id is the chat's own uuid, which is the strongest guess an
	// intruder can make: it needs no mapping to land on someone else's chat.
	sessionID := uuid.NewString()
	chatID := fixture.seedChatFor(t, sessionID, owner, 2, 90*time.Minute)
	require.Equal(t, chatID, uuid.MustParse(sessionID))

	fixture.observeAs(t, sessionID, "claude-code", intruder, time.Now().UTC().Add(-3*time.Hour))

	refused := fixture.firstPage(t, 50)
	require.Equal(t, enqueueCounts{Scanned: 1, Units: 1, Confirmed: 0, Stamped: 0}, countsOf(refused))
	require.Empty(t, fixture.pendingEvaluations(t), "another actor's transcript is never enqueued")

	// The owner's own activation for that session scores, and it scores alone:
	// the refused activation is scanned again, confirms nothing and stays
	// unstamped, so it can never ride the owner's unit.
	fixture.observeAs(t, sessionID, "claude-code", owner, time.Now().UTC().Add(-2*time.Hour))

	admitted := fixture.firstPage(t, 50)
	require.Equal(t, enqueueCounts{Scanned: 2, Units: 2, Confirmed: 1, Stamped: 1}, countsOf(admitted))

	rows := fixture.pendingEvaluations(t)
	require.Len(t, rows, 1)
	require.Equal(t, SurfaceDev, rows[0].Surface)
	require.Equal(t, sessionID, rows[0].SessionID)
	require.Equal(t, chatID, rows[0].ChatID)

	remaining, err := repo.New(fixture.db).ListPendingSkillObservations(t.Context(), repo.ListPendingSkillObservationsParams{
		ProjectID:   fixture.projectID,
		BatchSize:   50,
		AfterSeenAt: pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false},
		AfterID:     uuid.NullUUID{UUID: uuid.Nil, Valid: false},
	})
	require.NoError(t, err)
	require.Len(t, remaining, 1)
	require.Equal(t, intruder.userID, remaining[0].UserID, "only the refused activation is still pending")
}

// An activation that carries no actor cannot be bound to a chat, so the dev
// surface refuses it rather than trusting the session id on its own.
func TestEnqueuePageRefusesDevActivationWithoutActor(t *testing.T) {
	t.Parallel()
	fixture := newEfficacyFixture(t, "skill_efficacy_enqueue_no_actor")

	sessionID := "claude-session-anonymous"
	fixture.seedChat(t, sessionID, 2, 90*time.Minute)
	fixture.observeAs(t, sessionID, "claude-code", actor{userID: "", email: ""}, time.Now().UTC().Add(-2*time.Hour))

	result := fixture.firstPage(t, 50)
	require.Equal(t, enqueueCounts{Scanned: 1, Units: 1, Confirmed: 0, Stamped: 0}, countsOf(result))
	require.Empty(t, fixture.pendingEvaluations(t))
}

// The email is a binding of its own: an activation resolved to an email but no
// user id still owns the chat the capture path wrote that email to.
func TestEnqueuePageBindsDevSessionByActorEmailAlone(t *testing.T) {
	t.Parallel()
	fixture := newEfficacyFixture(t, "skill_efficacy_enqueue_email_actor")

	sessionID := "claude-session-email-only"
	fixture.seedChat(t, sessionID, 2, 90*time.Minute)
	fixture.observeAs(t, sessionID, "claude-code", actor{userID: "", email: defaultActor.email}, time.Now().UTC().Add(-2*time.Hour))

	result := fixture.firstPage(t, 50)
	require.Equal(t, enqueueCounts{Scanned: 1, Units: 1, Confirmed: 1, Stamped: 1}, countsOf(result))
	require.Len(t, fixture.pendingEvaluations(t), 1)
}

func TestEnqueuePageIgnoresUnreconciledActivations(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	fixture := newEfficacyFixture(t, "skill_efficacy_enqueue_unresolved")

	sessionID := "claude-session-unresolved"
	fixture.seedChat(t, sessionID, 2, 90*time.Minute)
	_, err := hooksrepo.New(fixture.db).InsertSkillObservation(ctx, hooksrepo.InsertSkillObservationParams{
		ProjectID:      fixture.projectID,
		IdempotencyKey: conv.ToPGText(uuid.NewString()),
		Provider:       "claude-code",
		UserID:         pgtype.Text{String: "", Valid: false},
		UserEmail:      pgtype.Text{String: "", Valid: false},
		Hostname:       pgtype.Text{String: "", Valid: false},
		SessionID:      conv.ToPGText(sessionID),
		SkillName:      "efficacy-skill",
		Source:         pgtype.Text{String: "", Valid: false},
		SourceLevel:    conv.ToPGText("project"),
		SourcePath:     pgtype.Text{String: "", Valid: false},
		RawSha256:      conv.ToPGText(hex.EncodeToString([]byte("unresolved"))),
		SeenAt:         conv.ToPGTimestamptz(time.Now().UTC().Add(-2 * time.Hour)),
	})
	require.NoError(t, err)

	result := fixture.firstPage(t, 50)
	require.Equal(t, enqueueCounts{Scanned: 0, Units: 0, Confirmed: 0, Stamped: 0}, countsOf(result))
	require.Empty(t, fixture.pendingEvaluations(t))
}

func TestEnqueuePageStampsOnlyConfirmedUnitsAcrossSurfaces(t *testing.T) {
	t.Parallel()
	fixture := newEfficacyFixture(t, "skill_efficacy_enqueue_surfaces")

	assistantSession := uuid.NewString()
	devSession := "claude-session-surfaces"
	noisySession := "claude-session-noisy"
	fixture.seedChat(t, assistantSession, 1, 90*time.Minute)
	devChatID := fixture.seedChat(t, devSession, 1, 90*time.Minute)
	fixture.seedChat(t, noisySession, 1, time.Minute)

	seenAt := time.Now().UTC().Add(-2 * time.Hour)
	// The assistant path records no actor: its session id is server-generated,
	// so the chat binding the dev surface needs does not apply to it.
	fixture.observeAs(t, assistantSession, "assistants", actor{userID: "", email: ""}, seenAt)
	fixture.observe(t, devSession, "claude-code", seenAt)
	fixture.observe(t, noisySession, "claude-code", seenAt)

	result := fixture.firstPage(t, 50)
	require.Equal(t, enqueueCounts{Scanned: 3, Units: 3, Confirmed: 2, Stamped: 2}, countsOf(result))

	surfaces := make(map[string]repo.SkillEfficacyEvaluation, 2)
	for _, row := range fixture.pendingEvaluations(t) {
		surfaces[row.Surface] = row
	}
	require.Len(t, surfaces, 2)
	require.Equal(t, assistantSession, surfaces[SurfaceAssistant].SessionID)
	require.Equal(t, uuid.MustParse(assistantSession), surfaces[SurfaceAssistant].ChatID, "a session id that is already a chat id resolves directly")
	require.Equal(t, devSession, surfaces[SurfaceDev].SessionID)
	require.Equal(t, devChatID, surfaces[SurfaceDev].ChatID, "a non-uuid session id resolves through the capture-path hash")

	// The noisy session was not enqueueable and stays unstamped, so it is still
	// scanned — and still refused — by the next walk.
	retry := fixture.firstPage(t, 50)
	require.Equal(t, enqueueCounts{Scanned: 1, Units: 1, Confirmed: 0, Stamped: 0}, countsOf(retry))
	require.Len(t, fixture.pendingEvaluations(t), 2)
}

// An organization no reservation can ever spend for gets no queue built for it:
// the rows would sit pending until the entitlement arrived, and every sweep tick
// in between would wake a coordinator that can only read them again.
func TestEnqueuePageBuildsNoQueueForAnOrganizationThatCannotSpend(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	tests := []struct {
		name     string
		features *stubFeatures
		disable  bool
	}{
		{name: "unentitled organization", features: &stubFeatures{enabled: false}, disable: false},
		{name: "pipeline switched off", features: &stubFeatures{enabled: true}, disable: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fixture := newEfficacyFixture(t, "skill_efficacy_enqueue_unspendable")

			sessionID := "enqueue-unspendable-" + uuid.NewString()[:8]
			fixture.seedChat(t, sessionID, 2, 90*time.Minute)
			fixture.observe(t, sessionID, "claude-code", time.Now().UTC().Add(-2*time.Hour))

			if tt.disable {
				_, err := repo.New(fixture.db).UpsertSkillEfficacySettingsForProject(ctx, repo.UpsertSkillEfficacySettingsForProjectParams{
					ProjectID:        fixture.projectID,
					Enabled:          false,
					PerSkillDailyCap: DefaultPerSkillDailyCap,
					OrgDailyCap:      DefaultOrgDailyCap,
					NewVersionBurst:  DefaultNewVersionBurst,
				})
				require.NoError(t, err)
			}

			result, err := EnqueuePage(ctx, fixture.db, tt.features, fixture.projectID, EnqueueCursor{}, 50)
			require.NoError(t, err)
			require.Equal(t, enqueueCounts{Scanned: 0, Units: 0, Confirmed: 0, Stamped: 0}, countsOf(result))
			require.True(t, result.Exhausted)
			require.Equal(t, EnqueueCursor{}, result.NextCursor, "a walk that read nothing starts the next one at the head")
			require.Empty(t, fixture.pendingEvaluations(t))

			// Nothing was stamped, so the entitlement arriving later still finds
			// every activation the refusal skipped.
			entitled, err := EnqueuePage(ctx, fixture.db, &stubFeatures{enabled: true}, fixture.projectID, EnqueueCursor{}, 50)
			require.NoError(t, err)
			if tt.disable {
				require.Equal(t, enqueueCounts{Scanned: 0, Units: 0, Confirmed: 0, Stamped: 0}, countsOf(entitled))
				return
			}
			require.Equal(t, enqueueCounts{Scanned: 1, Units: 1, Confirmed: 1, Stamped: 1}, countsOf(entitled))
			require.Len(t, fixture.pendingEvaluations(t), 1)
		})
	}
}
