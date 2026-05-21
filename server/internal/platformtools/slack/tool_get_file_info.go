package slack

import (
	"context"
	"io"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/platformtools/core"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

const toolNameGetFileInfo = "platform_slack_get_file_info"

type getFileInfoInput struct {
	File   string  `json:"file" jsonschema:"Slack file ID to inspect."`
	Cursor *string `json:"cursor,omitempty" jsonschema:"Pagination cursor for paging through file comments."`
	Limit  *int    `json:"limit,omitempty" jsonschema:"Maximum number of comments to return per page."`
	Page   *int    `json:"page,omitempty" jsonschema:"1-indexed page number when paginating comments via page/count."`
	Count  *int    `json:"count,omitempty" jsonschema:"Number of comments per page when paginating via page/count. Defaults to 100."`
}

func NewGetFileInfoTool(httpClient *guardian.HTTPClient) core.PlatformToolExecutor {
	readOnly := true
	destructive := false
	idempotent := true
	openWorld := true

	return &slackTool{
		descriptor: core.ToolDescriptor{
			SourceSlug:  sourceSlack,
			HandlerName: "get_file_info",
			Name:        toolNameGetFileInfo,
			Description: "Fetch metadata and comment history for a Slack file via files.info. Requires the files:read scope on the server's Slack token (SLACK_BOT_TOKEN or SLACK_TOKEN).",
			InputSchema: core.BuildInputSchema[getFileInfoInput](),
			Variables:   nil,
			Annotations: slackToolAnnotations(readOnly, destructive, idempotent, openWorld),
			Managed:     true,
			OwnerKind:   nil,
			OwnerID:     nil,
		},
		client: newAPIClient(defaultSlackAPIBaseURL, httpClient),
		callFn: callGetFileInfo,
	}
}

func callGetFileInfo(ctx context.Context, client *apiClient, env toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	var input getFileInfoInput
	if err := decodePayload(payload, &input); err != nil {
		return err
	}

	file, err := requireString("file", input.File)
	if err != nil {
		return err
	}

	request := map[string]any{
		"file": file,
	}
	setOptionalString(request, "cursor", input.Cursor)
	setOptionalInt(request, "limit", input.Limit)
	setOptionalInt(request, "page", input.Page)
	setOptionalInt(request, "count", input.Count)

	body, err := client.call(ctx, "files.info", request, tokenPreferBot, env)
	if err != nil {
		return err
	}
	return writeResponse(wr, body)
}
