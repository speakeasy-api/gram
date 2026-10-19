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
	require.Len(t, output.Hits, 1)

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
	require.Len(t, output.Hits, 1)
	require.Equal(t, chatID.String(), output.Hits[0].ChatID)
}

func TestChatEvidencePaginationRedactionAndExclusions(t *testing.T) {
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
		chatID := seedRecallChat(t, ctx, f.db, f.project.ID, f.principal.OrganizationID, owner, "excluded-"+account.String(), "literal_needle.%", uuid.NullUUID{UUID: account, Valid: true})
		anchor := seedRecallMessage(t, ctx, f.db, chatID, f.project.ID, recallMessageSeed{role: "user", content: "literal_needle.%", at: f.base})
		_, err := f.service.Read(ctx, f.principal, ReadChatInput{ProjectID: f.project.ID.String(), ChatID: chatID.String(), MessageID: anchor.String()})
		require.ErrorIs(t, err, ErrRiskReadNotFound)
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
	require.ElementsMatch(t, ids, found)
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
