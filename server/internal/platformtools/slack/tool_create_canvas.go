package slack

import (
	"context"
	"io"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/platformtools/core"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

const toolNameCreateCanvas = "platform_slack_create_canvas"

type createCanvasInput struct {
	Title           *string                `json:"title,omitempty" jsonschema:"Optional canvas title."`
	DocumentContent *canvasDocumentContent `json:"document_content,omitempty" jsonschema:"Optional initial canvas content."`
	ChannelID       *string                `json:"channel_id,omitempty" jsonschema:"Optional conversation ID to associate the canvas with at creation time."`
}

func NewCreateCanvasTool(httpClient *guardian.HTTPClient) core.PlatformToolExecutor {
	readOnly := false
	destructive := false
	idempotent := false
	openWorld := true

	return &slackTool{
		descriptor: core.ToolDescriptor{
			SourceSlug:  sourceSlack,
			HandlerName: "create_canvas",
			Name:        toolNameCreateCanvas,
			Description: "Create a standalone Slack canvas using the server's Slack token from SLACK_BOT_TOKEN or SLACK_TOKEN.",
			InputSchema: core.BuildInputSchema[createCanvasInput](),
			Variables:   nil,
			Annotations: slackToolAnnotations(readOnly, destructive, idempotent, openWorld),
			Managed:     true,
			OwnerKind:   nil,
			OwnerID:     nil,
		},
		client: newAPIClient(defaultSlackAPIBaseURL, httpClient),
		callFn: callCreateCanvas,
	}
}

func callCreateCanvas(ctx context.Context, client *apiClient, env toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	var input createCanvasInput
	if err := decodePayload(payload, &input); err != nil {
		return err
	}

	request := map[string]any{}
	setOptionalString(request, "title", input.Title)
	setOptionalString(request, "channel_id", input.ChannelID)
	if input.DocumentContent != nil {
		request["document_content"] = input.DocumentContent
	}

	body, err := client.call(ctx, "canvases.create", request, tokenPreferBot, env)
	if err != nil {
		return err
	}
	return writeResponse(wr, body)
}
