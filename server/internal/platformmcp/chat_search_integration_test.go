package platformmcp

import (
	"context"
	"encoding/json"
	"fmt"
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

type chatEvidenceFixture struct {
	db        *pgxpool.Pool
	principal Principal
	project   ResolvedProject
	service   *ChatSearchService
	base      time.Time
}

func newChatEvidenceFixture(t *testing.T) chatEvidenceFixture {
	t.Helper()
	db, err := platformMCPInfra.CloneTestDatabase(t, "platform_chat_evidence")
	require.NoError(t, err)
	principal, project := seedRegistrationLifecycle(t, t.Context(), db)
	engine := authz.NewEngine(testenv.NewLogger(t), db, func(context.Context, string) (bool, error) { return false, nil }, nil)
	service := NewChatSearchService(db, engine, audit.NewLogger(), allowBudget(), "chat-evidence-test-key-material")
	require.NotNil(t, service)
	return chatEvidenceFixture{db: db, principal: principal, project: project, service: service, base: time.Date(2026, 1, 5, 12, 0, 0, 0, time.UTC)}
}

func (f chatEvidenceFixture) context(ctx context.Context) context.Context {
	actor := contextvalues.WithAuthenticatedActor(ctx, &contextvalues.AuthContext{ActiveOrganizationID: f.principal.OrganizationID, UserID: f.principal.UserID}, urn.NewPrincipal(urn.PrincipalTypeUser, f.principal.UserID))
	return ContextWithPrincipal(authz.GrantsToContext(actor, []authz.Grant{authz.NewGrant(authz.ScopeChatRead, authz.WildcardResource)}), f.principal)
}

func (f chatEvidenceFixture) search(query string) SearchChatsInput {
	return SearchChatsInput{ProjectID: f.project.ID.String(), From: f.base.Add(-time.Hour).Format(time.RFC3339), To: f.base.Add(time.Hour).Format(time.RFC3339), Query: query}
}

func TestChatEvidenceFindsOtherOwnersCommandsAndReadsResults(t *testing.T) {
	t.Parallel()
	f := newChatEvidenceFixture(t)
	ctx := f.context(t.Context())
	owner := "user_fixture_other_owner"
	chatID := seedRecallChat(t, ctx, f.db, f.project.ID, f.principal.OrganizationID, owner, "evidence-shell-session", "Manage workspace apps", uuid.NullUUID{})
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
	require.Equal(t, owner, output.Hits[0].OwnerUserID)
	require.Equal(t, anchor.String(), output.Hits[0].MessageID)
	require.Contains(t, output.Hits[0].Text, "APP_DEMO_42")
	require.NotContains(t, output.Hits[0].Text, "fixture-short-secret")
	require.Empty(t, output.NextCursor)
	require.Contains(t, output.Coverage, "does not prove")
	event, err := audittest.LatestAuditLogByAction(ctx, f.db, audit.ActionChatSessionAccess)
	require.NoError(t, err)
	require.Equal(t, f.principal.UserID, event.ActorID)
	require.Equal(t, owner, event.SubjectSlug)
	require.Equal(t, chatID.String(), event.SubjectID)
	require.Equal(t, "platform_mcp", *event.ActingSurface)
	require.Empty(t, event.SubjectDisplay)

	read, err := f.service.Read(ctx, f.principal, ReadChatInput{ProjectID: f.project.ID.String(), ChatID: chatID.String(), MessageID: anchor.String()})
	require.NoError(t, err)
	require.Len(t, read.Messages, 4)
	require.Equal(t, resultID.String(), read.Messages[2].MessageID)
	require.Contains(t, read.Messages[2].Text, `"ok":true`)
	require.EqualValues(t, 1, read.Messages[3].Generation)
	encoded, err := json.Marshal(read)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "fixture-short-secret")
	require.NotContains(t, string(encoded), "fixture-result-secret")
	require.Contains(t, string(encoded), "[REDACTED]")
	listing := f.search("")
	listing.From = time.Now().Add(-time.Hour).Format(time.RFC3339)
	listing.To = time.Now().Add(time.Hour).Format(time.RFC3339)
	listed, err := f.service.Search(ctx, f.principal, listing)
	require.NoError(t, err)
	require.Len(t, listed.Hits, 1)
	require.Equal(t, "session", listed.Hits[0].Role)
	require.Contains(t, listed.Hits[0].Text, "Manage workspace apps")
	require.Empty(t, listed.Hits[0].MessageID)
	// A title-only listing can open a session without first knowing a message.
	latest, err := f.service.Read(ctx, f.principal, ReadChatInput{ProjectSlug: f.project.Slug, ChatID: chatID.String(), ContextSize: new(0)})
	require.NoError(t, err)
	require.Len(t, latest.Messages, 1)
	require.EqualValues(t, 1, latest.Messages[0].Generation)
}

func TestChatEvidenceEnforcesTranscriptGrantAndScope(t *testing.T) {
	t.Parallel()
	f := newChatEvidenceFixture(t)
	ctx := f.context(t.Context())
	chatID := seedRecallChat(t, ctx, f.db, f.project.ID, f.principal.OrganizationID, "user_fixture_other_owner", "evidence-boundaries", "needle", uuid.NullUUID{})
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
	count, err := audittest.AuditLogCountByAction(ctx, f.db, audit.ActionChatSessionAccess)
	require.NoError(t, err)
	require.Zero(t, count)
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
	writeCtx := authz.GrantsToContext(ctx, []authz.Grant{authz.NewGrant(authz.ScopeChatWrite, authz.WildcardResource)})
	output, err := f.service.Search(writeCtx, f.principal, f.search("needle"))
	require.NoError(t, err)
	require.Len(t, output.Hits, 2)

	other, err := projectsrepo.New(f.db).CreateProject(ctx, projectsrepo.CreateProjectParams{OrganizationID: f.principal.OrganizationID, Name: "Other fixture project", Slug: "other-fixture-project"})
	require.NoError(t, err)
	otherChat := seedRecallChat(t, ctx, f.db, other.ID, f.principal.OrganizationID, "user_fixture_other_owner", "evidence-other-project", "needle", uuid.NullUUID{})
	otherAnchor := seedRecallMessage(t, ctx, f.db, otherChat, other.ID, recallMessageSeed{role: "user", content: "needle", at: f.base})
	// Neither session IDs nor message IDs cross the exact selected project.
	_, err = f.service.Read(ctx, f.principal, ReadChatInput{ProjectID: f.project.ID.String(), ChatID: otherChat.String(), MessageID: otherAnchor.String()})
	require.ErrorIs(t, err, ErrRiskReadNotFound)
	_, err = f.service.Read(ctx, f.principal, ReadChatInput{ProjectID: f.project.ID.String(), ChatID: chatID.String(), MessageID: otherAnchor.String()})
	require.ErrorIs(t, err, ErrRiskReadNotFound)
	foreignPrincipal, foreignProject := seedRegistrationLifecycle(t, ctx, f.db)
	foreignChat := seedRecallChat(t, ctx, f.db, foreignProject.ID, foreignPrincipal.OrganizationID, foreignPrincipal.UserID, "evidence-other-tenant", "needle", uuid.NullUUID{})
	seedRecallMessage(t, ctx, f.db, foreignChat, foreignProject.ID, recallMessageSeed{role: "user", content: "needle", at: f.base})
	foreignSearch := f.search("needle")
	foreignSearch.ProjectID = foreignProject.ID.String()
	_, err = f.service.Search(ctx, f.principal, foreignSearch)
	require.ErrorIs(t, err, ErrRiskReadNotFound)
	output, err = f.service.Search(ctx, f.principal, f.search("needle"))
	require.NoError(t, err)
	require.Len(t, output.Hits, 2)
	for _, hit := range output.Hits {
		require.Equal(t, chatID.String(), hit.ChatID)
	}
}

func TestChatEvidencePaginationRedactionAndAccountCoverage(t *testing.T) {
	t.Parallel()
	f := newChatEvidenceFixture(t)
	ctx := f.context(t.Context())
	owner := "user_fixture_other_owner"
	allowed := seedRecallChat(t, ctx, f.db, f.project.ID, f.principal.OrganizationID, owner, "evidence-pages", "Fixture session", uuid.NullUUID{})
	ids := []string{}
	for i := range 3 {
		id := seedRecallMessage(t, ctx, f.db, allowed, f.project.ID, recallMessageSeed{role: "user", content: "literal_needle.%", at: f.base.Add(time.Duration(i) * time.Second)})
		ids = append(ids, id.String())
	}
	for _, accountType := range []pgtype.Text{conv.ToPGText("personal"), {}} {
		account := seedRecallUserAccount(t, ctx, f.db, f.principal.OrganizationID, accountType)
		chatID := seedRecallChat(t, ctx, f.db, f.project.ID, f.principal.OrganizationID, owner, "account-"+account.String(), "Account coverage", uuid.NullUUID{UUID: account, Valid: true})
		anchor := seedRecallMessage(t, ctx, f.db, chatID, f.project.ID, recallMessageSeed{role: "user", content: "account_coverage_needle", at: f.base})
		read, err := f.service.Read(ctx, f.principal, ReadChatInput{ProjectID: f.project.ID.String(), ChatID: chatID.String(), MessageID: anchor.String()})
		require.NoError(t, err)
		require.Equal(t, anchor.String(), read.Messages[0].MessageID)
		page, err := f.service.Search(ctx, f.principal, f.search("account_coverage_needle"))
		require.NoError(t, err)
		byChat := map[string]bool{}
		for _, hit := range page.Hits {
			byChat[hit.ChatID] = true
		}
		require.True(t, byChat[chatID.String()])
	}
	deleted := seedRecallChat(t, ctx, f.db, f.project.ID, f.principal.OrganizationID, owner, "evidence-deleted", "literal_needle.%", uuid.NullUUID{})
	seedRecallMessage(t, ctx, f.db, deleted, f.project.ID, recallMessageSeed{role: "user", content: "literal_needle.%", at: f.base})
	require.NoError(t, testrepo.New(f.db).ForceSoftDeleteChat(ctx, deleted))
	input := f.search("literal_needle.%")
	input.Limit = 1
	found := []string{}
	for {
		page, err := f.service.Search(ctx, f.principal, input)
		require.NoError(t, err)
		require.Len(t, page.Hits, 1)
		found = append(found, page.Hits[0].MessageID)
		if page.NextCursor == "" {
			break
		}
		require.Less(t, len(found), 4)
		next := input
		next.Cursor = page.NextCursor
		next.Query = "changed"
		_, err = f.service.Search(ctx, f.principal, next)
		require.ErrorIs(t, err, ErrSubjectReferenceNotFound)
		another := f.principal
		another.ConnectionID = uuid.NewString()
		_, err = f.service.Search(ctx, another, SearchChatsInput{ProjectID: input.ProjectID, From: input.From, To: input.To, Query: input.Query, Cursor: page.NextCursor})
		require.ErrorIs(t, err, ErrSubjectReferenceNotFound)
		input.Cursor = page.NextCursor
	}
	require.Equal(t, []string{ids[2], ids[1], ids[0]}, found)
	// A query must still match after credential redaction. Empty pages can
	// carry a continuation when the bounded raw candidate scan is exhausted.
	for i := range chatSearchScanLimit + 1 {
		seedRecallMessage(t, ctx, f.db, allowed, f.project.ID, recallMessageSeed{role: "tool", content: `{"token":"hidden_fixture_needle"}`, at: f.base.Add(time.Duration(i) * time.Millisecond)})
	}
	hidden := f.search("hidden_fixture_needle")
	page, err := f.service.Search(ctx, f.principal, hidden)
	require.NoError(t, err)
	require.Empty(t, page.Hits)
	require.NotEmpty(t, page.NextCursor)
	absent, err := f.service.Search(ctx, f.principal, f.search("definitely_absent_fixture"))
	require.NoError(t, err)
	require.Equal(t, page.Progress, absent.Progress)
	require.NotEmpty(t, absent.NextCursor)
	hidden.Cursor = page.NextCursor
	page, err = f.service.Search(ctx, f.principal, hidden)
	require.NoError(t, err)
	require.Empty(t, page.Hits)
	require.Empty(t, page.NextCursor)
}

func TestChatEvidenceFailsClosedWhenAuditFails(t *testing.T) {
	t.Parallel()
	f := newChatEvidenceFixture(t)
	ctx := f.context(t.Context())
	// Install rejection before any access event exists in this isolated DB.
	require.NoError(t, audittest.RejectAction(ctx, f.db, audit.ActionChatSessionAccess))
	chatID := seedRecallChat(t, ctx, f.db, f.project.ID, f.principal.OrganizationID, "user_fixture_other_owner", "evidence-audit-failure", "Fixture", uuid.NullUUID{})
	anchor := seedRecallMessage(t, ctx, f.db, chatID, f.project.ID, recallMessageSeed{role: "user", content: "audit-failure-needle", at: f.base})
	output, err := f.service.Search(ctx, f.principal, f.search("audit-failure-needle"))
	require.Error(t, err)
	require.Empty(t, output.Hits)
	read, err := f.service.Read(ctx, f.principal, ReadChatInput{ProjectID: f.project.ID.String(), ChatID: chatID.String(), MessageID: anchor.String()})
	require.Error(t, err)
	require.Empty(t, read.Messages)
}

func TestChatEvidenceBoundsAndSecretScanner(t *testing.T) {
	t.Parallel()
	f := newChatEvidenceFixture(t)
	ctx := f.context(t.Context())
	// Construct a synthetic provider credential so this fixture cannot be used.
	secret := "ghp_" + strings.Join([]string{"q1W2e3R4t5Y6", "u7I8o9P0a1S2", "d3F4g5H6j7K8"}, "")
	safe, err := f.service.redact(ctx, "credential="+secret, 0)
	require.NoError(t, err)
	require.NotContains(t, safe, secret)
	require.Contains(t, safe, "[REDACTED]")
	chatID := seedRecallChat(t, ctx, f.db, f.project.ID, f.principal.OrganizationID, "user_fixture_other_owner", "evidence-bounds", "Fixture", uuid.NullUUID{})
	anchor := seedRecallMessage(t, ctx, f.db, chatID, f.project.ID, recallMessageSeed{role: "user", content: strings.Repeat("z", 131073) + "needle", at: f.base, contentAssetURL: "https://assets.example.test/offloaded"})
	output, err := f.service.Search(ctx, f.principal, f.search("needle"))
	require.NoError(t, err)
	require.Empty(t, output.Hits)
	read, err := f.service.Read(ctx, f.principal, ReadChatInput{ProjectID: f.project.ID.String(), ChatID: chatID.String(), MessageID: anchor.String(), ContextSize: new(0)})
	require.NoError(t, err)
	require.Len(t, read.Messages, 1)
	require.True(t, read.Messages[0].Withheld)
	require.True(t, read.Messages[0].Offloaded)
	invalid := f.search("needle")
	invalid.From = f.base.Add(-32 * 24 * time.Hour).Format(time.RFC3339)
	_, err = f.service.Search(ctx, f.principal, invalid)
	require.ErrorIs(t, err, errChatEvidenceInvalid)
	invalid = f.search("needle")
	invalid.ProjectID = ""
	_, err = f.service.Search(ctx, f.principal, invalid)
	require.ErrorIs(t, err, errChatEvidenceInvalid)
	for _, text := range []string{strings.Repeat("🙂", 1201), strings.Repeat("Ⱥ", 1200) + "needle"} {
		snippet, truncated := evidenceSnippet(text, "needle", 1200)
		require.True(t, truncated)
		require.LessOrEqual(t, len([]rune(snippet)), 1200)
	}
}

func TestChatEvidenceProgressiveSortAndNarrowing(t *testing.T) {
	t.Parallel()
	f := newChatEvidenceFixture(t)
	ctx := f.context(t.Context())
	chats := []string{}
	messages := [][]string{}
	for _, name := range []string{"first", "second"} {
		chat := seedRecallChat(t, ctx, f.db, f.project.ID, f.principal.OrganizationID, "user_fixture_owner", name, "current metadata", uuid.NullUUID{})
		chats = append(chats, chat.String())
		ids := []string{}
		// Equal timestamps require the complete sequence/ID tie-breaker.
		for range 3 {
			id := seedRecallMessage(t, ctx, f.db, chat, f.project.ID, recallMessageSeed{role: "tool", content: "sort_needle", at: f.base, source: "codex"})
			ids = append(ids, id.String())
		}
		seedRecallMessage(t, ctx, f.db, chat, f.project.ID, recallMessageSeed{role: "tool", content: "sort_needle", at: f.base.Add(time.Hour), source: "codex"})
		messages = append(messages, ids)
	}
	for _, order := range []string{chatSearchSortNewestFirst, chatSearchSortOldestFirst} {
		input := f.search("sort_needle")
		input.SortOrder = order
		input.Limit = 1
		found := []string{}
		for range 10 {
			page, err := f.service.Search(ctx, f.principal, input)
			require.NoError(t, err)
			for _, hit := range page.Hits {
				found = append(found, hit.MessageID)
			}
			require.Equal(t, page.NextCursor == "", page.Progress.Complete)
			if page.NextCursor == "" {
				break
			}
			input.Cursor = page.NextCursor
		}
		want := []string{messages[0][0], messages[0][1], messages[0][2], messages[1][0], messages[1][1], messages[1][2]}
		if order == chatSearchSortNewestFirst {
			slices.Reverse(want)
		}
		require.Equal(t, want, found)
	}
	input := f.search("sort_needle")
	input.Limit = 1
	input.ChatIDs = []string{chats[0]}
	input.Source = "codex"
	first, err := f.service.Search(ctx, f.principal, input)
	require.NoError(t, err)
	require.Len(t, first.Hits, 1)
	input.Cursor = first.NextCursor
	input.SortOrder = chatSearchSortNewestFirst
	_, err = f.service.Search(ctx, f.principal, input)
	require.NoError(t, err)
	input.SortOrder = chatSearchSortOldestFirst
	_, err = f.service.Search(ctx, f.principal, input)
	require.ErrorIs(t, err, ErrSubjectReferenceNotFound)
	// Browse sessions by historical captured activity even when current metadata
	// was updated later. Source narrows eligibility, not the current title text.
	browse := f.search("")
	browse.Source = "codex"
	browse.ChatIDs = []string{chats[0]}
	page, err := f.service.Search(ctx, f.principal, browse)
	require.NoError(t, err)
	require.Len(t, page.Hits, 1)
	require.Equal(t, "session", page.Hits[0].Role)
	require.True(t, page.Progress.Complete)
	browse.Source = "unknown-fixture-source"
	page, err = f.service.Search(ctx, f.principal, browse)
	require.NoError(t, err)
	require.Empty(t, page.Hits)
}

func TestChatEvidenceDecodedCommandsAndRecoverableChunks(t *testing.T) {
	t.Parallel()
	f := newChatEvidenceFixture(t)
	ctx := f.context(t.Context())
	chat := seedRecallChat(t, ctx, f.db, f.project.ID, f.principal.OrganizationID, "user_fixture_owner", "decoded", "metadata", uuid.NullUUID{})
	safe, err := f.service.redact(ctx, `{"number":1e1000,"token":"fixture-short-credential"}`, 0)
	require.NoError(t, err)
	require.NotContains(t, safe, "fixture-short-credential")
	require.Contains(t, safe, "1e1000")
	require.Contains(t, canonicalEvidenceField(`{"id":1234567890123456789012345}`), "1234567890123456789012345")
	command := "printf \"quoted value\"\nnext-command"
	args, err := json.Marshal(map[string]string{"command": command, "token": "fixture-hidden-value"})
	require.NoError(t, err)
	calls, err := json.Marshal([]any{map[string]any{"function": map[string]any{"arguments": string(args)}}})
	require.NoError(t, err)
	seedRecallMessage(t, ctx, f.db, chat, f.project.ID, recallMessageSeed{role: "assistant", toolCalls: string(calls), at: f.base})
	for _, query := range []string{"printf \"quoted value\"", "\nnext-command", `"command":"printf`} {
		page, err := f.service.Search(ctx, f.principal, f.search(query))
		require.NoError(t, err)
		require.Len(t, page.Hits, 1)
		require.NotContains(t, page.Hits[0].Text, "fixture-hidden-value")
	}
	long := strings.Repeat("🙂 plain evidence ", 400) + "end-of-record"
	anchor := seedRecallMessage(t, ctx, f.db, chat, f.project.ID, recallMessageSeed{role: "tool", content: long, at: f.base.Add(-time.Minute)})
	input := ReadChatInput{ProjectID: f.project.ID.String(), ChatID: chat.String(), MessageID: anchor.String(), ContextSize: new(0)}
	var text strings.Builder
	for range 20 {
		page, err := f.service.Read(ctx, f.principal, input)
		require.NoError(t, err)
		require.Len(t, page.Messages, 1)
		hit := page.Messages[0]
		require.Equal(t, input.TextOffset, hit.TextOffset)
		text.WriteString(hit.Text)
		if hit.NextTextOffset == nil {
			break
		}
		require.Greater(t, *hit.NextTextOffset, input.TextOffset)
		input.TextOffset = *hit.NextTextOffset
		input.ExpectedTextVersion = hit.TextVersion
	}
	require.Equal(t, strings.TrimSpace(long), strings.TrimSpace(text.String()))
	input.ExpectedTextVersion = strings.Repeat("0", 64)
	_, err = f.service.Read(ctx, f.principal, input)
	require.ErrorIs(t, err, errChatEvidenceInvalid)
	// Read context uses capture sequence even when stored timestamps go backwards.
	page, err := f.service.Read(ctx, f.principal, ReadChatInput{ProjectID: f.project.ID.String(), ChatID: chat.String(), MessageID: anchor.String()})
	require.NoError(t, err)
	require.Len(t, page.Messages, 2)
	require.Less(t, page.Messages[0].Sequence, page.Messages[1].Sequence)
	require.True(t, page.Messages[0].At.After(page.Messages[1].At))
}

func TestChatEvidenceContinuesAcrossSessionAndByteBudgets(t *testing.T) {
	t.Parallel()
	f := newChatEvidenceFixture(t)
	ctx := f.context(t.Context())
	for i := range chatSearchSessionLimit + 2 {
		chat := seedRecallChat(t, ctx, f.db, f.project.ID, f.principal.OrganizationID, "user_fixture_owner", fmt.Sprintf("batch-%d", i), "browse_needle", uuid.NullUUID{})
		seedRecallMessage(t, ctx, f.db, chat, f.project.ID, recallMessageSeed{role: "user", content: "historical event", at: f.base})
	}
	browse := f.search("")
	browse.Limit = 20
	count := 0
	for range 10 {
		page, err := f.service.Search(ctx, f.principal, browse)
		require.NoError(t, err)
		count += len(page.Hits)
		require.Equal(t, page.NextCursor == "", page.Progress.Complete)
		if page.NextCursor == "" {
			break
		}
		browse.Cursor = page.NextCursor
	}
	require.Equal(t, chatSearchSessionLimit+2, count)
	for _, order := range []string{chatSearchSortNewestFirst, chatSearchSortOldestFirst} {
		noMatch := f.search("no_such_fixture_text")
		noMatch.SortOrder = order
		first, err := f.service.Search(ctx, f.principal, noMatch)
		require.NoError(t, err)
		require.Empty(t, first.Hits)
		require.NotEmpty(t, first.NextCursor)
		require.Equal(t, "session_budget", first.Progress.StopReason)
		require.Equal(t, chatSearchSessionLimit, first.Progress.SessionsExamined)
		noMatch.Cursor = first.NextCursor
		final, err := f.service.Search(ctx, f.principal, noMatch)
		require.NoError(t, err)
		require.Empty(t, final.Hits)
		require.True(t, final.Progress.Complete)
		require.Equal(t, 2, final.Progress.SessionsExamined)
	}
	chat := seedRecallChat(t, ctx, f.db, f.project.ID, f.principal.OrganizationID, "user_fixture_owner", "bytes", "metadata", uuid.NullUUID{})
	for range 34 {
		seedRecallMessage(t, ctx, f.db, chat, f.project.ID, recallMessageSeed{role: "tool", content: strings.Repeat("x", 131072), at: f.base})
	}
	input := f.search("absent_in_large_payloads")
	input.ChatIDs = []string{chat.String()}
	page, err := f.service.Search(ctx, f.principal, input)
	require.NoError(t, err)
	require.Empty(t, page.Hits)
	require.NotEmpty(t, page.NextCursor)
	require.Equal(t, "byte_budget", page.Progress.StopReason)
	require.LessOrEqual(t, page.Progress.PayloadBytes, int64(chatSearchByteLimit))
	input.Cursor = page.NextCursor
	last, err := f.service.Search(ctx, f.principal, input)
	require.NoError(t, err)
	require.True(t, last.Progress.Complete)
	require.Equal(t, 34, page.Progress.MessagesExamined+last.Progress.MessagesExamined)
}

func TestChatEvidenceHeaderThenMessagesAcrossPages(t *testing.T) {
	t.Parallel()
	f := newChatEvidenceFixture(t)
	ctx := f.context(t.Context())
	chat := seedRecallChat(t, ctx, f.db, f.project.ID, f.principal.OrganizationID, "user_fixture_owner", "header-pages", "shared_fixture_needle", uuid.NullUUID{})
	older := seedRecallMessage(t, ctx, f.db, chat, f.project.ID, recallMessageSeed{role: "user", content: "shared_fixture_needle", at: f.base})
	newer := seedRecallMessage(t, ctx, f.db, chat, f.project.ID, recallMessageSeed{role: "tool", content: "shared_fixture_needle", at: f.base.Add(time.Second)})
	for _, order := range []string{chatSearchSortNewestFirst, chatSearchSortOldestFirst} {
		input := f.search("shared_fixture_needle")
		input.SortOrder = order
		input.Limit = 1
		ids := []string{}
		for range 4 {
			page, err := f.service.Search(ctx, f.principal, input)
			require.NoError(t, err)
			for _, hit := range page.Hits {
				ids = append(ids, hit.MessageID)
			}
			if page.NextCursor == "" {
				break
			}
			input.Cursor = page.NextCursor
		}
		want := []string{"", older.String(), newer.String()}
		if order == chatSearchSortNewestFirst {
			want = []string{"", newer.String(), older.String()}
		}
		require.Equal(t, want, ids)
	}
}

func TestChatEvidenceSearchesNestedBrowserActions(t *testing.T) {
	t.Parallel()
	f := newChatEvidenceFixture(t)
	ctx := f.context(t.Context())
	chat := seedRecallChat(t, ctx, f.db, f.project.ID, f.principal.OrganizationID, "user_fixture_owner", "browser-evidence", "Browser activity", uuid.NullUUID{})
	args, err := json.Marshal(map[string]any{"actions": []any{map[string]any{"name": "navigate", "input": map[string]any{"url": "https://apps.example.test/settings/APP_FIXTURE", "authorization": "fixture-nested-credential"}}}})
	require.NoError(t, err)
	calls, err := json.Marshal([]any{map[string]any{"function": map[string]any{"name": "browser_batch", "arguments": string(args)}}})
	require.NoError(t, err)
	anchor := seedRecallMessage(t, ctx, f.db, chat, f.project.ID, recallMessageSeed{role: "assistant", toolCalls: string(calls), at: f.base})
	page, err := f.service.Search(ctx, f.principal, f.search("apps.example.test/settings/APP_FIXTURE"))
	require.NoError(t, err)
	require.Len(t, page.Hits, 1)
	require.Equal(t, anchor.String(), page.Hits[0].MessageID)
	read, err := f.service.Read(ctx, f.principal, ReadChatInput{ProjectID: f.project.ID.String(), ChatID: chat.String(), MessageID: anchor.String(), ContextSize: new(0)})
	require.NoError(t, err)
	require.Contains(t, read.Messages[0].Text, "https://apps.example.test/settings/APP_FIXTURE")
	require.NotContains(t, read.Messages[0].Text, "fixture-nested-credential")
	require.Contains(t, read.Messages[0].Text, "[REDACTED]")
	require.False(t, read.Messages[0].Withheld)
}
