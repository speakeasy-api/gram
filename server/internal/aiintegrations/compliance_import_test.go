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

func complianceUserActivity(id, chatID string) anthropicapi.Activity {
	return anthropicapi.Activity{
		ID:               id,
		Type:             anthropicComplianceActivityCreated,
		CreatedAt:        "2026-07-14T10:00:00Z",
		OrganizationID:   "ext-org",
		OrganizationUUID: "",
		Actor:            anthropicapi.Actor{Type: "user_actor", EmailAddress: "dev@example.com", UserID: "user_1", IPAddress: "", UserAgent: ""},
		ClaudeChatID:     chatID,
		ClaudeProjectID:  "",
	}
}

func complianceSystemActivity(id string) anthropicapi.Activity {
	return anthropicapi.Activity{
		ID:               id,
		Type:             anthropicComplianceActivityUpdated,
		CreatedAt:        "2026-07-14T10:00:01Z",
		OrganizationID:   "ext-org",
		OrganizationUUID: "",
		Actor:            anthropicapi.Actor{Type: "api_actor", EmailAddress: "", UserID: "", IPAddress: "", UserAgent: ""},
		ClaudeChatID:     "chat_ignored",
		ClaudeProjectID:  "",
	}
}

func complianceDiscoveryService(t *testing.T, serverURL string) (*ComplianceImportService, *anthropicapi.Client) {
	t.Helper()

	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), []string{})
	require.NoError(t, err)
	client := anthropicapi.New(policy, anthropicapi.WithBaseURL(serverURL), anthropicapi.WithAPIKey("anthropic-key"))
	svc := NewComplianceImportService(testenv.NewLogger(t), nil, policy, nil, func(context.Context, string, int) {})
	return svc, client
}

func complianceDiscoveryConfig(lastCursor string) Config {
	extOrgID := "ext-org"
	return Config{
		ID:                     uuid.New(),
		OrganizationID:         "org_test",
		Provider:               ProviderAnthropicCompliance,
		ProjectID:              uuid.New(),
		ExternalOrganizationID: &extOrgID,
		BillingMode:            "",
		APIKey:                 "anthropic-key",
		Enabled:                true,
		PollWatermarkAt:        time.Date(2026, 7, 14, 9, 0, 0, 0, time.UTC),
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
		ChatsImported:       0,
		MessagePagesFetched: 0,
		MessagePagesWritten: 0,
		CursorReached:       "",
		CursorPersisted:     "",
	}
}

func TestStreamChatActivitiesWalksForwardFromCursor(t *testing.T) {
	t.Parallel()

	// The feed is newest first; a forward walk pages with before_id and each
	// page's newest edge is its first_id. Pages arrive oldest window first.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("after_id"); got != "" {
			t.Errorf("forward walk must not send after_id, got %q", got)
		}
		if got := r.URL.Query().Get("created_at.gte"); got != "" {
			t.Errorf("forward walk must not send created_at.gte, got %q", got)
		}
		switch r.URL.Query().Get("before_id") {
		case "cur_start":
			_ = json.NewEncoder(w).Encode(anthropicapi.ActivitiesPage{
				Data:    []anthropicapi.Activity{complianceUserActivity("act_3", "chat_2"), complianceSystemActivity("act_2"), complianceUserActivity("act_1", "chat_1")},
				HasMore: true,
				FirstID: "act_3",
				LastID:  "act_1",
			})
		case "act_3":
			// A fully-filtered page: nothing importable, but the feed still
			// advances past it.
			_ = json.NewEncoder(w).Encode(anthropicapi.ActivitiesPage{
				Data:    []anthropicapi.Activity{complianceSystemActivity("act_5"), complianceSystemActivity("act_4")},
				HasMore: true,
				FirstID: "act_5",
				LastID:  "act_4",
			})
		case "act_5":
			_ = json.NewEncoder(w).Encode(anthropicapi.ActivitiesPage{
				Data:    []anthropicapi.Activity{complianceUserActivity("act_6", "chat_3")},
				HasMore: false,
				FirstID: "act_6",
				LastID:  "act_6",
			})
		default:
			t.Errorf("unexpected before_id %q", r.URL.Query().Get("before_id"))
		}
	}))
	t.Cleanup(server.Close)

	svc, client := complianceDiscoveryService(t, server.URL)
	cfg := complianceDiscoveryConfig("cur_start")
	progress := complianceDiscoveryProgress(false)

	out := make(chan discoveredActivity, 16)
	cursor, err := svc.streamChatActivities(t.Context(), client, cfg, out, progress)
	require.NoError(t, err)
	close(out)

	require.Equal(t, "act_6", cursor)
	require.Equal(t, 3, progress.ActivityPages)
	require.Equal(t, 3, progress.ChatActivities)

	var discovered []discoveredActivity
	for d := range out {
		discovered = append(discovered, d)
	}
	require.Len(t, discovered, 4)

	// Page one sends newest first (act_3, act_1); the last importable
	// activity carries the page's first_id checkpoint. The fully-filtered
	// second page yields a cursor-only sentinel carrying its first_id. The
	// final page's activity carries its own first_id.
	require.Equal(t, "act_3", discovered[0].activity.ID)
	require.Empty(t, discovered[0].activitiesCursor)
	require.False(t, discovered[0].cursorOnly)
	require.Equal(t, "act_1", discovered[1].activity.ID)
	require.Equal(t, "act_3", discovered[1].activitiesCursor)
	require.True(t, discovered[2].cursorOnly)
	require.Equal(t, "act_5", discovered[2].activitiesCursor)
	require.Empty(t, discovered[2].activity.ID)
	require.Equal(t, "act_6", discovered[3].activity.ID)
	require.Equal(t, "act_6", discovered[3].activitiesCursor)
}

func TestBackfillChatActivitiesReturnsNewestEdgeWithoutMarkers(t *testing.T) {
	t.Parallel()

	// First sync: no cursor stored. The backfill pages older with after_id
	// inside the watermark window and hands off the newest edge (the first
	// page's first_id) as the forward-walk cursor for every later sync.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("before_id"); got != "" {
			t.Errorf("backfill must not send before_id, got %q", got)
		}
		if got := r.URL.Query().Get("created_at.gte"); got == "" {
			t.Error("backfill must bound the window with created_at.gte")
		}
		switch r.URL.Query().Get("after_id") {
		case "":
			_ = json.NewEncoder(w).Encode(anthropicapi.ActivitiesPage{
				Data:    []anthropicapi.Activity{complianceUserActivity("act_9", "chat_9"), complianceUserActivity("act_8", "chat_8")},
				HasMore: true,
				FirstID: "act_9",
				LastID:  "act_8",
			})
		case "act_8":
			_ = json.NewEncoder(w).Encode(anthropicapi.ActivitiesPage{
				Data:    []anthropicapi.Activity{complianceUserActivity("act_7", "chat_7")},
				HasMore: false,
				FirstID: "act_7",
				LastID:  "act_7",
			})
		default:
			t.Errorf("unexpected after_id %q", r.URL.Query().Get("after_id"))
		}
	}))
	t.Cleanup(server.Close)

	svc, client := complianceDiscoveryService(t, server.URL)
	cfg := complianceDiscoveryConfig("")
	progress := complianceDiscoveryProgress(true)

	out := make(chan discoveredActivity, 16)
	cursor, err := svc.streamChatActivities(t.Context(), client, cfg, out, progress)
	require.NoError(t, err)
	close(out)

	// The handed-off cursor is the newest edge, not the backfill's deepest
	// point — resuming forward from the oldest edge would walk backwards
	// through history forever.
	require.Equal(t, "act_9", cursor)
	require.Equal(t, 2, progress.ActivityPages)
	require.Equal(t, 3, progress.ChatActivities)

	var discovered []discoveredActivity
	for d := range out {
		discovered = append(discovered, d)
	}
	require.Len(t, discovered, 3)
	for _, d := range discovered {
		require.False(t, d.cursorOnly)
		require.Empty(t, d.activitiesCursor)
	}
}

func TestWriteMessagePagesAdvancesActivitiesCursor(t *testing.T) {
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
	in <- messagePageBatch{chatID: uuid.Nil, rows: nil, lastID: "", activitiesCursor: "", cursorOnly: false}
	in <- messagePageBatch{chatID: uuid.Nil, rows: nil, lastID: "", activitiesCursor: "act_100", cursorOnly: false}
	// A cursor-only sentinel from a fully-filtered activities page: advances
	// the cursor without counting as a written message page.
	in <- messagePageBatch{chatID: uuid.Nil, rows: nil, lastID: "", activitiesCursor: "act_150", cursorOnly: true}
	in <- messagePageBatch{chatID: uuid.Nil, rows: nil, lastID: "", activitiesCursor: "act_200", cursorOnly: false}
	close(in)

	progress := &ComplianceSyncProgress{
		FirstSync:           true,
		ActivityPages:       0,
		ChatActivities:      0,
		ChatsImported:       0,
		MessagePagesFetched: 0,
		MessagePagesWritten: 0,
		CursorReached:       "",
		CursorPersisted:     "",
	}
	require.NoError(t, svc.writeMessagePages(ctx, cfg, in, progress))

	require.Equal(t, 3, progress.MessagePagesWritten)
	require.Equal(t, "act_200", progress.CursorPersisted)

	// The persisted cursor must be visible through the same read PollAIData
	// performs at the start of each retry attempt, so a failed run resumes
	// from the last completed activities page.
	reloaded, err := store.GetUsagePollConfig(ctx, cfg.ID, ScheduleAnthropicCompliance)
	require.NoError(t, err)
	require.Equal(t, "act_200", reloaded.LastCursor)
}
