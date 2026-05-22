package slack

import (
	"context"
	"fmt"
	"io"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/platformtools/core"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

const toolNamePostEphemeral = "platform_slack_post_ephemeral"

type postEphemeralInput struct {
	ChannelID   string       `json:"channel_id" jsonschema:"Channel, private group, or IM channel to post the ephemeral message into."`
	UserID      string       `json:"user_id" jsonschema:"ID of the user who will see the ephemeral message."`
	Text        *string      `json:"text,omitempty" jsonschema:"Message text. Required when neither 'blocks' nor 'attachments' is supplied; otherwise acts as the accessibility fallback."`
	Blocks      []slackBlock `json:"blocks,omitempty" jsonschema:"Optional Block Kit blocks."`
	Attachments *string      `json:"attachments,omitempty" jsonschema:"Optional JSON-encoded array of structured attachments."`
	ThreadTS    *string      `json:"thread_ts,omitempty" jsonschema:"Optional parent message timestamp to anchor the ephemeral message inside a thread."`
	LinkNames   *bool        `json:"link_names,omitempty" jsonschema:"Find and link channel names and usernames."`
	Parse       *string      `json:"parse,omitempty" jsonschema:"Override Slack message parsing. Accepts 'none', 'full', 'mrkdwn', or 'false'."`
	IconEmoji   *string      `json:"icon_emoji,omitempty" jsonschema:"Emoji to use as the message icon."`
	IconURL     *string      `json:"icon_url,omitempty" jsonschema:"URL of an image to use as the message icon."`
	Username    *string      `json:"username,omitempty" jsonschema:"Display name to show as the message sender."`
}

func NewChatPostEphemeralTool(httpClient *guardian.HTTPClient) core.PlatformToolExecutor {
	readOnly := false
	destructive := false
	idempotent := false
	openWorld := true

	return &slackTool{
		descriptor: core.ToolDescriptor{
			SourceSlug:  sourceSlack,
			HandlerName: "post_ephemeral",
			Name:        toolNamePostEphemeral,
			Description: "Post a Slack ephemeral message visible only to the targeted user, using the server's Slack token from SLACK_BOT_TOKEN or SLACK_TOKEN.",
			InputSchema: core.BuildInputSchema[postEphemeralInput](),
			Variables:   nil,
			Annotations: slackToolAnnotations(readOnly, destructive, idempotent, openWorld),
			Managed:     true,
			OwnerKind:   nil,
			OwnerID:     nil,
		},
		client: newAPIClient(defaultSlackAPIBaseURL, httpClient),
		callFn: callPostEphemeral,
	}
}

func callPostEphemeral(ctx context.Context, client *apiClient, env toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	var input postEphemeralInput
	if err := decodePayload(payload, &input); err != nil {
		return err
	}

	channelID, err := requireString("channel_id", input.ChannelID)
	if err != nil {
		return err
	}
	userID, err := requireString("user_id", input.UserID)
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
		"user":    userID,
	}
	setOptionalString(request, "text", input.Text)
	setOptionalString(request, "attachments", input.Attachments)
	setOptionalString(request, "thread_ts", input.ThreadTS)
	setOptionalString(request, "parse", input.Parse)
	setOptionalString(request, "icon_emoji", input.IconEmoji)
	setOptionalString(request, "icon_url", input.IconURL)
	setOptionalString(request, "username", input.Username)
	setOptionalBool(request, "link_names", input.LinkNames)
	if len(input.Blocks) > 0 {
		request["blocks"] = input.Blocks
	}

	body, err := client.call(ctx, "chat.postEphemeral", request, tokenPreferBot, env)
	if err != nil {
		return err
	}
	return writeResponse(wr, body)
}
