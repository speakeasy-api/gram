package slack

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/platformtools/core"
	slackapi "github.com/speakeasy-api/gram/server/internal/thirdparty/slack/api"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

// AssistantSlackCredentials holds the decrypted tokens from one slack_app
// typed auth client. Values are never logged or returned to callers.
type AssistantSlackCredentials struct {
	BotToken  string
	UserToken string
	Token     string
}

// AssistantSlackCredentialSource resolves the single enabled slack_app auth
// client authorized for the calling agent in (organizationID, projectID).
type AssistantSlackCredentialSource interface {
	ResolveForCaller(ctx context.Context, organizationID string, projectID uuid.UUID) (AssistantSlackCredentials, error)
}

// AllTools returns every assistant Slack platform tool executor in canonical
// order for the built-in assistant-slack toolset. Kept in lockstep with
// platformtools/registry.go (enforced by TestAssistantSlackToolsCoverEverySlackPlatformTool).
func AllTools(httpClient *guardian.HTTPClient, router ThreadRouter, enc *encryption.Client, serverURL *url.URL) []core.PlatformToolExecutor {
	return []core.PlatformToolExecutor{
		NewReadChannelMessagesTool(httpClient),
		NewReadThreadMessagesTool(httpClient),
		NewReadUserProfileTool(httpClient),
		NewSearchChannelsTool(httpClient),
		NewSearchMessagesAndFilesTool(httpClient),
		NewSearchUsersTool(httpClient),
		NewScheduleMessageTool(httpClient),
		NewSendMessageTool(httpClient, router),
		NewAddReactionTool(httpClient),
		NewRemoveReactionTool(httpClient),
		NewGetReactionsTool(httpClient),
		NewListReactionsTool(httpClient),
		NewListEmojiTool(httpClient),
		NewChatUpdateTool(httpClient),
		NewSetThreadStatusTool(httpClient),
		NewChatDeleteTool(httpClient),
		NewChatPostEphemeralTool(httpClient),
		NewChatGetPermalinkTool(httpClient),
		NewChatDeleteScheduledMessageTool(httpClient),
		NewChatListScheduledMessagesTool(httpClient),
		NewChatMeMessageTool(httpClient),
		NewGetChannelInfoTool(httpClient),
		NewListChannelMembersTool(httpClient),
		NewOpenConversationTool(httpClient),
		NewCreateChannelTool(httpClient),
		NewJoinChannelTool(httpClient),
		NewLeaveChannelTool(httpClient),
		NewInviteToChannelTool(httpClient),
		NewSetChannelTopicTool(httpClient),
		NewSetChannelPurposeTool(httpClient),
		NewMarkConversationTool(httpClient),
		NewArchiveChannelTool(httpClient),
		NewUnarchiveChannelTool(httpClient),
		NewRenameChannelTool(httpClient),
		NewRemoveFromChannelTool(httpClient),
		NewLookupUserByEmailTool(httpClient),
		NewListUserConversationsTool(httpClient),
		NewGetUserPresenceTool(httpClient),
		NewGetUserProfileFieldsTool(httpClient),
		NewGetUserDndTool(httpClient),
		NewGetTeamDndTool(httpClient),
		NewAddReminderTool(httpClient),
		NewListRemindersTool(httpClient),
		NewCompleteReminderTool(httpClient),
		NewDeleteReminderTool(httpClient),
		NewGetReminderTool(httpClient),
		NewUploadFileTool(httpClient),
		NewGetFileInfoTool(httpClient),
		NewGetFileURLTool(httpClient, enc, serverURL),
		NewListFilesTool(httpClient),
		NewDeleteFileTool(httpClient),
		NewPinMessageTool(httpClient),
		NewUnpinMessageTool(httpClient),
		NewListPinsTool(httpClient),
		NewAddBookmarkTool(httpClient),
		NewEditBookmarkTool(httpClient),
		NewRemoveBookmarkTool(httpClient),
		NewListBookmarksTool(httpClient),
		NewListUsergroupsTool(httpClient),
		NewListUsergroupMembersTool(httpClient),
		NewGetTeamInfoTool(httpClient),
		NewCreateCanvasTool(httpClient),
		NewEditCanvasTool(httpClient),
		NewDeleteCanvasTool(httpClient),
		NewLookupCanvasSectionsTool(httpClient),
		NewSetCanvasAccessTool(httpClient),
		NewRemoveCanvasAccessTool(httpClient),
		NewCreateChannelCanvasTool(httpClient),
	}
}

// NewAssistantSlackTools returns the full Slack platform toolset wrapped so
// every call resolves its Slack app tokens from the caller's authorized
// slack_app typed auth client instead of a project environment.
func NewAssistantSlackTools(
	httpClient *guardian.HTTPClient,
	router ThreadRouter,
	enc *encryption.Client,
	serverURL *url.URL,
	credentials AssistantSlackCredentialSource,
) []core.PlatformToolExecutor {
	raw := AllTools(httpClient, router, enc, serverURL)
	tools := make([]core.PlatformToolExecutor, 0, len(raw))
	for _, tool := range raw {
		inner, ok := tool.(*slackTool)
		if !ok {
			panic(fmt.Sprintf("platformtools/slack: unexpected tool implementation %T", tool))
		}
		tools = append(tools, &assistantSlackTool{inner: inner, credentials: credentials})
	}
	return tools
}

// assistantSlackTool executes a Slack platform tool using credentials resolved
// from the caller's authorized slack_app auth client, ignoring any incoming
// environment variables.
type assistantSlackTool struct {
	inner       *slackTool
	credentials AssistantSlackCredentialSource
}

func (t *assistantSlackTool) Descriptor() core.ToolDescriptor {
	return t.inner.Descriptor()
}

func (t *assistantSlackTool) Call(ctx context.Context, env toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	if t.credentials == nil {
		return oops.E(oops.CodeFailedPrecondition, nil, "assistant Slack credentials are not configured")
	}
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil || authCtx.ActiveOrganizationID == "" {
		return oops.E(oops.CodeFailedPrecondition, nil, "assistant Slack tools require a project")
	}
	creds, err := t.credentials.ResolveForCaller(ctx, authCtx.ActiveOrganizationID, *authCtx.ProjectID)
	if err != nil {
		return fmt.Errorf("resolve assistant Slack credentials: %w", err)
	}
	systemEnv := toolconfig.NewCaseInsensitiveEnv()
	for _, pair := range []struct {
		key string
		val string
	}{
		{slackBotTokenEnvVar, creds.BotToken},
		{slackUserTokenEnvVar, creds.UserToken},
		{slackTokenEnvVar, creds.Token},
	} {
		if pair.val == "" {
			continue
		}
		if strings.ContainsFunc(pair.val, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
			return oops.E(oops.CodeFailedPrecondition, nil, "Slack app auth client contains an invalid %s token; an organization admin must re-enter it", pair.key)
		}
		systemEnv.Set(pair.key, pair.val)
	}
	env.SystemEnv = systemEnv
	env.UserConfig = toolconfig.NewCaseInsensitiveEnv()
	// call, not Call: environment-based setup advice does not apply to a
	// typed slack_app auth client.
	return authClientConfigError(t.inner.call(ctx, env, payload, wr), creds)
}

func authClientConfigError(err error, creds AssistantSlackCredentials) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	for _, secret := range []string{creds.BotToken, creds.UserToken, creds.Token} {
		if secret != "" && strings.Contains(msg, secret) {
			return oops.E(oops.CodeUnexpected, nil, "Slack request failed")
		}
	}
	if missing, ok := errors.AsType[*slackapi.TokenNotConfiguredError](err); ok {
		return oops.E(
			oops.CodeFailedPrecondition,
			err,
			"Slack app auth client is missing %s; an organization admin must add it to this assistant's slack_app auth client",
			missingTokenVariables(missing),
		)
	}
	slackErr, ok := errors.AsType[*slackapi.Error](err)
	if !ok {
		return err
	}
	if slackErr.Code == "missing_scope" {
		scope := "a scope"
		if slackErr.Needed != "" {
			scope = "the " + slackErr.Needed + " scope"
		}
		return oops.E(
			oops.CodeFailedPrecondition,
			err,
			"Slack app auth client token lacks %s, which %s needs; a Slack workspace admin must add the scope, reinstall the app, and update the slack_app auth client token",
			scope,
			slackErr.Method,
		)
	}
	if _, rejected := slackTokenRejectedCodes[slackErr.Code]; rejected {
		return oops.E(
			oops.CodeFailedPrecondition,
			err,
			"Slack rejected the slack_app auth client token (%s); an organization admin must replace the token on this assistant's slack_app auth client",
			slackErr.Code,
		)
	}
	return err
}
