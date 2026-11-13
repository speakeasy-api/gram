package platformmcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/memorydigest"
	memorydigestrepo "github.com/speakeasy-api/gram/server/internal/memorydigest/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

// teamMemoryHistory is a month digest that rolled up a week digest that
// rolled up a day digest of one chat, plus a chat and a memo from today that
// no digest covers yet.
type teamMemoryHistory struct {
	foldedChat uuid.UUID
	day        uuid.UUID
	week       uuid.UUID
	month      uuid.UUID
	freshChat  uuid.UUID
	memo       uuid.UUID
}

func (f teamMemoryFixture) digest(t *testing.T, ctx context.Context, level memorydigest.Level, start time.Time, content string, chats, children []uuid.UUID) uuid.UUID {
	t.Helper()
	id, err := memorydigestrepo.New(f.db).InsertDigest(ctx, memorydigestrepo.InsertDigestParams{
		OrganizationID: f.principal.OrganizationID, ProjectID: f.project.ID, Level: string(level),
		PeriodStart: pgtype.Date{Time: start, InfinityModifier: pgtype.Finite, Valid: true},
		PeriodEnd:   pgtype.Date{Time: level.PeriodEnd(start), InfinityModifier: pgtype.Finite, Valid: true},
		Content:     content, ChatIds: chats, MemoIds: []uuid.UUID{}, ChildDigestIds: children,
		Model: pgtype.Text{}, InputTokens: pgtype.Int4{}, OutputTokens: pgtype.Int4{},
	})
	require.NoError(t, err)
	return id
}

func (f teamMemoryFixture) history(t *testing.T, ctx context.Context) teamMemoryHistory {
	t.Helper()
	now := time.Now().UTC()
	month := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, -3, 0)
	monday := month.AddDate(0, 0, (8-int(month.Weekday()))%7)
	var h teamMemoryHistory
	h.foldedChat = f.summarizedChat(t, ctx, f.project.ID, "Folded", "canary-folded-chat moved billing exports to the nightly job.", monday.Add(10*time.Hour), uuid.NullUUID{})
	h.day = f.digest(t, ctx, memorydigest.LevelDay, monday, "canary-day-digest billing exports moved to nightly.", []uuid.UUID{h.foldedChat}, []uuid.UUID{})
	h.week = f.digest(t, ctx, memorydigest.LevelWeek, monday, "canary-week-digest billing work that week.", []uuid.UUID{}, []uuid.UUID{h.day})
	h.month = f.digest(t, ctx, memorydigest.LevelMonth, month, "canary-month-digest billing exports were rebuilt this month.", []uuid.UUID{}, []uuid.UUID{h.week})
	h.freshChat = f.summarizedChat(t, ctx, f.project.ID, "Fresh", "canary-fresh-chat paused the export rollout after a timeout.\n\nSecond paragraph stays out of recall.", now.Add(-time.Hour), uuid.NullUUID{})
	memo, err := f.service.Remember(f.writer(ctx), f.principal, RememberInput{ProjectSlug: f.project.Slug, Content: "canary-memo exports must finish before 02:00 UTC."})
	require.NoError(t, err)
	h.memo = uuid.MustParse(memo.Memo.Source.ID)
	return h
}

func sourceRefs(sources []TeamMemorySource) []string {
	refs := make([]string, len(sources))
	for i, source := range sources {
		refs[i] = source.Ref()
	}
	return refs
}

func itemRefs(items []TeamMemoryItem) []string {
	refs := make([]string, len(items))
	for i, item := range items {
		refs[i] = item.Source.Ref()
	}
	return refs
}

func TestRecallTeamHistoryShowsEachPeriodAtItsMostCompressedLevel(t *testing.T) {
	t.Parallel()
	f := newTeamMemoryFixture(t)
	ctx := f.reader(t.Context())
	h := f.history(t, ctx)

	output, err := f.service.Recall(ctx, f.principal, RecallTeamHistoryInput{ProjectSlug: f.project.Slug})
	require.NoError(t, err)
	want := []string{"digest:" + h.month.String(), "chat:" + h.freshChat.String(), "memo:" + h.memo.String()}
	require.Equal(t, want, sourceRefs(output.Sources))
	require.Zero(t, output.Omitted)
	require.Contains(t, output.History, "[digest:"+h.month.String()+"]")
	require.Contains(t, output.History, "canary-month-digest")
	require.Contains(t, output.History, "canary-fresh-chat")
	require.Contains(t, output.History, "canary-memo")
	// Covered levels and the folded chat show only through the month, and
	// a chat contributes its first paragraph.
	for _, hidden := range []string{"canary-week-digest", "canary-day-digest", "canary-folded-chat", "Second paragraph"} {
		require.NotContains(t, output.History, hidden)
	}
	require.Less(t, strings.Index(output.History, "canary-month-digest"), strings.Index(output.History, "canary-fresh-chat"))

	event, err := audittest.LatestAuditLogByAction(ctx, f.db, audit.ActionTeamMemoryRead)
	require.NoError(t, err)
	var metadata struct {
		Tool    string   `json:"tool"`
		Sources []string `json:"sources"`
	}
	require.NoError(t, json.Unmarshal(event.Metadata, &metadata))
	require.Equal(t, want, metadata.Sources)
}

func TestRecallTeamHistoryHidesStaleDigestsAndIneligibleItems(t *testing.T) {
	t.Parallel()
	f := newTeamMemoryFixture(t)
	ctx := f.context(t.Context(), authz.NewGrant(authz.ScopeMemoryWrite, authz.WildcardResource))
	h := f.history(t, ctx)
	// Excluding the folded chat makes the day digest stale, which hides the
	// week and month above it until the nightly job rebuilds them.
	require.NoError(t, testrepo.New(f.db).ExcludeChatFromMemoryFixture(ctx, h.foldedChat))
	excluded := f.summarizedChat(t, ctx, f.project.ID, "Excluded", "canary-excluded-chat", time.Now().Add(-time.Hour), uuid.NullUUID{})
	require.NoError(t, testrepo.New(f.db).ExcludeChatFromMemoryFixture(ctx, excluded))
	other, err := projectsrepo.New(f.db).CreateProject(ctx, projectsrepo.CreateProjectParams{OrganizationID: f.principal.OrganizationID, Name: "Other recall project", Slug: "other-recall-project"})
	require.NoError(t, err)
	f.summarizedChat(t, ctx, other.ID, "Foreign", "canary-foreign-chat", time.Now().Add(-time.Hour), uuid.NullUUID{})

	output, err := f.service.Recall(ctx, f.principal, RecallTeamHistoryInput{ProjectSlug: f.project.Slug})
	require.NoError(t, err)
	require.Equal(t, []string{"chat:" + h.freshChat.String(), "memo:" + h.memo.String()}, sourceRefs(output.Sources))
	require.Equal(t, 2, strings.Count(output.History, "canary-"))

	_, err = f.service.Zoom(ctx, f.principal, ZoomTeamMemoryInput{ProjectSlug: f.project.Slug, Source: "digest:" + h.month.String()})
	require.ErrorIs(t, err, errTeamMemoryItemNotFound)
	_, err = f.service.Zoom(ctx, f.principal, ZoomTeamMemoryInput{ProjectSlug: f.project.Slug, Source: "chat:" + h.foldedChat.String()})
	require.ErrorIs(t, err, errTeamMemoryItemNotFound)
}

func TestRecallTeamHistoryRedactsBeforeCutting(t *testing.T) {
	t.Parallel()
	f := newTeamMemoryFixture(t)
	ctx := f.reader(t.Context())
	secret := "ghp_" + strings.Join([]string{"q1W2e3R4t5Y6", "u7I8o9P0a1S2", "d3F4g5H6j7K8"}, "")
	// The secret straddles the snippet cut, so masking after the cut would
	// miss it.
	summary := strings.Repeat("x", 590) + " " + secret + " rotated."
	f.summarizedChat(t, ctx, f.project.ID, "Token", summary, time.Now().Add(-time.Hour), uuid.NullUUID{})
	output, err := f.service.Recall(ctx, f.principal, RecallTeamHistoryInput{ProjectSlug: f.project.Slug})
	require.NoError(t, err)
	require.Len(t, output.Sources, 1)
	require.NotContains(t, output.History, secret[:8])
}

func TestRecallTeamHistoryValidatesBudgetAndRequiresMemoryRead(t *testing.T) {
	t.Parallel()
	f := newTeamMemoryFixture(t)
	for _, budget := range []int{-1, teamMemoryRecallMinTokens - 1, teamMemoryRecallMaxTokens + 1} {
		_, err := f.service.Recall(f.reader(t.Context()), f.principal, RecallTeamHistoryInput{ProjectSlug: f.project.Slug, MaxTokens: budget})
		require.ErrorIs(t, err, errTeamMemoryInvalid)
	}
	for _, grant := range []authz.Grant{
		authz.NewGrant(authz.ScopeChatRead, authz.WildcardResource),
		authz.NewGrant(authz.ScopeOrgAdmin, authz.WildcardResource),
	} {
		_, err := f.service.Recall(f.context(t.Context(), grant), f.principal, RecallTeamHistoryInput{ProjectSlug: f.project.Slug})
		var permission *ExternalAuthorizationError
		require.ErrorAs(t, err, &permission)
		require.Equal(t, "memory:read", permission.RequiredScope)
		_, err = f.service.Zoom(f.context(t.Context(), grant), f.principal, ZoomTeamMemoryInput{ProjectSlug: f.project.Slug, Source: "chat:" + uuid.NewString()})
		require.ErrorAs(t, err, &permission)
	}
	output, err := f.service.Recall(f.reader(t.Context()), f.principal, RecallTeamHistoryInput{ProjectSlug: f.project.Slug, MaxTokens: teamMemoryRecallMinTokens})
	require.NoError(t, err)
	require.Empty(t, output.Sources)
	require.Empty(t, output.History)
}

func TestZoomOpensEachLevelDownToSummariesAndMemos(t *testing.T) {
	t.Parallel()
	f := newTeamMemoryFixture(t)
	ctx := f.reader(t.Context())
	h := f.history(t, ctx)

	month, err := f.service.Zoom(ctx, f.principal, ZoomTeamMemoryInput{ProjectSlug: f.project.Slug, Source: "[digest:" + h.month.String() + "]"})
	require.NoError(t, err)
	require.Equal(t, "digest:"+h.month.String(), month.Item.Source.Ref())
	require.Equal(t, "month", month.Item.Level)
	require.Contains(t, month.Item.Text, "canary-month-digest")
	require.Equal(t, []string{"digest:" + h.week.String()}, itemRefs(month.Children))
	require.Contains(t, month.Children[0].Text, "canary-week-digest")

	week, err := f.service.Zoom(ctx, f.principal, ZoomTeamMemoryInput{ProjectSlug: f.project.Slug, Source: "digest:" + h.week.String()})
	require.NoError(t, err)
	require.Equal(t, []string{"digest:" + h.day.String()}, itemRefs(week.Children))

	day, err := f.service.Zoom(ctx, f.principal, ZoomTeamMemoryInput{ProjectSlug: f.project.Slug, Source: "digest:" + h.day.String()})
	require.NoError(t, err)
	require.Equal(t, []string{"chat:" + h.foldedChat.String()}, itemRefs(day.Children))
	require.Contains(t, day.Children[0].Text, "canary-folded-chat")

	chat, err := f.service.Zoom(ctx, f.principal, ZoomTeamMemoryInput{ProjectSlug: f.project.Slug, Source: "chat:" + h.freshChat.String()})
	require.NoError(t, err)
	require.Contains(t, chat.Item.Text, "Second paragraph stays out of recall.")
	require.Empty(t, chat.Children)

	memo, err := f.service.Zoom(ctx, f.principal, ZoomTeamMemoryInput{ProjectSlug: f.project.Slug, Source: "memo:" + h.memo.String()})
	require.NoError(t, err)
	require.Equal(t, "canary-memo exports must finish before 02:00 UTC.", memo.Item.Text)

	event, err := audittest.LatestAuditLogByAction(ctx, f.db, audit.ActionTeamMemoryRead)
	require.NoError(t, err)
	var metadata struct {
		Sources []string `json:"sources"`
	}
	require.NoError(t, json.Unmarshal(event.Metadata, &metadata))
	require.Equal(t, []string{"memo:" + h.memo.String()}, metadata.Sources)
}

func TestZoomRefusesInvalidAndForeignSources(t *testing.T) {
	t.Parallel()
	f := newTeamMemoryFixture(t)
	ctx := f.context(t.Context(), authz.NewGrant(authz.ScopeMemoryWrite, authz.WildcardResource))
	for _, source := range []string{"", "digest", "transcript:" + uuid.NewString(), "chat:not-a-uuid", "chat " + uuid.NewString()} {
		_, err := f.service.Zoom(ctx, f.principal, ZoomTeamMemoryInput{ProjectSlug: f.project.Slug, Source: source})
		require.ErrorIs(t, err, errTeamMemoryInvalid, source)
	}

	other, err := projectsrepo.New(f.db).CreateProject(ctx, projectsrepo.CreateProjectParams{OrganizationID: f.principal.OrganizationID, Name: "Other zoom project", Slug: "other-zoom-project"})
	require.NoError(t, err)
	foreign := f.summarizedChat(t, ctx, other.ID, "Foreign", "canary-foreign-chat", time.Now().Add(-time.Hour), uuid.NullUUID{})
	foreignMemo, err := f.service.Remember(ctx, f.principal, RememberInput{ProjectSlug: other.Slug, Content: "canary-foreign-memo"})
	require.NoError(t, err)
	for _, source := range []string{"chat:" + foreign.String(), foreignMemo.Memo.Source.Ref(), "digest:" + uuid.NewString()} {
		_, err := f.service.Zoom(ctx, f.principal, ZoomTeamMemoryInput{ProjectSlug: f.project.Slug, Source: source})
		require.ErrorIs(t, err, errTeamMemoryItemNotFound, source)
		result, ok := teamMemoryToolResult(err)
		require.True(t, ok)
		require.True(t, result.IsError)
	}
}
