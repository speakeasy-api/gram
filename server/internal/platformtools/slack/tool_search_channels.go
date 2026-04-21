package slack

import (
	"context"
	"io"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/platformtools/core"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

const toolNameSearchChannels = "platform_slack_search_channels"

var defaultSearchChannelTypes = []string{"public_channel"}

type searchChannelsInput struct {
	Query           *string  `json:"query,omitempty" jsonschema:"Optional case-insensitive substring filter applied to channel names."`
	ChannelTypes    []string `json:"channel_types,omitempty" jsonschema:"Conversation types to include. Defaults to public_channel."`
	Cursor          *string  `json:"cursor,omitempty" jsonschema:"Pagination cursor from a previous response."`
	Limit           *int     `json:"limit,omitempty" jsonschema:"Maximum number of channels to fetch per page. Slack allows up to 1000."`
	ExcludeArchived *bool    `json:"exclude_archived,omitempty" jsonschema:"Exclude archived channels from the response."`
}

func NewSearchChannelsTool(httpClient *guardian.HTTPClient) core.PlatformToolExecutor {
	readOnly := true
	destructive := false
	idempotent := true
	openWorld := true

	return &slackTool{
		descriptor: core.ToolDescriptor{
			SourceSlug:  sourceSlack,
			HandlerName: "search_channels",
			Name:        toolNameSearchChannels,
			Description: "List Slack conversations via conversations.list using the server's bot or user token. Optionally filters channels client-side by a name substring.",
			InputSchema: core.BuildInputSchema[searchChannelsInput](
				core.WithPropertyNumberRange("limit", 1, 1000),
			),
			Variables:   nil,
			Annotations: slackToolAnnotations(readOnly, destructive, idempotent, openWorld),
			Managed:     true,
			OwnerKind:   nil,
			OwnerID:     nil,
		},
		client: newAPIClient(defaultSlackAPIBaseURL, httpClient),
		callFn: callSearchChannels,
	}
}

func callSearchChannels(ctx context.Context, client *apiClient, env toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	var input searchChannelsInput
	if err := decodePayload(payload, &input); err != nil {
		return err
	}

	request := map[string]any{
		"types": strings.Join(defaultChannelTypes(input.ChannelTypes), ","),
	}
	setOptionalString(request, "cursor", input.Cursor)
	setOptionalInt(request, "limit", input.Limit)
	setOptionalBool(request, "exclude_archived", input.ExcludeArchived)

	body, err := client.call(ctx, "conversations.list", request, tokenPreferBot, env)
	if err != nil {
		return err
	}

	filtered, err := filterListResponse(body, "channels", channelMatchesQuery(input.Query))
	if err != nil {
		return err
	}
	return writeResponse(wr, filtered)
}

func defaultChannelTypes(channelTypes []string) []string {
	if len(channelTypes) == 0 {
		return append([]string(nil), defaultSearchChannelTypes...)
	}
	return append([]string(nil), channelTypes...)
}

func channelMatchesQuery(query *string) func(map[string]any) bool {
	needle := strings.ToLower(strings.TrimSpace(derefString(query)))
	if needle == "" {
		return nil
	}
	return func(entry map[string]any) bool {
		return stringFieldContains(entry, needle, "name", "name_normalized")
	}
}
