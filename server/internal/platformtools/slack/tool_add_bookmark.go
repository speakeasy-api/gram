package slack

import (
	"context"
	"io"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/platformtools/core"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

const toolNameAddBookmark = "platform_slack_add_bookmark"

type addBookmarkInput struct {
	ChannelID string  `json:"channel_id" jsonschema:"Slack conversation ID to attach the bookmark to."`
	Title     string  `json:"title" jsonschema:"Title shown in the channel bookmark bar."`
	Type      string  `json:"type" jsonschema:"Bookmark type. Slack currently accepts \"link\"."`
	Link      string  `json:"link" jsonschema:"URL the bookmark resolves to."`
	Emoji     *string `json:"emoji,omitempty" jsonschema:"Optional emoji tag (e.g. \":memo:\") displayed alongside the bookmark."`
	EntityID  *string `json:"entity_id,omitempty" jsonschema:"Optional ID of the bookmarked entity (messages or files only)."`
	ParentID  *string `json:"parent_id,omitempty" jsonschema:"Optional ID of a parent bookmark to nest under."`
}

func NewAddBookmarkTool(httpClient *guardian.HTTPClient) core.PlatformToolExecutor {
	readOnly := false
	destructive := false
	idempotent := false
	openWorld := true

	return &slackTool{
		descriptor: core.ToolDescriptor{
			SourceSlug:  sourceSlack,
			HandlerName: "add_bookmark",
			Name:        toolNameAddBookmark,
			Description: "Add a bookmark to a Slack channel using the server's Slack token from SLACK_BOT_TOKEN or SLACK_TOKEN.",
			InputSchema: core.BuildInputSchema[addBookmarkInput](),
			Variables:   nil,
			Annotations: slackToolAnnotations(readOnly, destructive, idempotent, openWorld),
			Managed:     true,
			OwnerKind:   nil,
			OwnerID:     nil,
		},
		client: newAPIClient(defaultSlackAPIBaseURL, httpClient),
		callFn: callAddBookmark,
	}
}

func callAddBookmark(ctx context.Context, client *apiClient, env toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	var input addBookmarkInput
	if err := decodePayload(payload, &input); err != nil {
		return err
	}

	channelID, err := requireString("channel_id", input.ChannelID)
	if err != nil {
		return err
	}
	title, err := requireString("title", input.Title)
	if err != nil {
		return err
	}
	bookmarkType, err := requireString("type", input.Type)
	if err != nil {
		return err
	}
	link, err := requireString("link", input.Link)
	if err != nil {
		return err
	}

	request := map[string]any{
		"channel_id": channelID,
		"title":      title,
		"type":       bookmarkType,
		"link":       link,
	}
	setOptionalString(request, "emoji", input.Emoji)
	setOptionalString(request, "entity_id", input.EntityID)
	setOptionalString(request, "parent_id", input.ParentID)

	body, err := client.call(ctx, "bookmarks.add", request, tokenPreferBot, env)
	if err != nil {
		return err
	}
	return writeResponse(wr, body)
}
