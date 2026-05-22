package slack

import (
	"context"
	"io"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/platformtools/core"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

const toolNameSetChannelPurpose = "platform_slack_set_channel_purpose"

type setChannelPurposeInput struct {
	ChannelID string `json:"channel_id" jsonschema:"Slack conversation ID whose description should be set."`
	Purpose   string `json:"purpose" jsonschema:"New description. Max 250 characters."`
}

func NewSetChannelPurposeTool(httpClient *guardian.HTTPClient) core.PlatformToolExecutor {
	readOnly := false
	destructive := false
	idempotent := true
	openWorld := true

	return &slackTool{
		descriptor: core.ToolDescriptor{
			SourceSlug:  sourceSlack,
			HandlerName: "set_channel_purpose",
			Name:        toolNameSetChannelPurpose,
			Description: "Set the description (purpose) of a Slack conversation using the server's Slack token from SLACK_BOT_TOKEN or SLACK_TOKEN.",
			InputSchema: core.BuildInputSchema[setChannelPurposeInput](),
			Variables:   nil,
			Annotations: slackToolAnnotations(readOnly, destructive, idempotent, openWorld),
			Managed:     true,
			OwnerKind:   nil,
			OwnerID:     nil,
		},
		client: newAPIClient(defaultSlackAPIBaseURL, httpClient),
		callFn: callSetChannelPurpose,
	}
}

func callSetChannelPurpose(ctx context.Context, client *apiClient, env toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	var input setChannelPurposeInput
	if err := decodePayload(payload, &input); err != nil {
		return err
	}

	channelID, err := requireString("channel_id", input.ChannelID)
	if err != nil {
		return err
	}
	purpose, err := requireString("purpose", input.Purpose)
	if err != nil {
		return err
	}

	request := map[string]any{
		"channel": channelID,
		"purpose": purpose,
	}

	body, err := client.call(ctx, "conversations.setPurpose", request, tokenPreferBot, env)
	if err != nil {
		return err
	}
	return writeResponse(wr, body)
}
