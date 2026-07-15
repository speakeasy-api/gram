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

func TestStreamChatActivitiesMarksLastImportableActivityPerPage(t *testing.T) {
	t.Parallel()

	userActivity := func(id, chatID string) anthropicapi.Activity {
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
	systemActivity := func(id string) anthropicapi.Activity {
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

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("after_id") {
		case "":
			_ = json.NewEncoder(w).Encode(anthropicapi.ActivitiesPage{
				Data:    []anthropicapi.Activity{userActivity("act_1", "chat_1"), systemActivity("act_2"), userActivity("act_3", "chat_2")},
				HasMore: true,
				FirstID: "act_1",
				LastID:  "act_3",
			})
		case "act_3":
			// A fully-filtered page: nothing importable, but the feed still
			// advances past it.
			_ = json.NewEncoder(w).Encode(anthropicapi.ActivitiesPage{
				Data:    []anthropicapi.Activity{systemActivity("act_4"), systemActivity("act_5")},
				HasMore: true,
				FirstID: "act_4",
				LastID:  "act_5",
			})
		case "act_5":
			_ = json.NewEncoder(w).Encode(anthropicapi.ActivitiesPage{
				Data:    []anthropicapi.Activity{userActivity("act_6", "chat_3")},
				HasMore: false,
				FirstID: "act_6",
				LastID:  "act_6",
			})
		default:
			t.Errorf("unexpected after_id %q", r.URL.Query().Get("after_id"))
		}
	}))
	t.Cleanup(server.Close)

	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), []string{})
	require.NoError(t, err)
	client := anthropicapi.New(policy, anthropicapi.WithBaseURL(server.URL), anthropicapi.WithAPIKey("anthropic-key"))

	svc := NewComplianceImportService(testenv.NewLogger(t), nil, policy, nil, func(context.Context, string, int) {})
	extOrgID := "ext-org"
	cfg := Config{
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
		LastCursor:             "",
		CreatedAt:              time.Time{},
		UpdatedAt:              time.Time{},
	}

	out := make(chan discoveredActivity, 16)
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

	// The api_actor activity is filtered, so the last importable activity of
	// page one (act_3) carries the page's cursor; mid-page activities carry
	// none. The fully-filtered second page yields a cursor-only sentinel so
	// its token still becomes durable. The final page's last activity
	// carries its cursor too.
	require.Equal(t, "act_1", discovered[0].activity.ID)
	require.Empty(t, discovered[0].activitiesCursor)
	require.False(t, discovered[0].cursorOnly)
	require.Equal(t, "act_3", discovered[1].activity.ID)
	require.Equal(t, "act_3", discovered[1].activitiesCursor)
	require.True(t, discovered[2].cursorOnly)
	require.Equal(t, "act_5", discovered[2].activitiesCursor)
	require.Empty(t, discovered[2].activity.ID)
	require.Equal(t, "act_6", discovered[3].activity.ID)
	require.Equal(t, "act_6", discovered[3].activitiesCursor)
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
	reloaded, err := store.GetUsagePollConfig(ctx, cfg.ID)
	require.NoError(t, err)
	require.Equal(t, "act_200", reloaded.LastCursor)
}
