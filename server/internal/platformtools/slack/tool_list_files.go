package slack

import (
	"context"
	"io"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/platformtools/core"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

const toolNameListFiles = "platform_slack_list_files"

type listFilesInput struct {
	User                   *string `json:"user,omitempty" jsonschema:"Filter to files created by this Slack user ID."`
	Channel                *string `json:"channel,omitempty" jsonschema:"Filter to files shared into this Slack channel ID."`
	TSFrom                 *string `json:"ts_from,omitempty" jsonschema:"Only return files created after this Slack timestamp (inclusive)."`
	TSTo                   *string `json:"ts_to,omitempty" jsonschema:"Only return files created before this Slack timestamp (inclusive)."`
	Types                  *string `json:"types,omitempty" jsonschema:"Comma-separated file type filter (for example: images,pdfs,snippets). Defaults to all."`
	Page                   *int    `json:"page,omitempty" jsonschema:"1-indexed page number to fetch. Defaults to 1."`
	Count                  *int    `json:"count,omitempty" jsonschema:"Number of files per page. Defaults to 100."`
	ShowFilesHiddenByLimit *bool   `json:"show_files_hidden_by_limit,omitempty" jsonschema:"Include truncated info for files hidden by the workspace file-history limit."`
}

func NewListFilesTool(httpClient *guardian.HTTPClient) core.PlatformToolExecutor {
	readOnly := true
	destructive := false
	idempotent := true
	openWorld := true

	return &slackTool{
		descriptor: core.ToolDescriptor{
			SourceSlug:  sourceSlack,
			HandlerName: "list_files",
			Name:        toolNameListFiles,
			Description: "List Slack files via files.list with optional filters by user, channel, time range, and type. Requires the files:read scope on the server's Slack token (SLACK_BOT_TOKEN or SLACK_TOKEN).",
			InputSchema: core.BuildInputSchema[listFilesInput](),
			Variables:   nil,
			Annotations: slackToolAnnotations(readOnly, destructive, idempotent, openWorld),
			Managed:     true,
			OwnerKind:   nil,
			OwnerID:     nil,
		},
		client: newAPIClient(defaultSlackAPIBaseURL, httpClient),
		callFn: callListFiles,
	}
}

func callListFiles(ctx context.Context, client *apiClient, env toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	var input listFilesInput
	if err := decodePayload(payload, &input); err != nil {
		return err
	}

	request := map[string]any{}
	setOptionalString(request, "user", input.User)
	setOptionalString(request, "channel", input.Channel)
	setOptionalString(request, "ts_from", input.TSFrom)
	setOptionalString(request, "ts_to", input.TSTo)
	setOptionalString(request, "types", input.Types)
	setOptionalInt(request, "page", input.Page)
	setOptionalInt(request, "count", input.Count)
	setOptionalBool(request, "show_files_hidden_by_limit", input.ShowFilesHiddenByLimit)

	body, err := client.call(ctx, "files.list", request, tokenPreferBot, env)
	if err != nil {
		return err
	}
	return writeResponse(wr, body)
}
