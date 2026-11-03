package orgmemory_test

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

	gen "github.com/speakeasy-api/gram/server/gen/org_memory"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/orgmemory"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

var infra *testenv.Environment

func TestMain(m *testing.M) {
	res, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{Postgres: true, Redis: true})
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

type fixture struct {
	db        *pgxpool.Pool
	service   *orgmemory.Service
	flags     *feature.InMemory
	ctx       context.Context //nolint:containedctx // the authenticated session each test derives grants from
	orgID     string
	projectID uuid.UUID
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	logger, tp := testenv.NewLogger(t), testenv.NewTracerProvider(t)
	db, err := infra.CloneTestDatabase(t, "orgmemory")
	require.NoError(t, err)
	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)
	mgr := testenv.NewTestManager(t, logger, tp, db, redisClient, cache.Suffix("gram-local"), billing.NewStubClient(logger, tp))
	engine := authz.NewEngine(logger, db, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient())

	ctx := testenv.InitAuthContext(t, t.Context(), db, mgr)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	flags := &feature.InMemory{}
	flags.SetFlag(feature.FlagOrgMemory, authCtx.ActiveOrganizationID, true)

	return fixture{
		db:        db,
		service:   orgmemory.NewService(logger, tp, db, mgr, engine, audit.NewLogger(), flags),
		flags:     flags,
		ctx:       ctx,
		orgID:     authCtx.ActiveOrganizationID,
		projectID: *authCtx.ProjectID,
	}
}

// reader holds memory:read on the project.
func (f fixture) reader(t *testing.T) context.Context {
	t.Helper()
	return authztest.WithExactGrants(t, f.ctx, authz.NewGrant(authz.ScopeMemoryRead, f.projectID.String()))
}

// reviewer also holds chat:read on the project, so session titles show.
func (f fixture) reviewer(t *testing.T) context.Context {
	t.Helper()
	return authztest.WithExactGrants(t, f.ctx,
		authz.NewGrant(authz.ScopeMemoryRead, f.projectID.String()),
		authz.NewGrant(authz.ScopeChatRead, f.projectID.String()))
}

func (f fixture) summarizedChat(t *testing.T, title, summary string, at time.Time) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := testrepo.New(f.db).SeedCapturedAgentChatFixture(t.Context(), testrepo.SeedCapturedAgentChatFixtureParams{
		ID: id, ProjectID: f.projectID, OrganizationID: f.orgID,
		UserID: conv.ToPGText("user_other"), ExternalChatID: conv.ToPGText("ext-" + id.String()),
		Title: conv.ToPGText(title), Cwd: conv.ToPGText("/code"), UserAccountID: uuid.NullUUID{},
	})
	require.NoError(t, err)
	require.NoError(t, testrepo.New(f.db).SetChatMemorySummaryFixture(t.Context(), testrepo.SetChatMemorySummaryFixtureParams{
		ID: id, Summary: conv.ToPGText(summary), SummaryGeneratedAt: conv.ToPGTimestamptz(at),
	}))
	return id
}

func (f fixture) memo(t *testing.T, content string, chatID *uuid.UUID, at time.Time) uuid.UUID {
	t.Helper()
	chat := uuid.NullUUID{}
	if chatID != nil {
		chat = uuid.NullUUID{UUID: *chatID, Valid: true}
	}
	id, err := testrepo.New(f.db).InsertMemoryMemoAtFixture(t.Context(), testrepo.InsertMemoryMemoAtFixtureParams{
		OrganizationID: f.orgID, ProjectID: f.projectID, ChatID: chat, Content: content, CreatedAt: conv.ToPGTimestamptz(at),
	})
	require.NoError(t, err)
	return id
}

func (f fixture) digest(t *testing.T, level string, start, end time.Time, content string, chats, memos, children []uuid.UUID) uuid.UUID {
	t.Helper()
	date := func(d time.Time) pgtype.Date {
		return pgtype.Date{Time: d, InfinityModifier: pgtype.Finite, Valid: true}
	}
	id, err := testrepo.New(f.db).InsertMemoryDigestSourcesFixture(t.Context(), testrepo.InsertMemoryDigestSourcesFixtureParams{
		OrganizationID: f.orgID, ProjectID: f.projectID, Level: level,
		PeriodStart: date(start), PeriodEnd: date(end), Content: content,
		ChatIds: chats, MemoIds: memos, ChildDigestIds: children,
	})
	require.NoError(t, err)
	return id
}

func day(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }

func TestOrgMemoryRequiresMemoryReadAndRollout(t *testing.T) {
	t.Parallel()
	f := newFixture(t)

	for _, grants := range [][]authz.Grant{
		{authz.NewGrant(authz.ScopeProjectRead, authz.WildcardResource)},
		{authz.NewGrant(authz.ScopeChatRead, authz.WildcardResource)},
		{authz.NewGrant(authz.ScopeMemoryRead, uuid.NewString())},
	} {
		ctx := authztest.WithExactGrants(t, f.ctx, grants...)
		_, err := f.service.ListOrgMemoryDigests(ctx, &gen.ListOrgMemoryDigestsPayload{Level: "week", Limit: 12})
		require.Error(t, err, "grants %v", grants)
	}

	f.flags.SetFlag(feature.FlagOrgMemory, f.orgID, false)
	_, err := f.service.GetOrgMemoryOverview(f.reader(t), &gen.GetOrgMemoryOverviewPayload{Days: 30})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeNotFound, oopsErr.Code)
}

func TestGetOrgMemoryDigestCitesOnlyEligibleSources(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	at := day(2026, time.October, 27).Add(10 * time.Hour)

	kept := f.summarizedChat(t, "Webhook cutover", "Moved billing webhooks onto the outbox.", at)
	excluded := f.summarizedChat(t, "Private planning", "Hiring plan draft.", at)
	require.NoError(t, testrepo.New(f.db).ExcludeChatFromMemoryFixture(t.Context(), excluded))
	memo := f.memo(t, "Staging deploy key rotates every 30 days.", &kept, at)

	dayID := f.digest(t, "day", day(2026, time.October, 27), day(2026, time.October, 28), "Webhook cutover finished.", []uuid.UUID{kept, excluded}, []uuid.UUID{memo}, nil)
	weekID := f.digest(t, "week", day(2026, time.October, 26), day(2026, time.November, 2), "Outbox week.", nil, nil, []uuid.UUID{dayID})

	before, err := audittest.AuditLogCountByAction(t.Context(), f.db, audit.ActionTeamMemoryRead)
	require.NoError(t, err)

	got, err := f.service.GetOrgMemoryDigest(f.reader(t), &gen.GetOrgMemoryDigestPayload{ID: dayID.String()})
	require.NoError(t, err)
	require.Equal(t, "day", got.Digest.Level)
	require.Equal(t, "2026-10-27", got.Digest.PeriodStart)
	require.Equal(t, 1, got.Digest.SessionCount, "an excluded session is neither listed nor counted")
	require.Len(t, got.Sessions, 1)
	require.Equal(t, kept.String(), got.Sessions[0].ID)
	require.Empty(t, got.Sessions[0].Title, "memory:read alone gets summaries without raw titles")
	require.Len(t, got.Memos, 1)
	require.Equal(t, "system", got.Memos[0].AuthorKind)
	require.Equal(t, kept.String(), *got.Memos[0].ChatID)

	reviewed, err := f.service.GetOrgMemoryDigest(f.reviewer(t), &gen.GetOrgMemoryDigestPayload{ID: dayID.String()})
	require.NoError(t, err)
	require.Equal(t, "Webhook cutover", reviewed.Sessions[0].Title)

	week, err := f.service.GetOrgMemoryDigest(f.reader(t), &gen.GetOrgMemoryDigestPayload{ID: weekID.String()})
	require.NoError(t, err)
	require.Len(t, week.Children, 1)
	require.Equal(t, dayID.String(), week.Children[0].ID)
	require.Empty(t, week.Sessions)

	after, err := audittest.AuditLogCountByAction(t.Context(), f.db, audit.ActionTeamMemoryRead)
	require.NoError(t, err)
	require.Equal(t, before+3, after)

	_, err = f.service.GetOrgMemoryDigest(f.reader(t), &gen.GetOrgMemoryDigestPayload{ID: uuid.NewString()})
	require.Error(t, err)
}

func TestListOrgMemoryDigestsPagesNewestFirst(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	for _, start := range []time.Time{day(2026, time.October, 12), day(2026, time.October, 19), day(2026, time.October, 26)} {
		f.digest(t, "week", start, start.AddDate(0, 0, 7), "Week of "+start.Format(time.DateOnly), nil, nil, nil)
	}
	f.digest(t, "month", day(2026, time.October, 1), day(2026, time.November, 1), "October.", nil, nil, nil)

	first, err := f.service.ListOrgMemoryDigests(f.reader(t), &gen.ListOrgMemoryDigestsPayload{Level: "week", Limit: 2})
	require.NoError(t, err)
	require.Len(t, first.Digests, 2)
	require.Equal(t, "2026-10-26", first.Digests[0].PeriodStart)
	require.Equal(t, "2026-10-19", first.Digests[1].PeriodStart)
	require.NotNil(t, first.NextBefore)

	second, err := f.service.ListOrgMemoryDigests(f.reader(t), &gen.ListOrgMemoryDigestsPayload{Level: "week", Limit: 2, Before: first.NextBefore})
	require.NoError(t, err)
	require.Len(t, second.Digests, 1)
	require.Equal(t, "2026-10-12", second.Digests[0].PeriodStart)
	require.Nil(t, second.NextBefore)
}

func TestOrgMemoryOverviewAndRecent(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	now := time.Now().UTC()

	folded := f.summarizedChat(t, "Folded", "Already in a day digest.", now.Add(-48*time.Hour))
	fresh := f.summarizedChat(t, "Fresh", "Not rolled up yet.", now.Add(-time.Hour))
	excluded := f.summarizedChat(t, "Excluded", "Left out.", now.Add(-time.Hour))
	require.NoError(t, testrepo.New(f.db).ExcludeChatFromMemoryFixture(t.Context(), excluded))
	f.memo(t, "Fresh memo.", nil, now.Add(-time.Hour))
	d := now.Add(-48 * time.Hour).Truncate(24 * time.Hour)
	f.digest(t, "day", d, d.AddDate(0, 0, 1), "Folded day.", []uuid.UUID{folded}, nil, nil)

	overview, err := f.service.GetOrgMemoryOverview(f.reader(t), &gen.GetOrgMemoryOverviewPayload{Days: 30})
	require.NoError(t, err)
	require.Equal(t, int64(2), overview.SessionsRemembered)
	require.Equal(t, int64(1), overview.SessionsLeftOut)
	require.Equal(t, int64(1), overview.MemosSaved)

	recent, err := f.service.ListOrgMemoryRecent(f.reader(t), &gen.ListOrgMemoryRecentPayload{Limit: 20})
	require.NoError(t, err)
	require.Len(t, recent.Sessions, 1)
	require.Equal(t, fresh.String(), recent.Sessions[0].ID)
	require.Len(t, recent.Memos, 1)

	// Dashboard reads are audited but never counted as agent recalls.
	again, err := f.service.GetOrgMemoryOverview(f.reader(t), &gen.GetOrgMemoryOverviewPayload{Days: 30})
	require.NoError(t, err)
	require.Equal(t, int64(0), again.Recalls)
}
