package slack

import (
	"context"
	"fmt"
	"io"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/platformtools/core"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

const toolNameUpdateMessage = "platform_slack_update_message"

type updateMessageInput struct {
	ChannelID      string       `json:"channel_id" jsonschema:"Channel containing the message to update."`
	TS             string       `json:"ts" jsonschema:"Timestamp of the message to update."`
	Text           *string      `json:"text,omitempty" jsonschema:"Replacement message text. At least one of text, blocks, or attachments must be provided."`
	Blocks         []slackBlock `json:"blocks,omitempty" jsonschema:"Replacement Block Kit blocks. At least one of text, blocks, or attachments must be provided."`
	Attachments    *string      `json:"attachments,omitempty" jsonschema:"Replacement attachments as a JSON-encoded array of structured attachments."`
	LinkNames      *bool        `json:"link_names,omitempty" jsonschema:"Find and link channel names and usernames in the updated text."`
	Parse          *string      `json:"parse,omitempty" jsonschema:"Override Slack message parsing. Accepts 'none' or 'full'."`
	ReplyBroadcast *bool        `json:"reply_broadcast,omitempty" jsonschema:"Broadcast an existing thread reply to the channel."`
	FileIDs        []string     `json:"file_ids,omitempty" jsonschema:"New file ids to attach to the updated message."`
}

func NewChatUpdateTool(httpClient *guardian.HTTPClient) core.PlatformToolExecutor {
	readOnly := false
	destructive := false
	idempotent := true
	openWorld := true

	return &slackTool{
		descriptor: core.ToolDescriptor{
			SourceSlug:  sourceSlack,
			HandlerName: "update_message",
			Name:        toolNameUpdateMessage,
			Description: "Update an existing Slack message using the server's Slack token from SLACK_BOT_TOKEN or SLACK_TOKEN. At least one of text, blocks, or attachments must be supplied.",
			InputSchema: core.BuildInputSchema[updateMessageInput](),
			Variables:   nil,
			Annotations: slackToolAnnotations(readOnly, destructive, idempotent, openWorld),
			Managed:     true,
			OwnerKind:   nil,
			OwnerID:     nil,
		},
		client: newAPIClient(defaultSlackAPIBaseURL, httpClient),
		callFn: callUpdateMessage,
	}
}

func callUpdateMessage(ctx context.Context, client *apiClient, env toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	var input updateMessageInput
	if err := decodePayload(payload, &input); err != nil {
		return err
	}

	channelID, err := requireString("channel_id", input.ChannelID)
	if err != nil {
		return err
	}
	ts, err := requireString("ts", input.TS)
	if err != nil {
		return err
	}

	hasText := input.Text != nil && *input.Text != ""
	hasAttachments := input.Attachments != nil && *input.Attachments != ""
	if !hasText && len(input.Blocks) == 0 && !hasAttachments {
		return fmt.Errorf("at least one of text, blocks, or attachments is required")
	}

	request := map[string]any{
		"channel": channelID,
		"ts":      ts,
	}
	setOptionalString(request, "text", input.Text)
	setOptionalString(request, "attachments", input.Attachments)
	setOptionalString(request, "parse", input.Parse)
	setOptionalBool(request, "link_names", input.LinkNames)
	setOptionalBool(request, "reply_broadcast", input.ReplyBroadcast)
	if len(input.Blocks) > 0 {
		request["blocks"] = input.Blocks
	}
	if len(input.FileIDs) > 0 {
		request["file_ids"] = input.FileIDs
	}

	body, err := client.call(ctx, "chat.update", request, tokenPreferBot, env)
	if err != nil {
		return err
	}
	return writeResponse(wr, body)
}
