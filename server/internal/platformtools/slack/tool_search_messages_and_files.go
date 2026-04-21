package slack

import (
	"context"
	"io"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/platformtools/core"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

const toolNameSearchMessagesAndFiles = "platform_slack_search_messages_and_files"

type searchMessagesAndFilesInput struct {
	Query     string  `json:"query" jsonschema:"Search query. Supports Slack modifiers like in:#channel, from:@user, before:2024-01-01, has:link."`
	Page      *int    `json:"page,omitempty" jsonschema:"1-indexed page number to fetch. Slack returns paging metadata in the response."`
	Limit     *int    `json:"limit,omitempty" jsonschema:"Maximum number of results per page. Slack allows up to 100."`
	Highlight *bool   `json:"highlight,omitempty" jsonschema:"Highlight matching text in results."`
	Sort      *string `json:"sort,omitempty" jsonschema:"Sort field. Slack accepts score or timestamp."`
	SortDir   *string `json:"sort_dir,omitempty" jsonschema:"Sort direction. Slack accepts asc or desc."`
}

func NewSearchMessagesAndFilesTool(httpClient *guardian.HTTPClient) core.PlatformToolExecutor {
	readOnly := true
	destructive := false
	idempotent := true
	openWorld := true

	return &slackTool{
		descriptor: core.ToolDescriptor{
			SourceSlug:  sourceSlack,
			HandlerName: "search_messages_and_files",
			Name:        toolNameSearchMessagesAndFiles,
			Description: "Search Slack messages and files via search.all. Requires a user token with search:read (SLACK_USER_TOKEN or SLACK_TOKEN).",
			InputSchema: core.BuildInputSchema[searchMessagesAndFilesInput](
				core.WithPropertyNumberRange("limit", 1, 100),
				core.WithPropertyEnum("sort", "score", "timestamp"),
				core.WithPropertyEnum("sort_dir", "asc", "desc"),
			),
			Variables:   nil,
			Annotations: slackToolAnnotations(readOnly, destructive, idempotent, openWorld),
			Managed:     true,
			OwnerKind:   nil,
			OwnerID:     nil,
		},
		client: newAPIClient(defaultSlackAPIBaseURL, httpClient),
		callFn: callSearchMessagesAndFiles,
	}
}

func callSearchMessagesAndFiles(ctx context.Context, client *apiClient, env toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	var input searchMessagesAndFilesInput
	if err := decodePayload(payload, &input); err != nil {
		return err
	}

	query, err := requireString("query", input.Query)
	if err != nil {
		return err
	}

	request := map[string]any{
		"query": query,
	}
	setOptionalInt(request, "page", input.Page)
	setOptionalInt(request, "count", input.Limit)
	setOptionalBool(request, "highlight", input.Highlight)
	setOptionalString(request, "sort", input.Sort)
	setOptionalString(request, "sort_dir", input.SortDir)

	body, err := client.call(ctx, "search.all", request, tokenRequireUser, env)
	if err != nil {
		return err
	}
	return writeResponse(wr, body)
}
