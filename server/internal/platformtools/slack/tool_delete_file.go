package slack

import (
	"context"
	"io"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/platformtools/core"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

const toolNameDeleteFile = "platform_slack_delete_file"

type deleteFileInput struct {
	File string `json:"file" jsonschema:"Slack file ID to delete."`
}

func NewDeleteFileTool(httpClient *guardian.HTTPClient) core.PlatformToolExecutor {
	readOnly := false
	destructive := true
	idempotent := true
	openWorld := true

	return &slackTool{
		descriptor: core.ToolDescriptor{
			SourceSlug:  sourceSlack,
			HandlerName: "delete_file",
			Name:        toolNameDeleteFile,
			Description: "Delete a Slack file via files.delete. Requires the files:write scope on the server's Slack token (SLACK_BOT_TOKEN or SLACK_TOKEN).",
			InputSchema: core.BuildInputSchema[deleteFileInput](),
			Variables:   nil,
			Annotations: slackToolAnnotations(readOnly, destructive, idempotent, openWorld),
			Managed:     true,
			OwnerKind:   nil,
			OwnerID:     nil,
		},
		client: newAPIClient(defaultSlackAPIBaseURL, httpClient),
		callFn: callDeleteFile,
	}
}

func callDeleteFile(ctx context.Context, client *apiClient, env toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	var input deleteFileInput
	if err := decodePayload(payload, &input); err != nil {
		return err
	}

	file, err := requireString("file", input.File)
	if err != nil {
		return err
	}

	body, err := client.call(ctx, "files.delete", map[string]any{"file": file}, tokenPreferBot, env)
	if err != nil {
		return err
	}
	return writeResponse(wr, body)
}
