package slack

import (
	"context"
	"io"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/platformtools/core"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

const toolNameRemoveBookmark = "platform_slack_remove_bookmark"

type removeBookmarkInput struct {
	BookmarkID    string  `json:"bookmark_id" jsonschema:"ID of the bookmark to remove."`
	ChannelID     string  `json:"channel_id" jsonschema:"Slack conversation ID owning the bookmark."`
	QuipSectionID *string `json:"quip_section_id,omitempty" jsonschema:"Optional Quip section ID to unbookmark instead of the top-level bookmark."`
}

func NewRemoveBookmarkTool(httpClient *guardian.HTTPClient) core.PlatformToolExecutor {
	readOnly := false
	destructive := true
	idempotent := true
	openWorld := true

	return &slackTool{
		descriptor: core.ToolDescriptor{
			SourceSlug:  sourceSlack,
			HandlerName: "remove_bookmark",
			Name:        toolNameRemoveBookmark,
			Description: "Remove a bookmark from a Slack channel using the server's Slack token from SLACK_BOT_TOKEN or SLACK_TOKEN.",
			InputSchema: core.BuildInputSchema[removeBookmarkInput](),
			Variables:   nil,
			Annotations: slackToolAnnotations(readOnly, destructive, idempotent, openWorld),
			Managed:     true,
			OwnerKind:   nil,
			OwnerID:     nil,
		},
		client: newAPIClient(defaultSlackAPIBaseURL, httpClient),
		callFn: callRemoveBookmark,
	}
}

func callRemoveBookmark(ctx context.Context, client *apiClient, env toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	var input removeBookmarkInput
	if err := decodePayload(payload, &input); err != nil {
		return err
	}

	bookmarkID, err := requireString("bookmark_id", input.BookmarkID)
	if err != nil {
		return err
	}
	channelID, err := requireString("channel_id", input.ChannelID)
	if err != nil {
		return err
	}

	request := map[string]any{
		"bookmark_id": bookmarkID,
		"channel_id":  channelID,
	}
	setOptionalString(request, "quip_section_id", input.QuipSectionID)

	body, err := client.call(ctx, "bookmarks.remove", request, tokenPreferBot, env)
	if err != nil {
		return err
	}
	return writeResponse(wr, body)
}
