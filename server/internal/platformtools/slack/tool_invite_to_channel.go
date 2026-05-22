package slack

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/platformtools/core"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

const toolNameInviteToChannel = "platform_slack_invite_to_channel"

type inviteToChannelInput struct {
	ChannelID string   `json:"channel_id" jsonschema:"Slack conversation ID to invite users into."`
	Users     []string `json:"users" jsonschema:"User IDs to invite. Up to 100 IDs are accepted in a single call."`
	Force     *bool    `json:"force,omitempty" jsonschema:"When multiple users are supplied, continue inviting the valid ones and ignore invalid IDs instead of failing the whole call."`
}

func NewInviteToChannelTool(httpClient *guardian.HTTPClient) core.PlatformToolExecutor {
	readOnly := false
	destructive := false
	idempotent := false
	openWorld := true

	return &slackTool{
		descriptor: core.ToolDescriptor{
			SourceSlug:  sourceSlack,
			HandlerName: "invite_to_channel",
			Name:        toolNameInviteToChannel,
			Description: "Invite users to a Slack channel using the server's Slack token from SLACK_BOT_TOKEN or SLACK_TOKEN.",
			InputSchema: core.BuildInputSchema[inviteToChannelInput](),
			Variables:   nil,
			Annotations: slackToolAnnotations(readOnly, destructive, idempotent, openWorld),
			Managed:     true,
			OwnerKind:   nil,
			OwnerID:     nil,
		},
		client: newAPIClient(defaultSlackAPIBaseURL, httpClient),
		callFn: callInviteToChannel,
	}
}

func callInviteToChannel(ctx context.Context, client *apiClient, env toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	var input inviteToChannelInput
	if err := decodePayload(payload, &input); err != nil {
		return err
	}

	channelID, err := requireString("channel_id", input.ChannelID)
	if err != nil {
		return err
	}

	users := make([]string, 0, len(input.Users))
	for _, u := range input.Users {
		if trimmed := strings.TrimSpace(u); trimmed != "" {
			users = append(users, trimmed)
		}
	}
	if len(users) == 0 {
		return fmt.Errorf("users is required")
	}

	request := map[string]any{
		"channel": channelID,
		"users":   users,
	}
	setOptionalBool(request, "force", input.Force)

	body, err := client.call(ctx, "conversations.invite", request, tokenPreferBot, env)
	if err != nil {
		return err
	}
	return writeResponse(wr, body)
}
