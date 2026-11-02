package platformmcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/feature"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type teamMemoryFixture struct {
	db        *pgxpool.Pool
	principal Principal
	project   ResolvedProject
	flags     *feature.InMemory
	service   *TeamMemoryService
}

func newTeamMemoryFixture(t *testing.T) teamMemoryFixture {
	t.Helper()
	db, err := platformMCPInfra.CloneTestDatabase(t, "platform_team_memory")
	require.NoError(t, err)
	principal, project := seedRegistrationLifecycle(t, t.Context(), db)
	engine := authz.NewEngine(testenv.NewLogger(t), db, func(context.Context, string) (bool, error) { return false, nil }, nil)
	flags := &feature.InMemory{}
	flags.SetFlag(feature.FlagTeamMemory, principal.OrganizationID, true)
	service := NewTeamMemoryService(db, engine, audit.NewLogger(), flags, NewPostgresOrganizationSlugResolver(db), allowBudget(), NewChatSearchRedactor())
	require.NotNil(t, service)
	return teamMemoryFixture{db: db, principal: principal, project: project, flags: flags, service: service}
}

// context authenticates the fixture principal with grants on the project.
func (f teamMemoryFixture) context(ctx context.Context, grants ...authz.Grant) context.Context {
	actor := contextvalues.WithAuthenticatedActor(ctx, &contextvalues.AuthContext{ActiveOrganizationID: f.principal.OrganizationID, UserID: f.principal.UserID}, urn.NewPrincipal(urn.PrincipalTypeUser, f.principal.UserID))
	return ContextWithPrincipal(authz.GrantsToContext(actor, grants), f.principal)
}

func (f teamMemoryFixture) writer(ctx context.Context) context.Context {
	return f.context(ctx, authz.NewGrant(authz.ScopeMemoryWrite, f.project.ID.String()))
}

func (f teamMemoryFixture) remember(content string) RememberInput {
	return RememberInput{ProjectSlug: f.project.Slug, Content: content}
}

func TestRememberStoresRedactedMemoAttributedToCaller(t *testing.T) {
	t.Parallel()
	f := newTeamMemoryFixture(t)
	ctx := f.writer(t.Context())
	chatID := seedRecallChat(t, ctx, f.db, f.project.ID, f.principal.OrganizationID, "user_fixture_other_owner", "memo-"+uuid.NewString(), "Queue choice", uuid.NullUUID{})
	secret := "ghp_" + strings.Join([]string{"q1W2e3R4t5Y6", "u7I8o9P0a1S2", "d3F4g5H6j7K8"}, "")

	output, err := f.service.Remember(ctx, f.principal, RememberInput{ProjectSlug: f.project.Slug, ChatID: chatID.String(), Content: "  We chose the retry queue for webhooks. Deploy token " + secret + "  "})
	require.NoError(t, err)
	require.True(t, output.Redacted)
	require.Equal(t, TeamMemorySourceMemo, output.Memo.Source.Kind)
	require.Equal(t, chatID.String(), output.Memo.Source.ChatID)
	require.Equal(t, f.principal.UserID, output.Memo.AuthorUserID)
	require.True(t, strings.HasPrefix(output.Memo.Content, "We chose the retry queue for webhooks."))
	require.NotContains(t, output.Memo.Content, secret[:12])

	stored, err := testrepo.New(f.db).GetMemoryMemoFixture(ctx, testrepo.GetMemoryMemoFixtureParams{ID: uuid.MustParse(output.Memo.Source.ID), ProjectID: f.project.ID})
	require.NoError(t, err)
	require.Equal(t, output.Memo.Content, stored.Content)
	require.NotContains(t, stored.Content, secret[:12])
	require.Equal(t, f.principal.UserID, stored.AuthorUserID.String)
	require.Equal(t, chatID, stored.ChatID.UUID)

	event, err := audittest.LatestAuditLogByAction(ctx, f.db, audit.ActionMemoryMemoCreate)
	require.NoError(t, err)
	require.Equal(t, f.principal.UserID, event.ActorID)
	require.Equal(t, output.Memo.Source.ID, event.SubjectID)
	for _, recorded := range [][]byte{event.Metadata, event.BeforeSnapshot, event.AfterSnapshot, []byte(event.SubjectDisplay)} {
		require.NotContains(t, string(recorded), "retry queue")
	}
}

func TestRememberRequiresExplicitProjectMemoryWriteGrant(t *testing.T) {
	t.Parallel()
	f := newTeamMemoryFixture(t)
	other, err := projectsrepo.New(f.db).CreateProject(t.Context(), projectsrepo.CreateProjectParams{OrganizationID: f.principal.OrganizationID, Name: "Other memory project", Slug: "other-memory-project"})
	require.NoError(t, err)

	for _, grants := range [][]authz.Grant{
		{authz.NewGrant(authz.ScopeOrgAdmin, authz.WildcardResource)},
		{authz.NewGrant(authz.ScopeProjectWrite, authz.WildcardResource)},
		{authz.NewGrant(authz.ScopeChatWrite, authz.WildcardResource)},
		{authz.NewGrant(authz.ScopeMemoryRead, f.project.ID.String())},
		{authz.NewGrant(authz.ScopeMemoryWrite, other.ID.String())},
	} {
		_, err := f.service.Remember(f.context(t.Context(), grants...), f.principal, f.remember("denied fact"))
		var permission *ExternalAuthorizationError
		require.ErrorAs(t, err, &permission, grants[0].Scope)
		require.Equal(t, "memory:write", permission.RequiredScope)
	}

	ctx := f.writer(t.Context())
	assistant := f.principal
	assistant.Surface = SurfaceProjectAssistant
	_, err = f.service.Remember(ctx, assistant, f.remember("assistant fact"))
	require.ErrorIs(t, err, ErrForbidden)
	mismatch := f.principal
	mismatch.UserID = "user_fixture_wrong_actor"
	_, err = f.service.Remember(ctx, mismatch, f.remember("mismatch fact"))
	require.ErrorIs(t, err, ErrForbidden)
	limited := *f.service
	limited.budget = denyBudget()
	_, err = limited.Remember(ctx, f.principal, f.remember("limited fact"))
	require.Error(t, err)

	count, err := testrepo.New(f.db).CountMemoryMemosFixture(t.Context())
	require.NoError(t, err)
	require.Zero(t, count)
	audits, err := audittest.AuditLogCountByAction(t.Context(), f.db, audit.ActionMemoryMemoCreate)
	require.NoError(t, err)
	require.Zero(t, audits)
}

func TestRememberRefusesWhenFlagOffOrInputInvalid(t *testing.T) {
	t.Parallel()
	f := newTeamMemoryFixture(t)
	ctx := f.writer(t.Context())

	for _, input := range []RememberInput{
		{ProjectSlug: f.project.Slug, Content: "   "},
		{ProjectSlug: f.project.Slug, Content: strings.Repeat("x", memoMaxRunes+1)},
		{ProjectSlug: f.project.Slug, Content: "fact", ChatID: "not-a-uuid"},
		{Content: "no project"},
		{ProjectSlug: f.project.Slug, ProjectID: f.project.ID.String(), Content: "two projects"},
	} {
		_, err := f.service.Remember(ctx, f.principal, input)
		require.ErrorIs(t, err, errTeamMemoryInvalid)
	}

	// A chat from another project is not a valid source.
	other, err := projectsrepo.New(f.db).CreateProject(t.Context(), projectsrepo.CreateProjectParams{OrganizationID: f.principal.OrganizationID, Name: "Other memo chats", Slug: "other-memo-chats"})
	require.NoError(t, err)
	foreign := seedRecallChat(t, ctx, f.db, other.ID, f.principal.OrganizationID, "user_fixture_other_owner", "foreign-"+uuid.NewString(), "Foreign", uuid.NullUUID{})
	_, err = f.service.Remember(ctx, f.principal, RememberInput{ProjectSlug: f.project.Slug, Content: "fact", ChatID: foreign.String()})
	require.ErrorIs(t, err, ErrRiskReadNotFound)

	f.flags.SetFlag(feature.FlagTeamMemory, f.principal.OrganizationID, false)
	_, err = f.service.Remember(ctx, f.principal, f.remember("flag off"))
	require.ErrorIs(t, err, errTeamMemoryNotEnabled)

	count, err := testrepo.New(f.db).CountMemoryMemosFixture(t.Context())
	require.NoError(t, err)
	require.Zero(t, count)
}

func TestRememberFailsClosedWhenAuditFails(t *testing.T) {
	t.Parallel()
	f := newTeamMemoryFixture(t)
	ctx := f.writer(t.Context())
	require.NoError(t, audittest.RejectAction(ctx, f.db, audit.ActionMemoryMemoCreate))
	_, err := f.service.Remember(ctx, f.principal, f.remember("audit failure fact"))
	require.Error(t, err)
	count, err := testrepo.New(f.db).CountMemoryMemosFixture(t.Context())
	require.NoError(t, err)
	require.Zero(t, count)
}

// summarizedChat seeds a chat whose summary the background job wrote from the
// masked transcript.
func (f teamMemoryFixture) summarizedChat(t *testing.T, ctx context.Context, projectID uuid.UUID, title, summary string, at time.Time, account uuid.NullUUID) uuid.UUID {
	t.Helper()
	chatID := seedRecallChat(t, ctx, f.db, projectID, f.principal.OrganizationID, "user_fixture_other_owner", "memory-"+uuid.NewString(), title, account)
	require.NoError(t, testrepo.New(f.db).SetChatMemorySummaryFixture(ctx, testrepo.SetChatMemorySummaryFixtureParams{ID: chatID, Summary: conv.ToPGText(summary), SummaryGeneratedAt: conv.ToPGTimestamptz(at)}))
	return chatID
}

func (f teamMemoryFixture) reader(ctx context.Context) context.Context {
	return f.context(ctx, authz.NewGrant(authz.ScopeMemoryRead, f.project.ID.String()))
}

func (f teamMemoryFixture) search(query string) SearchTeamMemoryInput {
	return SearchTeamMemoryInput{ProjectSlug: f.project.Slug, Query: query}
}

func hitRefs(hits []TeamMemoryHit) []string {
	refs := make([]string, len(hits))
	for i, hit := range hits {
		refs[i] = hit.Source.Ref()
	}
	return refs
}

func TestSearchTeamMemoryFindsSummariesMemosAndDigests(t *testing.T) {
	t.Parallel()
	f := newTeamMemoryFixture(t)
	ctx := f.context(t.Context(), authz.NewGrant(authz.ScopeMemoryWrite, f.project.ID.String()))
	now := time.Now().UTC()
	chatID := f.summarizedChat(t, ctx, f.project.ID, "Webhook retries", "We moved webhook retries onto the durable queue because the in-memory one dropped jobs.\n\nSecond paragraph with deploy steps.", now.Add(-48*time.Hour), uuid.NullUUID{})
	memo, err := f.service.Remember(ctx, f.principal, RememberInput{ProjectSlug: f.project.Slug, ChatID: chatID.String(), Content: "Webhook retries use the durable queue, decided after dropped jobs."})
	require.NoError(t, err)
	digestID, err := testrepo.New(f.db).InsertMemoryDigestFixture(ctx, testrepo.InsertMemoryDigestFixtureParams{
		OrganizationID: f.principal.OrganizationID, ProjectID: f.project.ID, Level: "day",
		PeriodStart: pgtype.Date{Time: now.Add(-48 * time.Hour).Truncate(24 * time.Hour), InfinityModifier: pgtype.Finite, Valid: true},
		PeriodEnd:   pgtype.Date{Time: now.Add(-24 * time.Hour).Truncate(24 * time.Hour), InfinityModifier: pgtype.Finite, Valid: true},
		Content:     "The team settled webhook retries on the durable queue.",
	})
	require.NoError(t, err)
	f.summarizedChat(t, ctx, f.project.ID, "Unrelated", "Billing page copy changes.", now.Add(-time.Hour), uuid.NullUUID{})

	output, err := f.service.Search(f.reader(ctx), f.principal, f.search("webhook retries"))
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"chat:" + chatID.String(), memo.Memo.Source.Ref(), "digest:" + digestID.String()}, hitRefs(output.Hits))
	for _, hit := range output.Hits {
		switch hit.Source.Kind {
		case TeamMemorySourceChat:
			require.Contains(t, hit.Text, "webhook retries onto the durable queue")
			require.False(t, hit.Truncated)
		case TeamMemorySourceMemo:
			require.Equal(t, chatID.String(), hit.Source.ChatID)
			require.Equal(t, f.principal.UserID, hit.AuthorUserID)
		case TeamMemorySourceDigest:
			require.Equal(t, "day", hit.Level)
			require.NotEmpty(t, hit.PeriodStart)
		}
	}

	memosOnly := f.search("webhook retries")
	memosOnly.Kinds = []string{TeamMemorySourceMemo}
	output, err = f.service.Search(f.reader(ctx), f.principal, memosOnly)
	require.NoError(t, err)
	require.Equal(t, []string{memo.Memo.Source.Ref()}, hitRefs(output.Hits))

	output, err = f.service.Search(f.reader(ctx), f.principal, f.search("kubernetes"))
	require.NoError(t, err)
	require.Empty(t, output.Hits)

	event, err := audittest.LatestAuditLogByAction(ctx, f.db, audit.ActionTeamMemoryRead)
	require.NoError(t, err)
	require.Equal(t, f.project.ID.String(), event.SubjectID)
	require.Equal(t, f.principal.UserID, event.ActorID)
	count, err := audittest.AuditLogCountByAction(ctx, f.db, audit.ActionTeamMemoryRead)
	require.NoError(t, err)
	require.Equal(t, int64(3), count)
}

func TestSearchTeamMemoryHidesIneligibleAndForeignItems(t *testing.T) {
	t.Parallel()
	f := newTeamMemoryFixture(t)
	ctx := f.context(t.Context(), authz.NewGrant(authz.ScopeMemoryWrite, authz.WildcardResource))
	now := time.Now().UTC()
	at := now.Add(-time.Hour)

	visible := f.summarizedChat(t, ctx, f.project.ID, "Visible", "canary-visible decision about caching", at, uuid.NullUUID{})
	excluded := f.summarizedChat(t, ctx, f.project.ID, "Excluded", "canary-excluded decision about caching", at, uuid.NullUUID{})
	require.NoError(t, testrepo.New(f.db).ExcludeChatFromMemoryFixture(ctx, excluded))
	unmasked := f.summarizedChat(t, ctx, f.project.ID, "Unmasked", "canary-unmasked decision about caching", at, uuid.NullUUID{})
	require.NoError(t, testrepo.New(f.db).SetChatSummaryUnredactedFixture(ctx, unmasked))
	deleted := f.summarizedChat(t, ctx, f.project.ID, "Deleted", "canary-deleted decision about caching", at, uuid.NullUUID{})
	require.NoError(t, testrepo.New(f.db).ForceSoftDeleteChat(ctx, deleted))
	personal := seedRecallUserAccount(t, ctx, f.db, f.principal.OrganizationID, conv.ToPGText("personal"))
	f.summarizedChat(t, ctx, f.project.ID, "Personal", "canary-personal decision about caching", at, uuid.NullUUID{UUID: personal, Valid: true})
	unclassified := seedRecallUserAccount(t, ctx, f.db, f.principal.OrganizationID, pgtype.Text{})
	f.summarizedChat(t, ctx, f.project.ID, "Unclassified", "canary-unclassified decision about caching", at, uuid.NullUUID{UUID: unclassified, Valid: true})
	f.summarizedChat(t, ctx, f.project.ID, "Old", "canary-old decision about caching", now.Add(-200*24*time.Hour), uuid.NullUUID{})
	_, err := f.service.Remember(ctx, f.principal, RememberInput{ProjectSlug: f.project.Slug, ChatID: excluded.String(), Content: "canary-excluded-memo decision about caching"})
	require.NoError(t, err)

	other, err := projectsrepo.New(f.db).CreateProject(ctx, projectsrepo.CreateProjectParams{OrganizationID: f.principal.OrganizationID, Name: "Other memory project", Slug: "other-memory-search"})
	require.NoError(t, err)
	f.summarizedChat(t, ctx, other.ID, "Foreign", "canary-foreign decision about caching", at, uuid.NullUUID{})
	_, err = f.service.Remember(ctx, f.principal, RememberInput{ProjectSlug: other.Slug, Content: "canary-foreign-memo decision about caching"})
	require.NoError(t, err)

	output, err := f.service.Search(f.reader(ctx), f.principal, f.search("caching decision"))
	require.NoError(t, err)
	require.Equal(t, []string{"chat:" + visible.String()}, hitRefs(output.Hits))
	encoded, err := json.Marshal(output)
	require.NoError(t, err)
	require.Equal(t, 1, strings.Count(string(encoded), "canary-"))

	// A wider window reaches the old summary.
	wide := f.search("caching decision")
	wide.From = now.Add(-300 * 24 * time.Hour).Format(time.RFC3339)
	output, err = f.service.Search(f.reader(ctx), f.principal, wide)
	require.NoError(t, err)
	require.Len(t, output.Hits, 2)
}

func TestSearchTeamMemoryRequiresProjectMemoryReadGrant(t *testing.T) {
	t.Parallel()
	f := newTeamMemoryFixture(t)
	for _, grants := range [][]authz.Grant{
		{authz.NewGrant(authz.ScopeOrgAdmin, authz.WildcardResource)},
		{authz.NewGrant(authz.ScopeProjectWrite, authz.WildcardResource)},
		{authz.NewGrant(authz.ScopeChatRead, authz.WildcardResource)},
		{authz.NewGrant(authz.ScopeLogRead, f.project.ID.String())},
	} {
		_, err := f.service.Search(f.context(t.Context(), grants...), f.principal, f.search("anything"))
		var permission *ExternalAuthorizationError
		require.ErrorAs(t, err, &permission, grants[0].Scope)
		require.Equal(t, "memory:read", permission.RequiredScope)
	}
	// memory:write satisfies memory:read.
	_, err := f.service.Search(f.writer(t.Context()), f.principal, f.search("anything"))
	require.NoError(t, err)

	for _, input := range []SearchTeamMemoryInput{
		{ProjectSlug: f.project.Slug, Query: "  "},
		{ProjectSlug: f.project.Slug, Query: strings.Repeat("x", teamMemoryQueryMaxRunes+1)},
		{ProjectSlug: f.project.Slug, Query: "q", Kinds: []string{"transcript"}},
		{ProjectSlug: f.project.Slug, Query: "q", Limit: teamMemoryMaxLimit + 1},
		{ProjectSlug: f.project.Slug, Query: "q", From: "2026-01-01T00:00:00Z", To: "2027-06-01T00:00:00Z"},
		{ProjectSlug: f.project.Slug, Query: "q", From: "yesterday"},
	} {
		_, err := f.service.Search(f.reader(t.Context()), f.principal, input)
		require.ErrorIs(t, err, errTeamMemoryInvalid)
	}

	f.flags.SetFlag(feature.FlagTeamMemory, f.principal.OrganizationID, false)
	_, err = f.service.Search(f.reader(t.Context()), f.principal, f.search("anything"))
	require.ErrorIs(t, err, errTeamMemoryNotEnabled)
}

func TestSearchTeamMemoryRedactsOnTheWayOut(t *testing.T) {
	t.Parallel()
	f := newTeamMemoryFixture(t)
	ctx := f.reader(t.Context())
	secret := "ghp_" + strings.Join([]string{"q1W2e3R4t5Y6", "u7I8o9P0a1S2", "d3F4g5H6j7K8"}, "")
	// A stored summary or title that predates a masking rule is masked again.
	f.summarizedChat(t, ctx, f.project.ID, "Token "+secret, "Rotated the deploy token "+secret+" for the release pipeline.", time.Now().Add(-time.Hour), uuid.NullUUID{})
	output, err := f.service.Search(ctx, f.principal, f.search("release pipeline"))
	require.NoError(t, err)
	require.Len(t, output.Hits, 1)
	encoded, err := json.Marshal(output)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), secret[:12])
}

func TestSearchTeamMemoryFailsClosedWhenAuditFails(t *testing.T) {
	t.Parallel()
	f := newTeamMemoryFixture(t)
	ctx := f.reader(t.Context())
	require.NoError(t, audittest.RejectAction(ctx, f.db, audit.ActionTeamMemoryRead))
	f.summarizedChat(t, ctx, f.project.ID, "Audit", "audit failure summary", time.Now().Add(-time.Hour), uuid.NullUUID{})
	output, err := f.service.Search(ctx, f.principal, f.search("audit failure"))
	require.Error(t, err)
	require.Empty(t, output.Hits)
}

func TestTeamMemoryUnknownProjectRefusesLikeMissingGrant(t *testing.T) {
	t.Parallel()
	f := newTeamMemoryFixture(t)
	ctx := f.writer(t.Context())

	_, unknown := f.service.Remember(ctx, f.principal, RememberInput{ProjectSlug: "no-such-memory-project", Content: "fact"})
	var unknownDenied *ExternalAuthorizationError
	require.ErrorAs(t, unknown, &unknownDenied)

	other, err := projectsrepo.New(f.db).CreateProject(t.Context(), projectsrepo.CreateProjectParams{OrganizationID: f.principal.OrganizationID, Name: "Ungranted memory project", Slug: "ungranted-memory-project"})
	require.NoError(t, err)
	_, ungranted := f.service.Remember(ctx, f.principal, RememberInput{ProjectSlug: other.Slug, Content: "fact"})
	var ungrantedDenied *ExternalAuthorizationError
	require.ErrorAs(t, ungranted, &ungrantedDenied)

	unknownResult, ok := teamMemoryToolResult(unknown)
	require.True(t, ok)
	ungrantedResult, ok := teamMemoryToolResult(ungranted)
	require.True(t, ok)
	require.Equal(t, ungrantedResult.Content, unknownResult.Content)
}

func TestTeamMemoryBudgetChargedOnlyAfterGrantAndFlag(t *testing.T) {
	t.Parallel()
	f := newTeamMemoryFixture(t)
	limited := *f.service
	limited.budget = denyBudget()

	// A spent budget never masks a missing grant or a disabled flag.
	other, err := projectsrepo.New(f.db).CreateProject(t.Context(), projectsrepo.CreateProjectParams{OrganizationID: f.principal.OrganizationID, Name: "Unmetered memory project", Slug: "unmetered-memory-project"})
	require.NoError(t, err)
	ctx := f.writer(t.Context())
	_, err = limited.Remember(ctx, f.principal, RememberInput{ProjectSlug: other.Slug, Content: "ungranted"})
	var denied *ExternalAuthorizationError
	require.ErrorAs(t, err, &denied)

	f.flags.SetFlag(feature.FlagTeamMemory, f.principal.OrganizationID, false)
	_, err = limited.Remember(ctx, f.principal, f.remember("flag off"))
	require.ErrorIs(t, err, errTeamMemoryNotEnabled)
}

func TestSearchTeamMemoryDropsHitsMatchedOnlyByMaskedText(t *testing.T) {
	t.Parallel()
	f := newTeamMemoryFixture(t)
	ctx := f.reader(t.Context())
	secret := "ghp_" + strings.Join([]string{"z9X8c7V6b5N4", "m3L2k1J0h9G8", "f7D6s5A4p3O2"}, "")
	// The stored summary predates the masking rule, so the query matches the
	// raw token; masking on the way out leaves no evidence, so no hit.
	f.summarizedChat(t, ctx, f.project.ID, "Rotation", "Rotated "+secret+" yesterday.", time.Now().Add(-time.Hour), uuid.NullUUID{})
	output, err := f.service.Search(ctx, f.principal, f.search(secret))
	require.NoError(t, err)
	require.Empty(t, output.Hits)
}

func TestSearchTeamMemorySnippetShowsTheMatch(t *testing.T) {
	t.Parallel()
	f := newTeamMemoryFixture(t)
	ctx := f.reader(t.Context())
	summary := "Opening paragraph about release planning.\n\n" + strings.Repeat("Filler sentence about unrelated work. ", 80) + "Final call: the flaky checkout test was quarantined."
	f.summarizedChat(t, ctx, f.project.ID, "Release", summary, time.Now().Add(-time.Hour), uuid.NullUUID{})
	output, err := f.service.Search(ctx, f.principal, f.search("quarantined checkout"))
	require.NoError(t, err)
	require.Len(t, output.Hits, 1)
	require.True(t, output.Hits[0].Truncated)
	require.Contains(t, output.Hits[0].Text, "flaky checkout test was quarantined")
	require.NotContains(t, output.Hits[0].Text, "Opening paragraph")
}

func TestSnippetAnchorFallsBackToStem(t *testing.T) {
	t.Parallel()
	anchor, ok := snippetAnchor("We changed the retry policy.", queryTerms(`retries -cron OR "policy"`))
	require.True(t, ok)
	require.Equal(t, "policy", anchor)
	anchor, ok = snippetAnchor("We changed the retry budget.", queryTerms("retries"))
	require.True(t, ok)
	require.Equal(t, "retr", anchor)
	_, ok = snippetAnchor("Nothing relevant.", queryTerms("kubernetes"))
	require.False(t, ok)
}
