package aiintegrations

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"

	"github.com/speakeasy-api/gram/server/internal/chat"
	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	anthropicapi "github.com/speakeasy-api/gram/server/internal/thirdparty/anthropic"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

func complianceFeedDiscovery(externalChatID, userAgent string) discoveredChat {
	return discoveredChat{
		externalChatID: externalChatID,
		title:          "",
		createdAt:      time.Date(2026, 7, 14, 9, 1, 0, 0, time.UTC),
		updatedAt:      time.Date(2026, 7, 14, 9, 1, 0, 0, time.UTC),
		userEmail:      "ada@example.com",
		externalUserID: "anthropic_user_1",
		client: chatClient{
			source:    complianceSourceFromUserAgent(userAgent),
			userAgent: userAgent,
			ipAddress: "203.0.113.9",
		},
		hasClient:   true,
		chatsCursor: "",
		cursorOnly:  false,
	}
}

func complianceListDiscovery(externalChatID, title, chatsCursor string) discoveredChat {
	return discoveredChat{
		externalChatID: externalChatID,
		title:          title,
		createdAt:      time.Date(2026, 7, 14, 9, 1, 0, 0, time.UTC),
		updatedAt:      time.Date(2026, 7, 14, 9, 30, 0, 0, time.UTC),
		userEmail:      "",
		externalUserID: "",
		client:         chatClient{source: "", userAgent: "", ipAddress: ""},
		hasClient:      false,
		chatsCursor:    chatsCursor,
		cursorOnly:     false,
	}
}

// complianceImportFixture is a project, a connected user, and an enabled
// Anthropic compliance config the import can write into.
func complianceImportFixture(t *testing.T) (context.Context, *ComplianceImportService, *Store, Config, uuid.UUID, string) {
	t.Helper()

	ctx, conn, store, orgID := newStoreTestDB(t)

	project, err := projectsrepo.New(conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
		Name:           "Compliance Import Test Project",
		Slug:           "project-" + uuid.NewString()[:8],
		OrganizationID: orgID,
	})
	require.NoError(t, err)

	userRow, err := usersrepo.New(conn).UpsertUser(ctx, usersrepo.UpsertUserParams{
		ID:          "user_" + uuid.NewString(),
		Email:       "ada@example.com",
		DisplayName: "Ada",
		PhotoUrl:    conv.ToPGTextEmpty(""),
		Admin:       false,
	})
	require.NoError(t, err)
	require.NoError(t, testrepo.New(conn).CreateOrganizationUserRelationshipFixture(ctx, testrepo.CreateOrganizationUserRelationshipFixtureParams{
		OrganizationID: orgID,
		UserID:         conv.ToPGText(userRow.ID),
	}))

	extOrgID := "ext-org"
	watermark := time.Now().UTC().Add(-initialUsagePollLookback)
	created := upsertConfigWithTx(t, ctx, conn, store, orgID, ProviderAnthropicCompliance, "anthropic-key", true, true, &extOrgID, &watermark)
	cfg := created.Config
	cfg.ProjectID = project.ID

	writer, shutdown := chat.NewChatMessageWriter(testenv.NewLogger(t), conn, nil)
	t.Cleanup(func() { _ = shutdown(context.Background()) })

	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), []string{})
	require.NoError(t, err)
	svc := NewComplianceImportService(testenv.NewLogger(t), conn, policy, writer, func(context.Context, string, int) {})

	return ctx, svc, store, cfg, project.ID, userRow.ID
}

func TestUpsertDiscoveredChatPreservesResolvedUserOnUnresolvedRevisit(t *testing.T) {
	t.Parallel()

	ctx, svc, _, cfg, projectID, userID := complianceImportFixture(t)
	resolver := newConnectedUserResolver(svc.db, cfg.OrganizationID)

	// A created activity resolves the actor email to a connected user.
	chatID, _, err := svc.upsertDiscoveredChat(ctx, cfg, complianceFeedDiscovery("chat_ext_1", "Mozilla/5.0"), resolver)
	require.NoError(t, err)

	chatRow, err := chatrepo.New(svc.db).GetChat(ctx, chatrepo.GetChatParams{ID: chatID, ProjectID: projectID})
	require.NoError(t, err)
	require.Equal(t, userID, chatRow.UserID.String)

	// A chat-list revisit whose entry carries no identity (the creator is
	// no longer a member, so user is null) must not clobber the resolved
	// user.
	sameChatID, _, err := svc.upsertDiscoveredChat(ctx, cfg, complianceListDiscovery("chat_ext_1", "Renamed chat", ""), resolver)
	require.NoError(t, err)
	require.Equal(t, chatID, sameChatID)

	chatRow, err = chatrepo.New(svc.db).GetChat(ctx, chatrepo.GetChatParams{ID: chatID, ProjectID: projectID})
	require.NoError(t, err)
	require.Equal(t, userID, chatRow.UserID.String, "resolved user must survive an unresolved revisit")
	require.Equal(t, "anthropic_user_1", chatRow.ExternalUserID.String, "external user id must survive an unresolved revisit")
	require.Equal(t, "Renamed chat", chatRow.Title.String, "the list's title is authoritative")
}

// complianceMessagesServer serves one chat's messages per after_id cursor.
// Each entry is the page returned for that cursor; a missing cursor fails
// the test.
func complianceMessagesServer(t *testing.T, pages map[string]map[string]anthropicapi.ChatMessagesPage) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		const prefix = "/v1/compliance/apps/chats/"
		if !strings.HasPrefix(r.URL.Path, prefix) || !strings.HasSuffix(r.URL.Path, "/messages") {
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		externalChatID := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, prefix), "/messages")
		page, ok := pages[externalChatID][r.URL.Query().Get("after_id")]
		if !ok {
			t.Errorf("unexpected messages request for %s after_id=%q", externalChatID, r.URL.Query().Get("after_id"))
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(page)
	}))
	t.Cleanup(server.Close)
	return server
}

func complianceMessagesPage(externalChatID, name, lastID string, messages ...anthropicapi.ChatMessage) anthropicapi.ChatMessagesPage {
	return anthropicapi.ChatMessagesPage{
		ID:               externalChatID,
		Name:             name,
		CreatedAt:        "2026-07-14T09:01:00Z",
		UpdatedAt:        "2026-07-14T09:30:00Z",
		DeletedAt:        nil,
		Href:             "https://claude.ai/chat/" + externalChatID,
		Model:            new("claude-opus-4-8"),
		OrganizationID:   "",
		OrganizationUUID: "",
		ProjectID:        "",
		User:             anthropicapi.ChatUser{ID: "anthropic_user_1", EmailAddress: "ada@example.com"},
		Messages:         messages,
		HasMore:          false,
		FirstID:          "",
		LastID:           lastID,
	}
}

func complianceMessage(id, role, text, createdAt string) anthropicapi.ChatMessage {
	return anthropicapi.ChatMessage{
		ID:             id,
		Role:           role,
		CreatedAt:      createdAt,
		Content:        json.RawMessage(`[{"type":"text","text":"` + text + `"}]`),
		Files:          nil,
		GeneratedFiles: nil,
		Artifacts:      nil,
	}
}

// importedChatID resolves the chat row the import wrote for an external
// chat id. The upsert is idempotent and carries no identity or title, so it
// returns the existing row untouched.
func importedChatID(ctx context.Context, t *testing.T, svc *ComplianceImportService, cfg Config, externalChatID string) uuid.UUID {
	t.Helper()

	chatID, _, err := svc.upsertDiscoveredChat(ctx, cfg, complianceListDiscovery(externalChatID, "", ""), newConnectedUserResolver(svc.db, cfg.OrganizationID))
	require.NoError(t, err)
	return chatID
}

// runComplianceImport drives the fetch and write stages over the given
// discoveries, the way SyncAnthropicCompliance does once discovery is done.
func runComplianceImport(ctx context.Context, t *testing.T, svc *ComplianceImportService, client *anthropicapi.Client, cfg Config, discoveries ...discoveredChat) *ComplianceSyncProgress {
	t.Helper()

	progress := complianceDiscoveryProgress(false)
	in := make(chan discoveredChat, len(discoveries))
	for _, d := range discoveries {
		in <- d
	}
	close(in)

	pages := make(chan messagePageBatch, anthropicComplianceMessagePageBufferSize)
	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() error {
		defer close(pages)
		return svc.importDiscoveredChats(gctx, client, cfg, in, pages, progress)
	})
	g.Go(func() error {
		return svc.writeMessagePages(gctx, cfg, pages, progress)
	})
	require.NoError(t, g.Wait())
	return progress
}

func TestImportDiscoveredChatsKeepsClientAcrossListRevisits(t *testing.T) {
	t.Parallel()

	ctx, svc, store, cfg, projectID, userID := complianceImportFixture(t)

	const desktopUA = "Mozilla/5.0 Claude/1.2.3 Electron/39.0.0"
	server := complianceMessagesServer(t, map[string]map[string]anthropicapi.ChatMessagesPage{
		"chat_desktop": {
			// First visit: the whole chat so far.
			"": complianceMessagesPage("chat_desktop", "Desktop chat", "msg_cur_2",
				complianceMessage("msg_1", "user", "hello", "2026-07-14T09:01:00Z"),
				complianceMessage("msg_2", "assistant", "hi", "2026-07-14T09:01:05Z"),
			),
			// Revisit after the chat received a new message.
			"msg_cur_2": complianceMessagesPage("chat_desktop", "Desktop chat", "msg_cur_3",
				complianceMessage("msg_3", "user", "one more thing", "2026-07-14T09:30:00Z"),
			),
		},
		"chat_unseen": {
			"": complianceMessagesPage("chat_unseen", "Web chat", "msg_cur_9",
				complianceMessage("msg_9", "user", "hey", "2026-07-14T09:02:00Z"),
			),
		},
	})
	client := anthropicapi.New(svc.guardianPolicy, anthropicapi.WithBaseURL(server.URL), anthropicapi.WithAPIKey("anthropic-key"))

	// Run one: the desktop chat arrives from the activity feed with its
	// client identity; a chat whose created activity was never seen arrives
	// from the chat list only.
	progress := runComplianceImport(ctx, t, svc, client, cfg,
		complianceFeedDiscovery("chat_desktop", desktopUA),
		complianceListDiscovery("chat_unseen", "Web chat", "list_cur_1"),
	)
	require.Equal(t, 2, progress.ChatsImported)
	require.Equal(t, 2, progress.MessagePagesWritten)
	require.Equal(t, "chats:list_cur_1", progress.CursorPersisted)

	desktopChatID := importedChatID(ctx, t, svc, cfg, "chat_desktop")
	desktopChat, err := chatrepo.New(svc.db).GetChat(ctx, chatrepo.GetChatParams{ID: desktopChatID, ProjectID: projectID})
	require.NoError(t, err)
	require.Equal(t, "Desktop chat", desktopChat.Title.String, "the first message page supplies the feed's title")
	require.Equal(t, userID, desktopChat.UserID.String)

	messages, err := chatrepo.New(svc.db).ListChatMessages(ctx, chatrepo.ListChatMessagesParams{ChatID: desktopChatID, ProjectID: projectID})
	require.NoError(t, err)
	require.Len(t, messages, 2)
	for _, msg := range messages {
		require.Equal(t, "claude", msg.Source.String)
		require.Equal(t, desktopUA, msg.UserAgent.String)
		require.Equal(t, "203.0.113.9", msg.IpAddress.String)
	}

	unseenChatID := importedChatID(ctx, t, svc, cfg, "chat_unseen")
	unseenMessages, err := chatrepo.New(svc.db).ListChatMessages(ctx, chatrepo.ListChatMessagesParams{ChatID: unseenChatID, ProjectID: projectID})
	require.NoError(t, err)
	require.Len(t, unseenMessages, 1)
	require.Equal(t, "claude-chat-web", unseenMessages[0].Source.String, "a chat first seen in the list has no user agent to classify")
	require.False(t, unseenMessages[0].UserAgent.Valid)

	reloaded, err := store.GetUsagePollConfig(ctx, cfg.ID, ScheduleAnthropicCompliance)
	require.NoError(t, err)
	require.Equal(t, "chats:list_cur_1", reloaded.LastCursor)

	// Run two: the desktop chat reappears in the chat list because it
	// received a message. Only the new message is fetched, past the stored
	// per-chat cursor, and it keeps the desktop identity its created
	// activity established.
	progress = runComplianceImport(ctx, t, svc, client, cfg,
		complianceListDiscovery("chat_desktop", "Desktop chat", "list_cur_2"),
	)
	require.Equal(t, 1, progress.ChatsImported)
	require.Equal(t, 1, progress.MessagePagesFetched)
	require.Equal(t, "chats:list_cur_2", progress.CursorPersisted)

	messages, err = chatrepo.New(svc.db).ListChatMessages(ctx, chatrepo.ListChatMessagesParams{ChatID: desktopChatID, ProjectID: projectID})
	require.NoError(t, err)
	require.Len(t, messages, 3)
	require.Equal(t, "msg_3", messages[2].ExternalMessageID.String)
	require.Equal(t, "claude", messages[2].Source.String)
	require.Equal(t, desktopUA, messages[2].UserAgent.String)
	require.Equal(t, "203.0.113.9", messages[2].IpAddress.String)

	messagesCursor, err := chatrepo.New(svc.db).LinkAIIntegrationConfigChat(ctx, chatrepo.LinkAIIntegrationConfigChatParams{
		AiIntegrationConfigID: cfg.ID,
		ChatID:                desktopChatID,
		ProjectID:             projectID,
	})
	require.NoError(t, err)
	require.Equal(t, "msg_cur_3", messagesCursor.String)
}

func TestImportDiscoveredChatsKeepsFeedClientForSameRunListRevisit(t *testing.T) {
	t.Parallel()

	ctx, svc, _, cfg, _, _ := complianceImportFixture(t)

	const desktopUA = "Mozilla/5.0 Claude/1.2.3 Electron/39.0.0"
	server := complianceMessagesServer(t, map[string]map[string]anthropicapi.ChatMessagesPage{
		"chat_desktop": {
			"": complianceMessagesPage("chat_desktop", "Desktop chat", "msg_cur_1",
				complianceMessage("msg_1", "user", "hello", "2026-07-14T09:01:00Z"),
			),
		},
	})
	client := anthropicapi.New(svc.guardianPolicy, anthropicapi.WithBaseURL(server.URL), anthropicapi.WithAPIKey("anthropic-key"))

	// The list revisit arrives while the feed's rows are still queued for
	// the writer: nothing is stored yet, so only the run's own memory of
	// the feed discovery can keep the desktop identity.
	in := make(chan discoveredChat, 2)
	in <- complianceFeedDiscovery("chat_desktop", desktopUA)
	in <- complianceListDiscovery("chat_desktop", "Desktop chat", "list_cur_1")
	close(in)

	out := make(chan messagePageBatch, 4)
	var batches []messagePageBatch
	drained := make(chan struct{})
	go func() {
		defer close(drained)
		for batch := range out {
			batches = append(batches, batch)
		}
	}()
	err := svc.importDiscoveredChats(ctx, client, cfg, in, out, complianceDiscoveryProgress(false))
	close(out)
	<-drained
	require.NoError(t, err)

	require.Len(t, batches, 2)
	for _, batch := range batches {
		require.Len(t, batch.rows, 1)
		require.Equal(t, "claude", batch.rows[0].Params.Source.String)
		require.Equal(t, desktopUA, batch.rows[0].Params.UserAgent.String)
		require.Equal(t, "203.0.113.9", batch.rows[0].Params.IpAddress.String)
	}
}

func TestConnectedUserResolverMatchesMixedCaseStoredEmail(t *testing.T) {
	t.Parallel()

	ctx, conn, _, orgID := newStoreTestDB(t)

	// WorkOS-synced users can be stored with their original casing; the
	// compliance feed reports lowercase emails.
	userRow, err := usersrepo.New(conn).UpsertUser(ctx, usersrepo.UpsertUserParams{
		ID:          "user_" + uuid.NewString(),
		Email:       "Ada.Lovelace@Example.com",
		DisplayName: "Ada",
		PhotoUrl:    conv.ToPGTextEmpty(""),
		Admin:       false,
	})
	require.NoError(t, err)
	require.NoError(t, testrepo.New(conn).CreateOrganizationUserRelationshipFixture(ctx, testrepo.CreateOrganizationUserRelationshipFixtureParams{
		OrganizationID: orgID,
		UserID:         conv.ToPGText(userRow.ID),
	}))

	resolver := newConnectedUserResolver(conn, orgID)
	resolvedID, err := resolver.resolve(ctx, "ada.lovelace@example.com")
	require.NoError(t, err)
	require.Equal(t, userRow.ID, resolvedID)
}
