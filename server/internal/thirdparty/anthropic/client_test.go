package anthropic

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestListActivitiesSendsAuthAndFilters(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/compliance/activities" {
			t.Errorf("expected path /v1/compliance/activities, got %s", r.URL.Path)
		}
		if got := r.Header.Get("x-api-key"); got != "anthropic-key" {
			t.Errorf("expected x-api-key anthropic-key, got %s", got)
		}
		if got := r.URL.Query()["activity_types[]"]; !slices.Equal(got, []string{"claude_chat_created", "claude_chat_updated"}) {
			t.Errorf("expected activity_types[] [claude_chat_created claude_chat_updated], got %v", got)
		}
		if got := r.URL.Query()["organization_ids[]"]; !slices.Equal(got, []string{"91012d09-e48b-438e-a489-1bebfd8fa6f9"}) {
			t.Errorf("expected organization_ids[] [91012d09-e48b-438e-a489-1bebfd8fa6f9], got %v", got)
		}
		if got := r.URL.Query().Get("created_at.gte"); got != "2026-04-10T08:09:10Z" {
			t.Errorf("expected created_at.gte 2026-04-10T08:09:10Z, got %s", got)
		}
		if got := r.URL.Query().Get("after_id"); got != "activity_last" {
			t.Errorf("expected after_id activity_last, got %s", got)
		}
		if got := r.URL.Query().Get("limit"); got != "5000" {
			t.Errorf("expected limit 5000, got %s", got)
		}

		_ = json.NewEncoder(w).Encode(ActivitiesPage{
			Data: []Activity{{
				ID:           "activity_1",
				Type:         "claude_chat_created",
				CreatedAt:    "2026-04-10T08:09:11Z",
				ClaudeChatID: "claude_chat_1",
				Actor:        Actor{Type: "user_actor", UserID: "user_1", EmailAddress: "dev@example.com"},
			}},
			HasMore: true,
			LastID:  "activity_1",
		})
	}))
	t.Cleanup(server.Close)

	client := New(testGuardianPolicy(t), WithBaseURL(server.URL), WithAPIKey("anthropic-key"))
	page, err := client.ListActivities(t.Context(), ListActivitiesParams{
		ActivityTypes:   []string{"claude_chat_created", "claude_chat_updated"},
		OrganizationIDs: []string{"91012d09-e48b-438e-a489-1bebfd8fa6f9"},
		CreatedAtGTE:    time.Date(2026, 4, 10, 8, 9, 10, 0, time.UTC),
		AfterID:         "activity_last",
		BeforeID:        "",
		Limit:           5000,
	})
	require.NoError(t, err)
	require.True(t, page.HasMore)
	require.Equal(t, "claude_chat_1", page.Data[0].ClaudeChatID)
}

func TestListActivitiesSendsBeforeID(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("before_id"); got != "activity_first" {
			t.Errorf("expected before_id activity_first, got %q", got)
		}
		if got := r.URL.Query().Get("after_id"); got != "" {
			t.Errorf("expected no after_id, got %q", got)
		}
		_ = json.NewEncoder(w).Encode(ActivitiesPage{
			Data:    nil,
			HasMore: false,
			FirstID: "",
			LastID:  "",
		})
	}))
	t.Cleanup(server.Close)

	client := New(testGuardianPolicy(t), WithBaseURL(server.URL), WithAPIKey("anthropic-key"))
	page, err := client.ListActivities(t.Context(), ListActivitiesParams{
		ActivityTypes:   nil,
		OrganizationIDs: nil,
		CreatedAtGTE:    time.Time{},
		AfterID:         "",
		BeforeID:        "activity_first",
		Limit:           100,
	})
	require.NoError(t, err)
	require.False(t, page.HasMore)
}

func TestGetChatMessagesDecodesDocsShape(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/compliance/apps/chats/claude_chat_1/messages" {
			t.Errorf("expected path /v1/compliance/apps/chats/claude_chat_1/messages, got %s", r.URL.Path)
		}
		if got := r.URL.Query().Get("order"); got != "asc" {
			t.Errorf("expected order asc, got %s", got)
		}
		if got := r.URL.Query().Get("tool_result_max_chars"); got != "-1" {
			t.Errorf("expected tool_result_max_chars -1, got %s", got)
		}
		if got := r.URL.Query().Get("tool_use_input_max_chars"); got != "-1" {
			t.Errorf("expected tool_use_input_max_chars -1, got %s", got)
		}

		_, _ = w.Write([]byte(`{
			"id": "claude_chat_1",
			"name": "Product Requirements Discussion",
			"created_at": "2026-04-10T08:09:10Z",
			"updated_at": "2026-04-10T09:10:11Z",
			"model": "claude-opus-4-8",
			"organization_uuid": "91012d09-e48b-438e-a489-1bebfd8fa6f9",
			"project_id": "claude_proj_1",
			"user": {"id": "user_1", "email_address": "dev@example.com"},
			"chat_messages": [{
				"id": "claude_chat_msg_1",
				"role": "user",
				"created_at": "2026-04-10T08:09:10Z",
				"content": [{"type": "text", "text": "hello"}],
				"files": [{"id": "claude_file_1", "filename": "mockup.pdf", "mime_type": "application/pdf"}]
			}],
			"has_more": false,
			"first_id": "first",
			"last_id": "last"
		}`))
	}))
	t.Cleanup(server.Close)

	client := New(testGuardianPolicy(t), WithBaseURL(server.URL), WithAPIKey("anthropic-key"))
	page, err := client.GetChatMessages(t.Context(), GetChatMessagesParams{ClaudeChatID: "claude_chat_1"})
	require.NoError(t, err)
	require.Equal(t, "Product Requirements Discussion", page.Name)
	require.NotNil(t, page.Model)
	require.Equal(t, "claude-opus-4-8", *page.Model)
	require.Len(t, page.Messages, 1)
	require.Equal(t, "claude_file_1", page.Messages[0].Files[0].ID)
}

func testGuardianPolicy(t *testing.T) *guardian.Policy {
	t.Helper()

	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), []string{})
	require.NoError(t, err)
	return policy
}
