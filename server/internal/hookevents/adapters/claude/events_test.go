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

	ev, err := Normalize(authCtx, &gen.ClaudePayload{
		HookEventName: "UserPromptSubmit",
		SessionID:     &sessionID,
		UserEmail:     &userEmail,
		Prompt:        &prompt,
	}, hookevents.EventContext{
		OrganizationID: "org-id",
		ProjectID:      projectID,
		User: hookevents.User{
			ID:    "user-id",
			Email: userEmail,
		},
	}, timestamp)
	require.NoError(t, err)
	require.NotNil(t, ev)

	promptEvent, ok := ev.(*hookevents.UserPromptSubmit)
	require.True(t, ok)
	assert.Equal(t, hookevents.ProviderClaude, promptEvent.Provider)
	assert.Equal(t, hookevents.EventTypeUserPromptSubmit, promptEvent.Type)
	assert.Equal(t, "UserPromptSubmit", promptEvent.RawEventType)
	assert.Equal(t, timestamp, promptEvent.Timestamp)
	assert.Equal(t, "org-id", promptEvent.Context.OrganizationID)
	assert.Equal(t, projectID, promptEvent.Context.ProjectID)
	assert.Equal(t, "user-id", promptEvent.Context.User.ID)
	assert.Equal(t, userEmail, promptEvent.Context.User.Email)
	assert.Equal(t, sessionID, promptEvent.ConversationID)
	assert.Equal(t, prompt, promptEvent.Prompt)
}

func TestNormalize_UnknownEvent(t *testing.T) {
	t.Parallel()

	ev, err := Normalize(nil, &gen.ClaudePayload{HookEventName: "SomethingNew"}, hookevents.EventContext{}, time.Now())
	require.NoError(t, err)
	assert.Nil(t, ev)
}
