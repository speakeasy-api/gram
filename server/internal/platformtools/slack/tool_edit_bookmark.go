package slack

import (
	"context"
	"io"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/platformtools/core"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

const toolNameEditBookmark = "platform_slack_edit_bookmark"

type editBookmarkInput struct {
	BookmarkID string  `json:"bookmark_id" jsonschema:"ID of the bookmark to edit."`
	ChannelID  string  `json:"channel_id" jsonschema:"Slack conversation ID owning the bookmark."`
	Title      *string `json:"title,omitempty" jsonschema:"Optional new title for the bookmark."`
	Link       *string `json:"link,omitempty" jsonschema:"Optional new URL for the bookmark."`
	Emoji      *string `json:"emoji,omitempty" jsonschema:"Optional new emoji tag (e.g. \":memo:\") for the bookmark."`
}

func NewEditBookmarkTool(httpClient *guardian.HTTPClient) core.PlatformToolExecutor {
	readOnly := false
	destructive := false
	idempotent := true
	openWorld := true

	return &slackTool{
		descriptor: core.ToolDescriptor{
			SourceSlug:  sourceSlack,
			HandlerName: "edit_bookmark",
			Name:        toolNameEditBookmark,
			Description: "Edit an existing Slack channel bookmark using the server's Slack token from SLACK_BOT_TOKEN or SLACK_TOKEN.",
			InputSchema: core.BuildInputSchema[editBookmarkInput](),
			Variables:   nil,
			Annotations: slackToolAnnotations(readOnly, destructive, idempotent, openWorld),
			Managed:     true,
			OwnerKind:   nil,
			OwnerID:     nil,
		},
		client: newAPIClient(defaultSlackAPIBaseURL, httpClient),
		callFn: callEditBookmark,
	}
}

func callEditBookmark(ctx context.Context, client *apiClient, env toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	var input editBookmarkInput
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
	setOptionalString(request, "title", input.Title)
	setOptionalString(request, "link", input.Link)
	setOptionalString(request, "emoji", input.Emoji)

	body, err := client.call(ctx, "bookmarks.edit", request, tokenPreferBot, env)
	if err != nil {
		return err
	}
	return writeResponse(wr, body)
}
