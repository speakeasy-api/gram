package slack

import (
	"context"
	"io"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/platformtools/core"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

const toolNameRenameChannel = "platform_slack_rename_channel"

type renameChannelInput struct {
	ChannelID string `json:"channel_id" jsonschema:"Slack conversation ID to rename."`
	Name      string `json:"name" jsonschema:"New channel name. Slack lowercases the value and limits it to letters, numbers, hyphens, and underscores (max 80 characters)."`
}

func NewRenameChannelTool(httpClient *guardian.HTTPClient) core.PlatformToolExecutor {
	readOnly := false
	destructive := false
	idempotent := true
	openWorld := true

	return &slackTool{
		descriptor: core.ToolDescriptor{
			SourceSlug:  sourceSlack,
			HandlerName: "rename_channel",
			Name:        toolNameRenameChannel,
			Description: "Rename a Slack channel using the server's Slack token from SLACK_BOT_TOKEN or SLACK_TOKEN.",
			InputSchema: core.BuildInputSchema[renameChannelInput](),
			Variables:   nil,
			Annotations: slackToolAnnotations(readOnly, destructive, idempotent, openWorld),
			Managed:     true,
			OwnerKind:   nil,
			OwnerID:     nil,
		},
		client: newAPIClient(defaultSlackAPIBaseURL, httpClient),
		callFn: callRenameChannel,
	}
}

func callRenameChannel(ctx context.Context, client *apiClient, env toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	var input renameChannelInput
	if err := decodePayload(payload, &input); err != nil {
		return err
	}

	channelID, err := requireString("channel_id", input.ChannelID)
	if err != nil {
		return err
	}
	name, err := requireString("name", input.Name)
	if err != nil {
		return err
	}

	request := map[string]any{
		"channel": channelID,
		"name":    name,
	}

	body, err := client.call(ctx, "conversations.rename", request, tokenPreferBot, env)
	if err != nil {
		return err
	}
	return writeResponse(wr, body)
}
