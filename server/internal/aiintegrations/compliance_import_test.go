package aiintegrations

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/chat"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	anthropicapi "github.com/speakeasy-api/gram/server/internal/thirdparty/anthropic"
)

func TestComplianceSourceFromUserAgentDesktop(t *testing.T) {
	t.Parallel()

	require.Equal(t, "claude", complianceSourceFromUserAgent("Claude/1.2.3"))
	require.Equal(t, "claude", complianceSourceFromUserAgent("Electron/39.0.0"))
}

func TestComplianceSourceFromUserAgentWeb(t *testing.T) {
	t.Parallel()

	require.Equal(t, "claude-chat-web", complianceSourceFromUserAgent("Mozilla/5.0"))
	require.Equal(t, "claude-chat-web", complianceSourceFromUserAgent(""))
}

func TestChatsCursorStoredFormIgnoresActivityFeedToken(t *testing.T) {
	t.Parallel()

	// The schedule stored an activity feed token before it walked the chat
	// list; that token must never reach the chat list as a cursor.
	require.Empty(t, chatsCursorFromStored(""))
	require.Empty(t, chatsCursorFromStored("activity_01XyDMpzjS89pFZXqSFUBDr6"))
	require.Equal(t, "eyJrIjogInVwZGF0ZWRfYXQifQ", chatsCursorFromStored("chats:eyJrIjogInVwZGF0ZWRfYXQifQ"))

	require.Equal(t, "chats:eyJrIjogInVwZGF0ZWRfYXQifQ", storedChatsCursor("eyJrIjogInVwZGF0ZWRfYXQifQ"))
	require.Empty(t, storedChatsCursor(""))
}

func complianceUserActivity(id, chatID, userAgent string) anthropicapi.Activity {
	return anthropicapi.Activity{
		ID:               id,
		Type:             anthropicComplianceActivityCreated,
		CreatedAt:        "2026-07-14T09:01:00Z",
		OrganizationID:   "ext-org",
		OrganizationUUID: "",
		Actor:            anthropicapi.Actor{Type: "user_actor", EmailAddress: "dev@example.com", UserID: "user_1", IPAddress: "203.0.113.9", UserAgent: userAgent},
		ClaudeChatID:     chatID,
		ClaudeProjectID:  "",
	}
}

func complianceAPIActivity(id string) anthropicapi.Activity {
	return anthropicapi.Activity{
		ID:               id,
		Type:             anthropicComplianceActivityCreated,
		CreatedAt:        "2026-07-14T09:01:01Z",
		OrganizationID:   "ext-org",
		OrganizationUUID: "",
		Actor:            anthropicapi.Actor{Type: "api_actor", EmailAddress: "", UserID: "", IPAddress: "", UserAgent: ""},
		ClaudeChatID:     "chat_ignored",
		ClaudeProjectID:  "",
	}
}

func complianceListedChat(id, name string) anthropicapi.ChatSummary {
	return anthropicapi.ChatSummary{
		ID:               id,
		Name:             name,
		CreatedAt:        "2026-07-14T08:00:00Z",
		UpdatedAt:        "2026-07-14T09:02:00Z",
		DeletedAt:        nil,
		Href:             "https://claude.ai/chat/" + id,
		Model:            nil,
		OrganizationUUID: "",
		ProjectID:        "",
		User:             anthropicapi.ChatUser{ID: "user_1", EmailAddress: "dev@example.com"},
	}
}

func complianceDeletedChat(id string) anthropicapi.ChatSummary {
	deleted := complianceListedChat(id, "")
	deleted.DeletedAt = new("2026-07-14T09:03:00Z")
	return deleted
}

func complianceDiscoveryService(t *testing.T, serverURL string) (*ComplianceImportService, *anthropicapi.Client) {
	t.Helper()

	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), []string{})
	require.NoError(t, err)
	client := anthropicapi.New(policy, anthropicapi.WithBaseURL(serverURL), anthropicapi.WithAPIKey("anthropic-key"))
	svc := NewComplianceImportService(testenv.NewLogger(t), nil, policy, nil, func(context.Context, string, int) {})
	return svc, client
}

// complianceWatermark is the schedule watermark the discovery tests start
// from: the previous run's end time.
var complianceWatermark = time.Date(2026, 7, 14, 9, 0, 0, 0, time.UTC)

func complianceDiscoveryConfig(lastCursor string) Config {
	extOrgID := "ext-org"
	return Config{
		ID:                     uuid.New(),
		SyncID:                 uuid.Nil,
		OrganizationID:         "org_test",
		Provider:               ProviderAnthropicCompliance,
		ProjectID:              uuid.New(),
		ExternalOrganizationID: &extOrgID,
		BillingMode:            "",
		APIKey:                 "anthropic-key",
		Enabled:                true,
		PollWatermarkAt:        complianceWatermark,
		NextPollAfter:          time.Time{},
		LastPollError:          "",
		LastPollFailedAt:       time.Time{},
		LastPollSuccessAt:      time.Time{},
		ConsecutiveFailures:    0,
		LastCursor:             lastCursor,
		CreatedAt:              time.Time{},
		UpdatedAt:              time.Time{},
	}
}

func complianceDiscoveryProgress(firstSync bool) *ComplianceSyncProgress {
	return &ComplianceSyncProgress{
		FirstSync:           firstSync,
		ActivityPages:       0,
		ChatActivities:      0,
		ChatListPages:       0,
		ChatsListed:         0,
		ChatsImported:       0,
		MessagePagesFetched: 0,
		MessagePagesWritten: 0,
		CursorReached:       "",
		CursorPersisted:     "",
	}
}

func collectDiscovered(out chan discoveredChat) []discoveredChat {
	close(out)
	var discovered []discoveredChat
	for d := range out {
		discovered = append(discovered, d)
	}
	return discovered
}

func TestStreamCreatedChatsWindowPollsSinceWatermark(t *testing.T) {
	t.Parallel()

	// The window reaches back past the watermark by the overlap and stops
	// one indexing lag before the run's end time. Activities are newest
	// first, so paging within the window uses after_id.
	endTime := complianceWatermark.Add(5 * time.Minute)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query()["activity_types[]"]; len(got) != 1 || got[0] != "claude_chat_created" {
			t.Errorf("expected activity_types[] [claude_chat_created], got %v", got)
		}
		if got := r.URL.Query()["organization_ids[]"]; len(got) != 1 || got[0] != "ext-org" {
			t.Errorf("expected organization_ids[] [ext-org], got %v", got)
		}
		if got := r.URL.Query().Get("created_at.gte"); got != "2026-07-14T08:55:00Z" {
			t.Errorf("expected created_at.gte 2026-07-14T08:55:00Z, got %q", got)
		}
		if got := r.URL.Query().Get("created_at.lt"); got != "2026-07-14T09:04:00Z" {
			t.Errorf("expected created_at.lt 2026-07-14T09:04:00Z, got %q", got)
		}
		if got := r.URL.Query().Get("before_id"); got != "" {
			t.Errorf("window poll must not send before_id, got %q", got)
		}
		switch r.URL.Query().Get("after_id") {
		case "":
			_ = json.NewEncoder(w).Encode(anthropicapi.ActivitiesPage{
				Data: []anthropicapi.Activity{
					complianceUserActivity("act_3", "chat_2", "Claude/1.2.3 Electron/39.0.0"),
					complianceAPIActivity("act_2"),
					complianceUserActivity("act_1", "chat_1", "Mozilla/5.0"),
				},
				HasMore: true,
				FirstID: "act_3",
				LastID:  "act_1",
			})
		case "act_1":
			_ = json.NewEncoder(w).Encode(anthropicapi.ActivitiesPage{
				Data:    []anthropicapi.Activity{complianceUserActivity("act_0", "chat_0", "")},
				HasMore: false,
				FirstID: "act_0",
				LastID:  "act_0",
			})
		default:
			t.Errorf("unexpected after_id %q", r.URL.Query().Get("after_id"))
		}
	}))
	t.Cleanup(server.Close)

	svc, client := complianceDiscoveryService(t, server.URL)
	cfg := complianceDiscoveryConfig("chats:cur_start")
	progress := complianceDiscoveryProgress(false)

	out := make(chan discoveredChat, 16)
	require.NoError(t, svc.streamCreatedChats(t.Context(), client, cfg, false, endTime, out, progress))

	require.Equal(t, 2, progress.ActivityPages)
	require.Equal(t, 3, progress.ChatActivities)

	discovered := collectDiscovered(out)
	require.Len(t, discovered, 3)
	require.Equal(t, "chat_2", discovered[0].externalChatID)
	require.True(t, discovered[0].hasClient)
	require.Equal(t, chatClient{source: "claude", userAgent: "Claude/1.2.3 Electron/39.0.0", ipAddress: "203.0.113.9"}, discovered[0].client)
	require.Equal(t, "dev@example.com", discovered[0].userEmail)
	require.Equal(t, "user_1", discovered[0].externalUserID)
	require.Equal(t, time.Date(2026, 7, 14, 9, 1, 0, 0, time.UTC), discovered[0].createdAt)
	require.Equal(t, "chat_1", discovered[1].externalChatID)
	require.Equal(t, "claude-chat-web", discovered[1].client.source)
	require.Equal(t, "chat_0", discovered[2].externalChatID)
	for _, d := range discovered {
		require.Empty(t, d.chatsCursor, "the activity window carries no checkpoint markers")
		require.False(t, d.cursorOnly)
	}
}

func TestStreamCreatedChatsFirstSyncReachesBackInitialLookback(t *testing.T) {
	t.Parallel()

	endTime := complianceWatermark.Add(5 * time.Minute)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("created_at.gte"); got != "2026-07-13T09:00:00Z" {
			t.Errorf("expected created_at.gte 2026-07-13T09:00:00Z, got %q", got)
		}
		_ = json.NewEncoder(w).Encode(anthropicapi.ActivitiesPage{Data: nil, HasMore: false, FirstID: "", LastID: ""})
	}))
	t.Cleanup(server.Close)

	svc, client := complianceDiscoveryService(t, server.URL)
	cfg := complianceDiscoveryConfig("")
	progress := complianceDiscoveryProgress(true)

	out := make(chan discoveredChat, 16)
	require.NoError(t, svc.streamCreatedChats(t.Context(), client, cfg, true, endTime, out, progress))
	require.Equal(t, 1, progress.ActivityPages)
	require.Empty(t, collectDiscovered(out))
}

func TestStreamCreatedChatsSkipsEmptyWindow(t *testing.T) {
	t.Parallel()

	// A run whose end time does not clear the overlap plus indexing lag has
	// no window to poll and must not call the feed.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected request %s", r.URL.String())
	}))
	t.Cleanup(server.Close)

	svc, client := complianceDiscoveryService(t, server.URL)
	cfg := complianceDiscoveryConfig("chats:cur_start")
	progress := complianceDiscoveryProgress(false)

	out := make(chan discoveredChat, 16)
	require.NoError(t, svc.streamCreatedChats(t.Context(), client, cfg, false, complianceWatermark.Add(-10*time.Minute), out, progress))
	require.Equal(t, 0, progress.ActivityPages)
	require.Empty(t, collectDiscovered(out))
}

func TestStreamUpdatedChatsWalksForwardFromCursor(t *testing.T) {
	t.Parallel()

	// The chat list is ascending by updated_at; a forward walk passes each
	// page's last_id as after_id and sends no time bound once positioned.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/compliance/apps/chats" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		if got := r.URL.Query().Get("order_by"); got != "updated_at" {
			t.Errorf("expected order_by updated_at, got %q", got)
		}
		if got := r.URL.Query().Get("updated_at.gte"); got != "" {
			t.Errorf("a cursor-positioned walk must not send updated_at.gte, got %q", got)
		}
		if got := r.URL.Query()["organization_ids[]"]; len(got) != 1 || got[0] != "ext-org" {
			t.Errorf("expected organization_ids[] [ext-org], got %v", got)
		}
		if got := r.URL.Query().Get("limit"); got != "100" {
			t.Errorf("expected limit 100, got %q", got)
		}
		switch r.URL.Query().Get("after_id") {
		case "cur_start":
			_ = json.NewEncoder(w).Encode(anthropicapi.ChatsPage{
				Data:    []anthropicapi.ChatSummary{complianceListedChat("chat_1", "Chat one"), complianceDeletedChat("chat_gone")},
				HasMore: true,
				FirstID: "p0",
				LastID:  "p1",
			})
		case "p1":
			// A page holding only a deleted chat: nothing to import, but the
			// walk still advances past it.
			_ = json.NewEncoder(w).Encode(anthropicapi.ChatsPage{
				Data:    []anthropicapi.ChatSummary{complianceDeletedChat("chat_gone_2")},
				HasMore: true,
				FirstID: "p1",
				LastID:  "p2",
			})
		case "p2":
			_ = json.NewEncoder(w).Encode(anthropicapi.ChatsPage{
				Data:    []anthropicapi.ChatSummary{complianceListedChat("chat_3", "Chat three")},
				HasMore: false,
				FirstID: "p2",
				LastID:  "p3",
			})
		default:
			t.Errorf("unexpected after_id %q", r.URL.Query().Get("after_id"))
		}
	}))
	t.Cleanup(server.Close)

	svc, client := complianceDiscoveryService(t, server.URL)
	cfg := complianceDiscoveryConfig("chats:cur_start")
	progress := complianceDiscoveryProgress(false)

	out := make(chan discoveredChat, 16)
	cursor, err := svc.streamUpdatedChats(t.Context(), client, cfg, "cur_start", out, progress)
	require.NoError(t, err)

	require.Equal(t, "p3", cursor)
	require.Equal(t, 3, progress.ChatListPages)
	require.Equal(t, 2, progress.ChatsListed)

	discovered := collectDiscovered(out)
	require.Len(t, discovered, 3)

	// chat_1 closes out page one and carries its last_id; the page holding
	// only a deleted chat yields a cursor-only sentinel; chat_3 carries the
	// final page's last_id.
	require.Equal(t, "chat_1", discovered[0].externalChatID)
	require.Equal(t, "Chat one", discovered[0].title)
	require.Equal(t, "dev@example.com", discovered[0].userEmail)
	require.Equal(t, "user_1", discovered[0].externalUserID)
	require.Equal(t, time.Date(2026, 7, 14, 8, 0, 0, 0, time.UTC), discovered[0].createdAt)
	require.Equal(t, time.Date(2026, 7, 14, 9, 2, 0, 0, time.UTC), discovered[0].updatedAt)
	require.False(t, discovered[0].hasClient)
	require.Equal(t, "p1", discovered[0].chatsCursor)
	require.False(t, discovered[0].cursorOnly)
	require.True(t, discovered[1].cursorOnly)
	require.Equal(t, "p2", discovered[1].chatsCursor)
	require.Empty(t, discovered[1].externalChatID)
	require.Equal(t, "chat_3", discovered[2].externalChatID)
	require.Equal(t, "p3", discovered[2].chatsCursor)
}

func TestStreamUpdatedChatsFirstSyncBoundsFirstPageOnly(t *testing.T) {
	t.Parallel()

	// With no cursor the first page is bounded by updated_at.gte at the
	// initial lookback; later pages are positioned by after_id alone.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("after_id") {
		case "":
			if got := r.URL.Query().Get("updated_at.gte"); got != "2026-07-13T09:00:00Z" {
				t.Errorf("expected updated_at.gte 2026-07-13T09:00:00Z, got %q", got)
			}
			_ = json.NewEncoder(w).Encode(anthropicapi.ChatsPage{
				Data:    []anthropicapi.ChatSummary{complianceListedChat("chat_1", "Chat one")},
				HasMore: true,
				FirstID: "p0",
				LastID:  "p1",
			})
		case "p1":
			if got := r.URL.Query().Get("updated_at.gte"); got != "" {
				t.Errorf("a cursor-positioned page must not send updated_at.gte, got %q", got)
			}
			_ = json.NewEncoder(w).Encode(anthropicapi.ChatsPage{
				Data:    []anthropicapi.ChatSummary{complianceListedChat("chat_2", "Chat two")},
				HasMore: false,
				FirstID: "p1",
				LastID:  "p2",
			})
		default:
			t.Errorf("unexpected after_id %q", r.URL.Query().Get("after_id"))
		}
	}))
	t.Cleanup(server.Close)

	svc, client := complianceDiscoveryService(t, server.URL)
	cfg := complianceDiscoveryConfig("")
	progress := complianceDiscoveryProgress(true)

	out := make(chan discoveredChat, 16)
	cursor, err := svc.streamUpdatedChats(t.Context(), client, cfg, "", out, progress)
	require.NoError(t, err)
	require.Equal(t, "p2", cursor)
	require.Equal(t, 2, progress.ChatListPages)
	require.Len(t, collectDiscovered(out), 2)
}

func TestStreamUpdatedChatsKeepsCursorWhenListIsEmpty(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(anthropicapi.ChatsPage{Data: nil, HasMore: false, FirstID: "", LastID: ""})
	}))
	t.Cleanup(server.Close)

	svc, client := complianceDiscoveryService(t, server.URL)
	cfg := complianceDiscoveryConfig("chats:cur_start")
	progress := complianceDiscoveryProgress(false)

	out := make(chan discoveredChat, 16)
	cursor, err := svc.streamUpdatedChats(t.Context(), client, cfg, "cur_start", out, progress)
	require.NoError(t, err)
	require.Equal(t, "cur_start", cursor)
	require.Empty(t, collectDiscovered(out))
}

func TestWriteMessagePagesAdvancesChatsCursor(t *testing.T) {
	t.Parallel()

	ctx, conn, store, orgID := newStoreTestDB(t)

	extOrgID := "ext-org"
	watermark := time.Now().UTC().Add(-initialUsagePollLookback)
	created := upsertConfigWithTx(t, ctx, conn, store, orgID, ProviderAnthropicCompliance, "anthropic-key", true, true, &extOrgID, &watermark)
	cfg := created.Config

	writer, shutdown := chat.NewChatMessageWriter(testenv.NewLogger(t), conn, nil)
	t.Cleanup(func() { _ = shutdown(context.Background()) })

	svc := NewComplianceImportService(testenv.NewLogger(t), conn, nil, writer, func(context.Context, string, int) {})

	in := make(chan messagePageBatch, 4)
	in <- messagePageBatch{chatID: uuid.Nil, rows: nil, lastID: "", chatsCursor: "", cursorOnly: false}
	in <- messagePageBatch{chatID: uuid.Nil, rows: nil, lastID: "", chatsCursor: "cur_100", cursorOnly: false}
	// A cursor-only sentinel from a list page with nothing importable:
	// advances the cursor without counting as a written message page.
	in <- messagePageBatch{chatID: uuid.Nil, rows: nil, lastID: "", chatsCursor: "cur_150", cursorOnly: true}
	in <- messagePageBatch{chatID: uuid.Nil, rows: nil, lastID: "", chatsCursor: "cur_200", cursorOnly: false}
	close(in)

	progress := complianceDiscoveryProgress(true)
	require.NoError(t, svc.writeMessagePages(ctx, cfg, in, progress))

	require.Equal(t, 3, progress.MessagePagesWritten)
	require.Equal(t, "chats:cur_200", progress.CursorPersisted)

	// The persisted cursor must be visible through the same read PollAIData
	// performs at the start of each retry attempt, in the stored form the
	// next run decodes, so a failed run resumes from the last completed
	// list page.
	reloaded, err := store.GetUsagePollConfig(ctx, cfg.ID, ScheduleAnthropicCompliance)
	require.NoError(t, err)
	require.Equal(t, "chats:cur_200", reloaded.LastCursor)
	require.Equal(t, "cur_200", chatsCursorFromStored(reloaded.LastCursor))
}
