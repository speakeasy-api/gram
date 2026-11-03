package slack

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/platformtools/core"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

// ProjectTokenSource resolves the bot token of the Slack workspace whose
// Speakeasy app answers for a project.
type ProjectTokenSource interface {
	ProjectBotToken(ctx context.Context, organizationID string, projectID uuid.UUID) (string, error)
}

// NewSpeakeasyAppTools returns the Slack tools a project's managed assistant
// uses to reply through the Speakeasy Slack app. They post with the bot token
// of the workspace that routes to the caller's project, which never reaches
// the assistant runtime. Only tools the app's chat:write scope covers are
// included.
func NewSpeakeasyAppTools(httpClient *guardian.HTTPClient, router ThreadRouter, tokens ProjectTokenSource) []core.PlatformToolExecutor {
	inner := []core.PlatformToolExecutor{
		NewSendMessageTool(httpClient, router),
		NewChatPostEphemeralTool(httpClient),
		NewChatUpdateTool(httpClient),
		NewSetThreadStatusTool(httpClient),
		NewChatGetPermalinkTool(httpClient),
	}
	tools := make([]core.PlatformToolExecutor, 0, len(inner))
	for _, tool := range inner {
		tools = append(tools, &speakeasyAppTool{inner: tool, tokens: tokens})
	}
	return tools
}

// speakeasyAppTool runs a Slack tool with the Speakeasy app's bot token for
// the calling project in place of any token in the call's environment.
type speakeasyAppTool struct {
	inner  core.PlatformToolExecutor
	tokens ProjectTokenSource
}

func (t *speakeasyAppTool) Descriptor() core.ToolDescriptor {
	return t.inner.Descriptor()
}

func (t *speakeasyAppTool) Call(ctx context.Context, env toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	if t.tokens == nil {
		return errors.New("the Speakeasy Slack app is not configured")
	}
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil || authCtx.ActiveOrganizationID == "" {
		return errors.New("the Speakeasy Slack app tools require a project")
	}
	token, err := t.tokens.ProjectBotToken(ctx, authCtx.ActiveOrganizationID, *authCtx.ProjectID)
	if err != nil {
		return fmt.Errorf("no Slack workspace answers for this project: %w", err)
	}
	systemEnv := toolconfig.NewCaseInsensitiveEnv()
	systemEnv.Set(slackBotTokenEnvVar, token)
	env.SystemEnv = systemEnv
	env.UserConfig = toolconfig.NewCaseInsensitiveEnv()
	if err := t.inner.Call(ctx, env, payload, wr); err != nil {
		return fmt.Errorf("%s: %w", t.inner.Descriptor().Name, err)
	}
	return nil
}
