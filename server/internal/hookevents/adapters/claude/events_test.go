package claude

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

func TestNormalize_UserPromptSubmit(t *testing.T) {
	t.Parallel()

	projectID := uuid.New()
	authCtx := &contextvalues.AuthContext{
		ActiveOrganizationID: "org-id",
		ProjectID:            &projectID,
	}
	sessionID := "claude-session"
	userEmail := "dev@example.com"
	prompt := "fix the bug"
	timestamp := time.Unix(123, 0).UTC()

	ev, ok, err := Normalize(authCtx, &gen.ClaudePayload{
		HookEventName: "UserPromptSubmit",
		SessionID:     &sessionID,
		UserEmail:     &userEmail,
		Prompt:        &prompt,
	}, hookevents.Identity{
		OrganizationID: "org-id",
		ProjectID:      projectID,
		UserID:         "user-id",
		UserEmail:      userEmail,
	}, timestamp)
	require.NoError(t, err)
	require.True(t, ok)

	promptEvent := ev.(*hookevents.UserPromptSubmit)
	assert.Equal(t, hookevents.ProviderClaude, promptEvent.Provider)
	assert.Equal(t, hookevents.EventTypeUserPromptSubmit, promptEvent.Type)
	assert.Equal(t, "UserPromptSubmit", promptEvent.RawEventType)
	assert.Equal(t, timestamp, promptEvent.Timestamp)
	assert.Equal(t, "org-id", promptEvent.OrganizationID)
	assert.Equal(t, projectID, promptEvent.ProjectID)
	assert.Equal(t, "user-id", promptEvent.UserID)
	assert.Equal(t, userEmail, promptEvent.UserEmail)
	assert.Equal(t, sessionID, promptEvent.ConversationID)
	assert.Equal(t, prompt, promptEvent.Prompt)
}

func TestNormalize_UnknownEvent(t *testing.T) {
	t.Parallel()

	ev, ok, err := Normalize(nil, &gen.ClaudePayload{HookEventName: "SomethingNew"}, hookevents.Identity{}, time.Now())
	require.NoError(t, err)
	assert.False(t, ok)
	assert.Nil(t, ev)
}
