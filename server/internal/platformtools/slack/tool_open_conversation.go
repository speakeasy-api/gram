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

const toolNameOpenConversation = "platform_slack_open_conversation"

type openConversationInput struct {
	ChannelID       *string  `json:"channel_id,omitempty" jsonschema:"Existing IM or MPIM ID to resume. Provide either this or users."`
	Users           []string `json:"users,omitempty" jsonschema:"1-8 Slack user IDs to open a new direct or multi-party conversation with. Provide either this or channel_id."`
	ReturnIM        *bool    `json:"return_im,omitempty" jsonschema:"Return the full IM channel definition in the response."`
	PreventCreation *bool    `json:"prevent_creation,omitempty" jsonschema:"Do not create a new conversation if one does not already exist."`
}

func NewOpenConversationTool(httpClient *guardian.HTTPClient) core.PlatformToolExecutor {
	readOnly := false
	destructive := false
	idempotent := true
	openWorld := true

	return &slackTool{
		descriptor: core.ToolDescriptor{
			SourceSlug:  sourceSlack,
			HandlerName: "open_conversation",
			Name:        toolNameOpenConversation,
			Description: "Open or resume a Slack direct or multi-party direct message conversation using the server's Slack token from SLACK_BOT_TOKEN or SLACK_TOKEN. Provide either channel_id to resume an existing IM/MPIM or users (1-8 user IDs) to open a new one.",
			InputSchema: core.BuildInputSchema[openConversationInput](),
			Variables:   nil,
			Annotations: slackToolAnnotations(readOnly, destructive, idempotent, openWorld),
			Managed:     true,
			OwnerKind:   nil,
			OwnerID:     nil,
		},
		client: newAPIClient(defaultSlackAPIBaseURL, httpClient),
		callFn: callOpenConversation,
	}
}

func callOpenConversation(ctx context.Context, client *apiClient, env toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	var input openConversationInput
	if err := decodePayload(payload, &input); err != nil {
		return err
	}

	channelID := strings.TrimSpace(derefString(input.ChannelID))
	users := make([]string, 0, len(input.Users))
	for _, u := range input.Users {
		if trimmed := strings.TrimSpace(u); trimmed != "" {
			users = append(users, trimmed)
		}
	}

	switch {
	case channelID == "" && len(users) == 0:
		return fmt.Errorf("either channel_id or users is required")
	case channelID != "" && len(users) > 0:
		return fmt.Errorf("provide only one of channel_id or users")
	}

	request := map[string]any{}
	if channelID != "" {
		request["channel"] = channelID
	}
	if len(users) > 0 {
		request["users"] = users
	}
	setOptionalBool(request, "return_im", input.ReturnIM)
	setOptionalBool(request, "prevent_creation", input.PreventCreation)

	body, err := client.call(ctx, "conversations.open", request, tokenPreferBot, env)
	if err != nil {
		return err
	}
	return writeResponse(wr, body)
}
