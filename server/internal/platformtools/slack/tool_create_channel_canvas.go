package slack

import (
	"context"
	"io"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/platformtools/core"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

const toolNameCreateChannelCanvas = "platform_slack_create_channel_canvas"

type createChannelCanvasInput struct {
	ChannelID       string                 `json:"channel_id" jsonschema:"Conversation ID to attach the canvas to."`
	Title           *string                `json:"title,omitempty" jsonschema:"Optional canvas title."`
	DocumentContent *canvasDocumentContent `json:"document_content,omitempty" jsonschema:"Optional initial canvas content."`
}

func NewCreateChannelCanvasTool(httpClient *guardian.HTTPClient) core.PlatformToolExecutor {
	readOnly := false
	destructive := false
	idempotent := false
	openWorld := true

	return &slackTool{
		descriptor: core.ToolDescriptor{
			SourceSlug:  sourceSlack,
			HandlerName: "create_channel_canvas",
			Name:        toolNameCreateChannelCanvas,
			Description: "Create a Slack canvas tied to a channel via conversations.canvases.create using the server's Slack token from SLACK_BOT_TOKEN or SLACK_TOKEN.",
			InputSchema: core.BuildInputSchema[createChannelCanvasInput](),
			Variables:   nil,
			Annotations: slackToolAnnotations(readOnly, destructive, idempotent, openWorld),
			Managed:     true,
			OwnerKind:   nil,
			OwnerID:     nil,
		},
		client: newAPIClient(defaultSlackAPIBaseURL, httpClient),
		callFn: callCreateChannelCanvas,
	}
}

func callCreateChannelCanvas(ctx context.Context, client *apiClient, env toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	var input createChannelCanvasInput
	if err := decodePayload(payload, &input); err != nil {
		return err
	}

	channelID, err := requireString("channel_id", input.ChannelID)
	if err != nil {
		return err
	}

	request := map[string]any{
		"channel_id": channelID,
	}
	setOptionalString(request, "title", input.Title)
	if input.DocumentContent != nil {
		request["document_content"] = input.DocumentContent
	}

	body, err := client.call(ctx, "conversations.canvases.create", request, tokenPreferBot, env)
	if err != nil {
		return err
	}
	return writeResponse(wr, body)
}
