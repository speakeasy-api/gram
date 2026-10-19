package platformmcp

import (
	"context"
	"encoding/json"
	"slices"
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
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type chatSearchFixture struct {
	db        *pgxpool.Pool
	principal Principal
	project   ResolvedProject
	service   *ChatSearchService
	base      time.Time
}

func newChatSearchFixture(t *testing.T) chatSearchFixture {
	t.Helper()
	db, err := platformMCPInfra.CloneTestDatabase(t, "platform_chat_search")
	require.NoError(t, err)
	principal, project := seedRegistrationLifecycle(t, t.Context(), db)
	engine := authz.NewEngine(testenv.NewLogger(t), db, func(context.Context, string) (bool, error) { return false, nil }, nil)
	service := NewChatSearchService(db, engine, audit.NewLogger(), allowBudget(), "chat-search-test-key-material")
	require.NotNil(t, service)
	return chatSearchFixture{db: db, principal: principal, project: project, service: service, base: time.Date(2026, 1, 5, 12, 0, 0, 0, time.UTC)}
}

func (f chatSearchFixture) context(ctx context.Context) context.Context {
	actor := contextvalues.WithAuthenticatedActor(ctx, &contextvalues.AuthContext{ActiveOrganizationID: f.principal.OrganizationID, UserID: f.principal.UserID}, urn.NewPrincipal(urn.PrincipalTypeUser, f.principal.UserID))
	return ContextWithPrincipal(authz.GrantsToContext(actor, []authz.Grant{authz.NewGrant(authz.ScopeChatRead, authz.WildcardResource)}), f.principal)
}

func (f chatSearchFixture) search(query string) SearchChatsInput {
	return SearchChatsInput{ProjectID: f.project.ID.String(), From: f.base.Add(-time.Hour).Format(time.RFC3339), To: f.base.Add(time.Hour).Format(time.RFC3339), Query: query}
}

func (f chatSearchFixture) chat(t *testing.T, ctx context.Context, owner, title string) uuid.UUID {
	t.Helper()
	return seedRecallChat(t, ctx, f.db, f.project.ID, f.principal.OrganizationID, owner, "search-"+uuid.NewString(), title, uuid.NullUUID{})
}

func (f chatSearchFixture) participant(t *testing.T, ctx context.Context, chatID, messageID uuid.UUID, provider, providerUserID, userID string) {
	t.Helper()
	require.NoError(t, testrepo.New(f.db).SeedChatMessageParticipantFixture(ctx, testrepo.SeedChatMessageParticipantFixtureParams{
		ProjectID: f.project.ID, ChatID: uuid.NullUUID{UUID: chatID, Valid: true}, MessageID: uuid.NullUUID{UUID: messageID, Valid: true},
		Provider: provider, ProviderUserID: providerUserID, UserID: conv.ToPGTextEmpty(userID),
	}))
}

func hitIDs(hits []ChatMessageHit) []string {
	ids := make([]string, len(hits))
	for i, hit := range hits {
		ids[i] = hit.MessageID
	}
	return ids
}

// searchAll follows next_cursor to the end, failing if it does not terminate.
func (f chatSearchFixture) searchAll(t *testing.T, ctx context.Context, input SearchChatsInput) ([]string, int) {
	t.Helper()
	ids := []string{}
	for pages := 1; pages <= 50; pages++ {
		page, err := f.service.Search(ctx, f.principal, input)
		require.NoError(t, err)
		ids = append(ids, hitIDs(page.Hits)...)
		if page.NextCursor == "" {
			return ids, pages
		}
		input.Cursor = page.NextCursor
	}
	require.FailNow(t, "search did not terminate")
	return nil, 0
}

func TestChatSearchFindsOtherOwnersCommandsAndReadsContext(t *testing.T) {
	t.Parallel()
	f := newChatSearchFixture(t)
	ctx := f.context(t.Context())
	owner := "user_fixture_other_owner"
	chatID := f.chat(t, ctx, owner, "Manage workspace apps")
	seedRecallMessage(t, ctx, f.db, chatID, f.project.ID, recallMessageSeed{role: "user", content: "Please remove the obsolete test app", at: f.base.Add(-time.Minute)})
	args, err := json.Marshal(map[string]string{"command": "curl https://api.example.test/apps.manifest.delete --app-id APP_DEMO_42", "authorization": "Bearer fixture-short-secret"})
	require.NoError(t, err)
	calls, err := json.Marshal([]any{map[string]any{"id": "call_fixture_1", "type": "function", "function": map[string]any{"name": "Bash", "arguments": string(args)}}})
	require.NoError(t, err)
	anchor := seedRecallMessage(t, ctx, f.db, chatID, f.project.ID, recallMessageSeed{role: "assistant", content: "Checking the API", toolCalls: string(calls), at: f.base, source: "codex"})
	resultID := seedRecallMessage(t, ctx, f.db, chatID, f.project.ID, recallMessageSeed{role: "tool", content: `{"ok":true,"token":"fixture-result-secret"}`, at: f.base.Add(time.Minute)})
	seedRecallMessage(t, ctx, f.db, chatID, f.project.ID, recallMessageSeed{role: "assistant", content: "Compacted context", generation: 1, at: f.base.Add(2 * time.Minute)})

	output, err := f.service.Search(ctx, f.principal, f.search("APPS.MANIFEST.DELETE"))
	require.NoError(t, err)
	require.Len(t, output.Hits, 1)
	hit := output.Hits[0]
	require.Equal(t, owner, hit.OwnerUserID)
	require.Equal(t, "Manage workspace apps", hit.ChatTitle)
	require.Equal(t, anchor.String(), hit.MessageID)
	require.Equal(t, "assistant", hit.Role)
	require.Equal(t, "codex", hit.Source)
	require.Equal(t, f.base, hit.CreatedAt)
	require.Contains(t, hit.Text, "APP_DEMO_42")
	require.NotContains(t, hit.Text, "fixture-short-secret")
	require.Empty(t, output.NextCursor)
	event, err := audittest.LatestAuditLogByAction(ctx, f.db, audit.ActionChatSessionAccess)
	require.NoError(t, err)
	require.Equal(t, f.principal.UserID, event.ActorID)
	require.Equal(t, owner, event.SubjectSlug)
	require.Equal(t, chatID.String(), event.SubjectID)
	require.Equal(t, "platform_mcp", *event.ActingSurface)
	require.Empty(t, event.SubjectDisplay)

	read, err := f.service.Read(ctx, f.principal, ReadChatInput{ProjectID: f.project.ID.String(), ChatID: chatID.String(), MessageID: anchor.String()})
	require.NoError(t, err)
	require.Equal(t, "Manage workspace apps", read.ChatTitle)
	require.Equal(t, owner, read.OwnerUserID)
	require.Len(t, read.Messages, 4)
	require.Equal(t, resultID.String(), read.Messages[2].MessageID)
	require.Contains(t, read.Messages[2].Text, `"ok":true`)
	require.EqualValues(t, 1, read.Messages[3].Generation)
	encoded, err := json.Marshal(read)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "fixture-short-secret")
	require.NotContains(t, string(encoded), "fixture-result-secret")
	require.Contains(t, string(encoded), "[REDACTED]")

	latest, err := f.service.Read(ctx, f.principal, ReadChatInput{ProjectSlug: f.project.Slug, ChatID: chatID.String(), ContextSize: new(0)})
	require.NoError(t, err)
	require.Len(t, latest.Messages, 1)
	require.EqualValues(t, 1, latest.Messages[0].Generation)
	count, err := audittest.AuditLogCountByAction(ctx, f.db, audit.ActionChatSessionAccess)
	require.NoError(t, err)
	require.EqualValues(t, 3, count)
}

func TestChatSearchRequiresExplicitTranscriptGrant(t *testing.T) {
	t.Parallel()
	f := newChatSearchFixture(t)
	ctx := f.context(t.Context())
	chatID := f.chat(t, ctx, "user_fixture_other_owner", "grants")
	anchor := seedRecallMessage(t, ctx, f.db, chatID, f.project.ID, recallMessageSeed{role: "user", content: "needle", at: f.base})
	for _, scope := range []authz.Scope{authz.ScopeOrgAdmin, authz.ScopeProjectRead} {
		denied := authz.GrantsToContext(ctx, []authz.Grant{authz.NewGrant(scope, authz.WildcardResource)})
		_, err := f.service.Search(denied, f.principal, f.search("needle"))
		var permission *ExternalAuthorizationError
		require.ErrorAs(t, err, &permission)
		require.Equal(t, "chat:read", permission.RequiredScope)
		_, err = f.service.Read(denied, f.principal, ReadChatInput{ProjectID: f.project.ID.String(), ChatID: chatID.String(), MessageID: anchor.String()})
		require.ErrorAs(t, err, &permission)
	}
	// A project-scoped grant is not the unrestricted grant the tools require.
	projectOnly := authz.GrantsToContext(ctx, []authz.Grant{authz.NewGrant(authz.ScopeChatRead, f.project.ID.String())})
	_, err := f.service.Search(projectOnly, f.principal, f.search("needle"))
	var permission *ExternalAuthorizationError
	require.ErrorAs(t, err, &permission)
	assistant := f.principal
	assistant.Surface = SurfaceProjectAssistant
	_, err = f.service.Search(ctx, assistant, f.search("needle"))
	require.ErrorIs(t, err, ErrForbidden)
	mismatch := f.principal
	mismatch.UserID = "user_fixture_wrong_actor"
	_, err = f.service.Search(ctx, mismatch, f.search("needle"))
	require.ErrorIs(t, err, ErrForbidden)
	_, err = f.service.Search(ContextWithPrincipal(t.Context(), f.principal), f.principal, f.search("needle"))
	require.ErrorIs(t, err, ErrForbidden)
	limited := *f.service
	limited.budget = denyBudget()
	_, err = limited.Search(ctx, f.principal, f.search("needle"))
	require.Error(t, err)
	count, err := audittest.AuditLogCountByAction(ctx, f.db, audit.ActionChatSessionAccess)
	require.NoError(t, err)
	require.Zero(t, count)

	writeCtx := authz.GrantsToContext(ctx, []authz.Grant{authz.NewGrant(authz.ScopeChatWrite, authz.WildcardResource)})
	output, err := f.service.Search(writeCtx, f.principal, f.search("needle"))
	require.NoError(t, err)
	require.Equal(t, []string{anchor.String()}, hitIDs(output.Hits))
}

func TestChatSearchStaysInsideProjectOrganizationAndLiveRows(t *testing.T) {
	t.Parallel()
	f := newChatSearchFixture(t)
	ctx := f.context(t.Context())
	owner := "user_fixture_other_owner"
	chatID := f.chat(t, ctx, owner, "tenancy")
	anchor := seedRecallMessage(t, ctx, f.db, chatID, f.project.ID, recallMessageSeed{role: "user", content: "needle", at: f.base})
	want := []string{anchor.String()}
	// Every captured provider account type is in scope.
	for _, accountType := range []pgtype.Text{conv.ToPGText("personal"), {}} {
		account := seedRecallUserAccount(t, ctx, f.db, f.principal.OrganizationID, accountType)
		accountChat := seedRecallChat(t, ctx, f.db, f.project.ID, f.principal.OrganizationID, owner, "account-"+account.String(), "account", uuid.NullUUID{UUID: account, Valid: true})
		want = append(want, seedRecallMessage(t, ctx, f.db, accountChat, f.project.ID, recallMessageSeed{role: "user", content: "needle", at: f.base}).String())
	}

	other, err := projectsrepo.New(f.db).CreateProject(ctx, projectsrepo.CreateProjectParams{OrganizationID: f.principal.OrganizationID, Name: "Other fixture project", Slug: "other-fixture-project"})
	require.NoError(t, err)
	otherChat := seedRecallChat(t, ctx, f.db, other.ID, f.principal.OrganizationID, owner, "other-project", "needle", uuid.NullUUID{})
	otherAnchor := seedRecallMessage(t, ctx, f.db, otherChat, other.ID, recallMessageSeed{role: "user", content: "needle", at: f.base})
	deleted := f.chat(t, ctx, owner, "deleted")
	seedRecallMessage(t, ctx, f.db, deleted, f.project.ID, recallMessageSeed{role: "user", content: "needle", at: f.base})
	require.NoError(t, testrepo.New(f.db).ForceSoftDeleteChat(ctx, deleted))
	foreignPrincipal, foreignProject := seedRegistrationLifecycle(t, ctx, f.db)
	foreignChat := seedRecallChat(t, ctx, f.db, foreignProject.ID, foreignPrincipal.OrganizationID, foreignPrincipal.UserID, "other-tenant", "needle", uuid.NullUUID{})
	seedRecallMessage(t, ctx, f.db, foreignChat, foreignProject.ID, recallMessageSeed{role: "user", content: "needle", at: f.base})

	found, _ := f.searchAll(t, ctx, f.search("needle"))
	require.ElementsMatch(t, want, found)
	foreignSearch := f.search("needle")
	foreignSearch.ProjectID = foreignProject.ID.String()
	_, err = f.service.Search(ctx, f.principal, foreignSearch)
	require.ErrorIs(t, err, ErrRiskReadNotFound)
	for _, input := range []ReadChatInput{
		{ProjectID: f.project.ID.String(), ChatID: otherChat.String(), MessageID: otherAnchor.String()},
		{ProjectID: f.project.ID.String(), ChatID: chatID.String(), MessageID: otherAnchor.String()},
		{ProjectID: f.project.ID.String(), ChatID: deleted.String()},
		{ProjectID: foreignProject.ID.String(), ChatID: foreignChat.String()},
	} {
		_, err = f.service.Read(ctx, f.principal, input)
		require.ErrorIs(t, err, ErrRiskReadNotFound)
	}
	_, err = projectsrepo.New(f.db).DeleteProject(ctx, f.project.ID)
	require.NoError(t, err)
	_, err = f.service.Search(ctx, f.principal, f.search("needle"))
	require.ErrorIs(t, err, ErrRiskReadNotFound)
	_, err = f.service.Read(ctx, f.principal, ReadChatInput{ProjectID: f.project.ID.String(), ChatID: chatID.String()})
	require.ErrorIs(t, err, ErrRiskReadNotFound)
}

func TestChatSearchParticipantAndOwnerFilters(t *testing.T) {
	t.Parallel()
	f := newChatSearchFixture(t)
	ctx := f.context(t.Context())
	// A shared assistant chat is owned by one person while others post in it.
	shared := f.chat(t, ctx, "user_fixture_owner", "shared assistant")
	byOwner := seedRecallMessage(t, ctx, f.db, shared, f.project.ID, recallMessageSeed{role: "user", content: "participant_needle from owner", userID: "user_fixture_owner", at: f.base})
	byGuest := seedRecallMessage(t, ctx, f.db, shared, f.project.ID, recallMessageSeed{role: "user", content: "participant_needle from guest", at: f.base.Add(time.Second)})
	f.participant(t, ctx, shared, byGuest, "slack", "UEXAMPLE01", "user_fixture_guest")
	reply := seedRecallMessage(t, ctx, f.db, shared, f.project.ID, recallMessageSeed{role: "assistant", content: "participant_needle reply", at: f.base.Add(2 * time.Second)})
	other := f.chat(t, ctx, "user_fixture_other_owner", "other")
	unattributed := seedRecallMessage(t, ctx, f.db, other, f.project.ID, recallMessageSeed{role: "user", content: "participant_needle elsewhere", at: f.base})
	// A participant row is scoped to its own chat and message.
	f.participant(t, ctx, shared, unattributed, "slack", "UEXAMPLE02", "")

	cases := []struct {
		name              string
		owner             string
		participant       string
		provider          string
		want              []uuid.UUID
		emptyQueryMatches bool
	}{
		{name: "message user", participant: "user_fixture_owner", want: []uuid.UUID{byOwner}},
		{name: "recorded user", participant: "user_fixture_guest", want: []uuid.UUID{byGuest}},
		{name: "provider user", participant: "UEXAMPLE01", want: []uuid.UUID{byGuest}},
		{name: "provider restricted", participant: "UEXAMPLE01", provider: "slack", want: []uuid.UUID{byGuest}},
		{name: "other provider", participant: "UEXAMPLE01", provider: "github", want: nil},
		{name: "provider excludes message user", participant: "user_fixture_owner", provider: "slack", want: nil},
		{name: "mismatched chat", participant: "UEXAMPLE02", want: nil},
		{name: "owner", owner: "user_fixture_owner", want: []uuid.UUID{reply, byGuest, byOwner}},
		{name: "owner and participant", owner: "user_fixture_other_owner", participant: "UEXAMPLE01", want: nil},
	}
	for _, tc := range cases {
		input := f.search("participant_needle")
		input.OwnerUserID = tc.owner
		input.ParticipantID = tc.participant
		input.ParticipantProvider = tc.provider
		output, err := f.service.Search(ctx, f.principal, input)
		require.NoError(t, err, tc.name)
		want := []string{}
		for _, id := range tc.want {
			want = append(want, id.String())
		}
		require.Equal(t, want, hitIDs(output.Hits), tc.name)
		for _, hit := range output.Hits {
			require.Equal(t, shared.String(), hit.ChatID, tc.name)
			require.Equal(t, "user_fixture_owner", hit.OwnerUserID, tc.name)
		}
	}
	read, err := f.service.Read(ctx, f.principal, ReadChatInput{ProjectID: f.project.ID.String(), ChatID: shared.String(), MessageID: byGuest.String(), ContextSize: new(1)})
	require.NoError(t, err)
	require.Len(t, read.Messages, 3)
	require.Equal(t, "user_fixture_owner", read.Messages[0].UserID)
	require.Empty(t, read.Messages[0].Participants)
	require.Equal(t, []ChatParticipant{{Provider: "slack", ProviderUserID: "UEXAMPLE01", UserID: "user_fixture_guest", DisplayName: ""}}, read.Messages[1].Participants)
	_, err = f.service.Search(ctx, f.principal, SearchChatsInput{ProjectID: f.project.ID.String(), From: f.search("").From, To: f.search("").To, ParticipantProvider: "slack"})
	require.ErrorIs(t, err, errChatSearchInvalid)
}

func TestChatSearchSourceChatDateAndLiteralQueryFilters(t *testing.T) {
	t.Parallel()
	f := newChatSearchFixture(t)
	ctx := f.context(t.Context())
	first := f.chat(t, ctx, "user_fixture_owner", "Quarterly café review")
	second := f.chat(t, ctx, "user_fixture_owner", "other")
	percent := seedRecallMessage(t, ctx, f.db, first, f.project.ID, recallMessageSeed{role: "user", content: `progress 100%_done \ ok`, at: f.base, source: "codex"})
	plain := seedRecallMessage(t, ctx, f.db, first, f.project.ID, recallMessageSeed{role: "user", content: "progress 100xydone", at: f.base.Add(time.Second)})
	emoji := seedRecallMessage(t, ctx, f.db, second, f.project.ID, recallMessageSeed{role: "user", content: "deploy 🙂 naïve #", at: f.base.Add(2 * time.Second), source: "claude"})
	start := seedRecallMessage(t, ctx, f.db, second, f.project.ID, recallMessageSeed{role: "user", content: "window edge", at: f.base.Add(-time.Hour)})
	seedRecallMessage(t, ctx, f.db, second, f.project.ID, recallMessageSeed{role: "user", content: "window edge", at: f.base.Add(time.Hour)})
	seedRecallMessage(t, ctx, f.db, second, f.project.ID, recallMessageSeed{role: "user", content: "100%_done outside", at: f.base.Add(-2 * time.Hour)})

	cases := []struct {
		name    string
		query   string
		source  string
		chatIDs []string
		want    []uuid.UUID
	}{
		{name: "percent and underscore are literal", query: "100%_", want: []uuid.UUID{percent}},
		{name: "underscore alone", query: "_", want: []uuid.UUID{percent}},
		{name: "backslash", query: `\`, want: []uuid.UUID{percent}},
		{name: "case insensitive", query: "PROGRESS 100", want: []uuid.UUID{plain, percent}},
		{name: "unicode", query: "🙂 naïve", want: []uuid.UUID{emoji}},
		{name: "one punctuation character", query: "#", want: []uuid.UUID{emoji}},
		{name: "from inclusive to exclusive", query: "window edge", want: []uuid.UUID{start}},
		{name: "title matches its messages", query: "café", want: []uuid.UUID{plain, percent}},
		{name: "empty browses", want: []uuid.UUID{emoji, plain, percent, start}},
		{name: "source", source: "codex", want: []uuid.UUID{percent}},
		{name: "chat ids", chatIDs: []string{second.String(), second.String()}, want: []uuid.UUID{emoji, start}},
		{name: "chat ids and source", source: "claude", chatIDs: []string{first.String()}, want: nil},
	}
	for _, tc := range cases {
		input := f.search(tc.query)
		input.Source = tc.source
		input.ChatIDs = tc.chatIDs
		output, err := f.service.Search(ctx, f.principal, input)
		require.NoError(t, err, tc.name)
		want := []string{}
		for _, id := range tc.want {
			want = append(want, id.String())
		}
		require.Equal(t, want, hitIDs(output.Hits), tc.name)
	}
	invalid := []SearchChatsInput{
		{ProjectID: f.project.ID.String(), From: f.base.Add(-32 * 24 * time.Hour).Format(time.RFC3339), To: f.base.Format(time.RFC3339)},
		{ProjectID: f.project.ID.String(), From: f.base.Format(time.RFC3339), To: f.base.Format(time.RFC3339)},
		{ProjectID: f.project.ID.String(), From: "yesterday", To: f.base.Format(time.RFC3339)},
		{From: f.search("").From, To: f.search("").To},
		{ProjectID: f.project.ID.String(), From: f.search("").From, To: f.search("").To, Query: strings.Repeat("a", 257)},
		{ProjectID: f.project.ID.String(), From: f.search("").From, To: f.search("").To, Limit: chatSearchMaxLimit + 1},
		{ProjectID: f.project.ID.String(), From: f.search("").From, To: f.search("").To, ChatIDs: []string{"not-a-uuid"}},
		{ProjectID: f.project.ID.String(), From: f.search("").From, To: f.search("").To, SortOrder: "relevance"},
	}
	for _, input := range invalid {
		_, err := f.service.Search(ctx, f.principal, input)
		require.ErrorIs(t, err, errChatSearchInvalid)
	}
	unicode := f.search(strings.Repeat("🙂", 256))
	_, err := f.service.Search(ctx, f.principal, unicode)
	require.NoError(t, err)
}

func TestChatSearchKeysetPaginationAcrossChats(t *testing.T) {
	t.Parallel()
	f := newChatSearchFixture(t)
	ctx := f.context(t.Context())
	type seeded struct {
		id string
		at time.Time
	}
	messages := []seeded{}
	chats := []uuid.UUID{f.chat(t, ctx, "user_fixture_owner", "first"), f.chat(t, ctx, "user_fixture_owner", "second")}
	// Interleave chats in time and repeat timestamps across them, so the order
	// is global message time with the ID as tie-breaker, not chat grouping.
	for i, offset := range []time.Duration{0, 0, time.Second, 3 * time.Second, 3 * time.Second, 2 * time.Second} {
		at := f.base.Add(offset)
		id := seedRecallMessage(t, ctx, f.db, chats[i%2], f.project.ID, recallMessageSeed{role: "user", content: "page_needle", at: at})
		messages = append(messages, seeded{id: id.String(), at: at})
	}
	slices.SortFunc(messages, func(a, b seeded) int {
		if c := a.at.Compare(b.at); c != 0 {
			return c
		}
		return strings.Compare(a.id, b.id)
	})
	oldest := make([]string, len(messages))
	for i, message := range messages {
		oldest[i] = message.id
	}
	newest := slices.Clone(oldest)
	slices.Reverse(newest)
	for _, limit := range []int{1, 2, 4, 6} {
		for order, want := range map[string][]string{chatSearchSortOldestFirst: oldest, chatSearchSortNewestFirst: newest} {
			input := f.search("page_needle")
			input.SortOrder = order
			input.Limit = limit
			found, pages := f.searchAll(t, ctx, input)
			require.Equal(t, want, found, "%s limit %d", order, limit)
			// SQL reads one extra row, so a full last page has no empty follow-up.
			require.Equal(t, (len(want)+limit-1)/limit, pages, "%s limit %d", order, limit)
		}
	}

	input := f.search("page_needle")
	input.Limit = 1
	page, err := f.service.Search(ctx, f.principal, input)
	require.NoError(t, err)
	require.NotEmpty(t, page.NextCursor)
	for _, change := range []func(*SearchChatsInput){
		func(in *SearchChatsInput) { in.Query = "changed" },
		func(in *SearchChatsInput) { in.SortOrder = chatSearchSortOldestFirst },
		func(in *SearchChatsInput) { in.From = f.base.Add(-2 * time.Hour).Format(time.RFC3339) },
		func(in *SearchChatsInput) { in.ChatIDs = []string{chats[0].String()} },
	} {
		next := input
		next.Cursor = page.NextCursor
		change(&next)
		_, err = f.service.Search(ctx, f.principal, next)
		require.ErrorIs(t, err, ErrSubjectReferenceNotFound)
	}
	// The project selector and limit are not part of the cursor binding.
	next := input
	next.Cursor = page.NextCursor
	next.ProjectID = ""
	next.ProjectSlug = f.project.Slug
	next.Limit = 3
	continued, err := f.service.Search(ctx, f.principal, next)
	require.NoError(t, err)
	require.Equal(t, newest[1:4], hitIDs(continued.Hits))
	another := f.principal
	another.ConnectionID = uuid.NewString()
	next.Cursor = page.NextCursor
	_, err = f.service.Search(ctx, another, next)
	require.ErrorIs(t, err, ErrSubjectReferenceNotFound)
}

func TestChatSearchDropsRedactedMatchesWithoutStallingPages(t *testing.T) {
	t.Parallel()
	f := newChatSearchFixture(t)
	ctx := f.context(t.Context())
	chatID := f.chat(t, ctx, "user_fixture_owner", "redaction pages")
	for i := range 5 {
		seedRecallMessage(t, ctx, f.db, chatID, f.project.ID, recallMessageSeed{role: "tool", content: `{"token":"hidden_fixture_needle"}`, at: f.base.Add(time.Duration(i) * time.Second)})
	}
	visible := seedRecallMessage(t, ctx, f.db, chatID, f.project.ID, recallMessageSeed{role: "tool", content: `{"token":"hidden_fixture_needle","note":"hidden_fixture_needle shown"}`, at: f.base.Add(-time.Second)})
	input := f.search("hidden_fixture_needle")
	input.Limit = 2
	first, err := f.service.Search(ctx, f.principal, input)
	require.NoError(t, err)
	require.Empty(t, first.Hits)
	require.NotEmpty(t, first.NextCursor)
	found, pages := f.searchAll(t, ctx, input)
	require.Equal(t, []string{visible.String()}, found)
	require.Equal(t, 3, pages)
	// A dropped hit is not audited as accessed.
	count, err := audittest.AuditLogCountByAction(ctx, f.db, audit.ActionChatSessionAccess)
	require.NoError(t, err)
	require.EqualValues(t, 1, count)
}

func TestChatSearchRedactsBeforeTruncatingAndOmitsAttachments(t *testing.T) {
	t.Parallel()
	f := newChatSearchFixture(t)
	ctx := f.context(t.Context())
	chatID := f.chat(t, ctx, "user_fixture_owner", "redaction")
	// Construct a synthetic provider credential so this fixture cannot be used.
	secret := "ghp_" + strings.Join([]string{"q1W2e3R4t5Y6", "u7I8o9P0a1S2", "d3F4g5H6j7K8"}, "")
	// The snippet window starts inside the credential, so truncating before
	// redaction would expose part of it.
	long := strings.Repeat("🙂", 2000) + " credential=" + secret + " " + strings.Repeat("x", 376) + " snippet_needle " + strings.Repeat("y", 2000)
	longID := seedRecallMessage(t, ctx, f.db, chatID, f.project.ID, recallMessageSeed{role: "tool", content: long, at: f.base})
	page, err := f.service.Search(ctx, f.principal, f.search("snippet_needle"))
	require.NoError(t, err)
	require.Len(t, page.Hits, 1)
	require.True(t, page.Hits[0].Truncated)
	require.Contains(t, page.Hits[0].Text, "snippet_needle")
	require.LessOrEqual(t, len([]rune(page.Hits[0].Text)), chatSearchSnippetRunes)
	require.NotContains(t, page.Hits[0].Text, secret[:12])
	require.NotContains(t, page.Hits[0].Text, secret[len(secret)-12:])
	read, err := f.service.Read(ctx, f.principal, ReadChatInput{ProjectID: f.project.ID.String(), ChatID: chatID.String(), MessageID: longID.String(), ContextSize: new(0)})
	require.NoError(t, err)
	require.True(t, read.Messages[0].Truncated)
	require.Len(t, []rune(read.Messages[0].Text), chatReadTextRunes)

	payload := `{"usage":{"prompt_tokens":12,"api_key":"fixture-api-key"},"id":1234567890123456789012345,` +
		`"parts":[{"type":"text","text":"attachment_needle"},{"type":"image","source":{"data":"aW1hZ2UtYnl0ZXM="}}],` +
		`"args":{"type":"file","path":"/tmp/report.txt","operation":"delete"}}`
	attachment := seedRecallMessage(t, ctx, f.db, chatID, f.project.ID, recallMessageSeed{role: "user", content: "attachment_needle", contentRaw: payload, at: f.base.Add(time.Second)})
	browserArgs, err := json.Marshal(map[string]any{"actions": []any{map[string]any{"name": "navigate", "input": map[string]any{"url": "https://apps.example.test/settings/APP_FIXTURE", "authorization": "fixture-nested-credential"}}}})
	require.NoError(t, err)
	calls, err := json.Marshal([]any{map[string]any{"function": map[string]any{"name": "browser_batch", "arguments": string(browserArgs)}}})
	require.NoError(t, err)
	browser := seedRecallMessage(t, ctx, f.db, chatID, f.project.ID, recallMessageSeed{role: "assistant", toolCalls: string(calls), at: f.base.Add(2 * time.Second)})
	read, err = f.service.Read(ctx, f.principal, ReadChatInput{ProjectID: f.project.ID.String(), ChatID: chatID.String(), MessageID: attachment.String(), ContextSize: new(1)})
	require.NoError(t, err)
	require.Len(t, read.Messages, 3)
	text := read.Messages[1].Text
	require.Equal(t, attachment.String(), read.Messages[1].MessageID)
	require.Contains(t, text, `"prompt_tokens":12`)
	require.Contains(t, text, "1234567890123456789012345")
	require.Contains(t, text, "[WITHHELD_ATTACHMENT]")
	require.NotContains(t, text, "aW1hZ2UtYnl0ZXM=")
	require.NotContains(t, text, "fixture-api-key")
	require.Contains(t, text, `"path":"/tmp/report.txt"`)
	require.Contains(t, text, `"operation":"delete"`)
	require.Equal(t, browser.String(), read.Messages[2].MessageID)
	require.Contains(t, read.Messages[2].Text, "https://apps.example.test/settings/APP_FIXTURE")
	require.NotContains(t, read.Messages[2].Text, "fixture-nested-credential")

	// Stored jsonb normalizes exponents, so check number preservation directly.
	safe, changed, err := f.service.redactText(ctx, `{"big":1e1000,"token":"fixture-token"}`)
	require.NoError(t, err)
	require.True(t, changed)
	require.Contains(t, safe, `"big":1e1000`)
	require.NotContains(t, safe, "fixture-token")

	// Fields too large to return are neither matched nor shown.
	oversized := seedRecallMessage(t, ctx, f.db, chatID, f.project.ID, recallMessageSeed{role: "user", content: strings.Repeat("z", 131073) + "oversized_needle", at: f.base.Add(3 * time.Second)})
	page, err = f.service.Search(ctx, f.principal, f.search("oversized_needle"))
	require.NoError(t, err)
	require.Empty(t, page.Hits)
	read, err = f.service.Read(ctx, f.principal, ReadChatInput{ProjectID: f.project.ID.String(), ChatID: chatID.String(), MessageID: oversized.String(), ContextSize: new(0)})
	require.NoError(t, err)
	require.True(t, read.Messages[0].Truncated)
	require.Empty(t, read.Messages[0].Text)
}

func TestChatSearchReadWindowAndBounds(t *testing.T) {
	t.Parallel()
	f := newChatSearchFixture(t)
	ctx := f.context(t.Context())
	chatID := f.chat(t, ctx, "user_fixture_owner", "window")
	ids := []string{}
	for i := range 9 {
		// Stored times run backwards: the window follows capture sequence.
		ids = append(ids, seedRecallMessage(t, ctx, f.db, chatID, f.project.ID, recallMessageSeed{role: "user", content: "window", at: f.base.Add(-time.Duration(i) * time.Second)}).String())
	}
	read, err := f.service.Read(ctx, f.principal, ReadChatInput{ProjectID: f.project.ID.String(), ChatID: chatID.String(), MessageID: ids[4], ContextSize: new(2)})
	require.NoError(t, err)
	got := []string{}
	for _, message := range read.Messages {
		got = append(got, message.MessageID)
	}
	require.Equal(t, ids[2:7], got)
	read, err = f.service.Read(ctx, f.principal, ReadChatInput{ProjectID: f.project.ID.String(), ChatID: chatID.String()})
	require.NoError(t, err)
	require.Len(t, read.Messages, chatReadDefaultContext+1)
	require.Equal(t, ids[8], read.Messages[len(read.Messages)-1].MessageID)
	for _, input := range []ReadChatInput{
		{ProjectID: f.project.ID.String(), ChatID: chatID.String(), ContextSize: new(chatReadMaxContext + 1)},
		{ProjectID: f.project.ID.String(), ChatID: chatID.String(), ContextSize: new(-1)},
		{ProjectID: f.project.ID.String(), ChatID: "not-a-uuid"},
		{ProjectID: f.project.ID.String(), ChatID: chatID.String(), MessageID: "not-a-uuid"},
	} {
		_, err = f.service.Read(ctx, f.principal, input)
		require.ErrorIs(t, err, errChatSearchInvalid)
	}
}

func TestChatSearchFailsClosedWhenAuditFails(t *testing.T) {
	t.Parallel()
	f := newChatSearchFixture(t)
	ctx := f.context(t.Context())
	// Install rejection before any access event exists in this isolated DB.
	require.NoError(t, audittest.RejectAction(ctx, f.db, audit.ActionChatSessionAccess))
	chatID := f.chat(t, ctx, "user_fixture_other_owner", "audit failure")
	anchor := seedRecallMessage(t, ctx, f.db, chatID, f.project.ID, recallMessageSeed{role: "user", content: "audit-failure-needle", at: f.base})
	output, err := f.service.Search(ctx, f.principal, f.search("audit-failure-needle"))
	require.Error(t, err)
	require.Empty(t, output.Hits)
	read, err := f.service.Read(ctx, f.principal, ReadChatInput{ProjectID: f.project.ID.String(), ChatID: chatID.String(), MessageID: anchor.String()})
	require.Error(t, err)
	require.Empty(t, read.Messages)
}

func TestChatSearchRedactsMetadataBeforeTruncating(t *testing.T) {
	t.Parallel()
	f := newChatSearchFixture(t)
	ctx := f.context(t.Context())
	secret := "ghp_" + strings.Join([]string{"q1W2e3R4t5Y6", "u7I8o9P0a1S2", "d3F4g5H6j7K8"}, "")
	chatID := f.chat(t, ctx, "user_fixture_owner", strings.Repeat("t", 1000)+" "+secret)
	message := seedRecallMessage(t, ctx, f.db, chatID, f.project.ID, recallMessageSeed{role: "user", content: "metadata_needle", source: strings.Repeat("s", 104) + " " + secret, at: f.base})
	f.participant(t, ctx, chatID, message, "slack", secret, "")
	page, err := f.service.Search(ctx, f.principal, f.search("metadata_needle"))
	require.NoError(t, err)
	require.Len(t, page.Hits, 1)
	read, err := f.service.Read(ctx, f.principal, ReadChatInput{ProjectID: f.project.ID.String(), ChatID: chatID.String(), MessageID: message.String(), ContextSize: new(0)})
	require.NoError(t, err)
	require.Len(t, read.Messages, 1)
	require.Len(t, read.Messages[0].Participants, 1)
	for _, value := range []string{page.Hits[0].ChatTitle, page.Hits[0].Source, read.ChatTitle, read.Messages[0].Source, read.Messages[0].Participants[0].ProviderUserID} {
		require.NotContains(t, value, secret[:20])
		require.Contains(t, value, "[REDACTED]")
	}
}

func TestChatSearchKeepsNumericUsageCounters(t *testing.T) {
	t.Parallel()
	f := newChatSearchFixture(t)
	text, _, err := f.service.redactText(t.Context(), `{"prompt_tokens":12,"max_tokens":4096,"secret_tokens":123456,"access_tokens":456789,"number":1e1000}`)
	require.NoError(t, err)
	require.Contains(t, text, `"prompt_tokens":12`)
	require.Contains(t, text, `"max_tokens":4096`)
	require.Contains(t, text, "1e1000")
	require.NotContains(t, text, "123456")
	require.NotContains(t, text, "456789")
}
