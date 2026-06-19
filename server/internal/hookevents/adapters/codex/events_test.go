package codex

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/hookevents"
)

func TestNormalize_PreToolUse(t *testing.T) {
	t.Parallel()

	projectID := uuid.New()
	authCtx := &contextvalues.AuthContext{
		ActiveOrganizationID: "org-id",
		ProjectID:            &projectID,
	}
	sessionID := "codex-session"
	userEmail := "dev@example.com"
	toolName := "mcp__linear__list_issues"
	toolInput := map[string]any{"query": "bug"}
	timestamp := time.Unix(123, 0).UTC()

	ev, ok, err := Normalize(authCtx, &gen.CodexPayload{
		HookEventName: "PreToolUse",
		SessionID:     &sessionID,
		UserEmail:     &userEmail,
		ToolName:      &toolName,
		ToolInput:     toolInput,
	}, hookevents.Identity{
		OrganizationID: "org-id",
		ProjectID:      projectID,
		UserID:         "user-id",
		UserEmail:      userEmail,
	}, timestamp)
	require.NoError(t, err)
	require.True(t, ok)

	toolEvent := ev.(*hookevents.BeforeToolUse)
	assert.Equal(t, hookevents.ProviderCodex, toolEvent.Provider)
	assert.Equal(t, hookevents.EventTypeBeforeToolUse, toolEvent.Type)
	assert.Equal(t, "PreToolUse", toolEvent.RawEventType)
	assert.Equal(t, timestamp, toolEvent.Timestamp)
	assert.Equal(t, "org-id", toolEvent.OrganizationID)
	assert.Equal(t, projectID, toolEvent.ProjectID)
	assert.Equal(t, "user-id", toolEvent.UserID)
	assert.Equal(t, userEmail, toolEvent.UserEmail)
	assert.Equal(t, sessionID, toolEvent.ConversationID)
	assert.Equal(t, toolName, toolEvent.ToolName)
	assert.Equal(t, toolInput, toolEvent.ToolInput)
}

func TestNormalize_UnknownEvent(t *testing.T) {
	t.Parallel()

	ev, ok, err := Normalize(nil, &gen.CodexPayload{HookEventName: "SomethingNew"}, hookevents.Identity{}, time.Now())
	require.NoError(t, err)
	assert.False(t, ok)
	assert.Nil(t, ev)
}
