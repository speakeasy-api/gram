package slack

import (
	"context"
	"io"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/platformtools/core"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

const toolNameGetChannelInfo = "platform_slack_get_channel_info"

type getChannelInfoInput struct {
	ChannelID         string `json:"channel_id" jsonschema:"Slack conversation ID to inspect."`
	IncludeLocale     *bool  `json:"include_locale,omitempty" jsonschema:"Include the conversation locale in the response."`
	IncludeNumMembers *bool  `json:"include_num_members,omitempty" jsonschema:"Include the member count for the conversation."`
}

func NewGetChannelInfoTool(httpClient *guardian.HTTPClient) core.PlatformToolExecutor {
	readOnly := true
	destructive := false
	idempotent := true
	openWorld := true

	return &slackTool{
		descriptor: core.ToolDescriptor{
			SourceSlug:  sourceSlack,
			HandlerName: "get_channel_info",
			Name:        toolNameGetChannelInfo,
			Description: "Get metadata for a Slack conversation (channel, DM, or MPIM) using the server's Slack token from SLACK_BOT_TOKEN or SLACK_TOKEN.",
			InputSchema: core.BuildInputSchema[getChannelInfoInput](),
			Variables:   nil,
			Annotations: slackToolAnnotations(readOnly, destructive, idempotent, openWorld),
			Managed:     true,
			OwnerKind:   nil,
			OwnerID:     nil,
		},
		client: newAPIClient(defaultSlackAPIBaseURL, httpClient),
		callFn: callGetChannelInfo,
	}
}

func callGetChannelInfo(ctx context.Context, client *apiClient, env toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	var input getChannelInfoInput
	if err := decodePayload(payload, &input); err != nil {
		return err
	}

	channelID, err := requireString("channel_id", input.ChannelID)
	if err != nil {
		return err
	}

	request := map[string]any{
		"channel": channelID,
	}
	setOptionalBool(request, "include_locale", input.IncludeLocale)
	setOptionalBool(request, "include_num_members", input.IncludeNumMembers)

	body, err := client.call(ctx, "conversations.info", request, tokenPreferBot, env)
	if err != nil {
		return err
	}
	return writeResponse(wr, body)
}
