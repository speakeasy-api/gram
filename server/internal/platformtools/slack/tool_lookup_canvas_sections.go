package slack

import (
	"context"
	"fmt"
	"io"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/platformtools/core"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

const toolNameLookupCanvasSections = "platform_slack_lookup_canvas_sections"

type canvasSectionsCriteria struct {
	ContainsText *string  `json:"contains_text,omitempty" jsonschema:"Substring to match against section text."`
	SectionTypes []string `json:"section_types,omitempty" jsonschema:"Section kinds to match (e.g. h1, h2, h3, any_header)."`
}

type lookupCanvasSectionsInput struct {
	CanvasID string                 `json:"canvas_id" jsonschema:"ID of the canvas to inspect."`
	Criteria canvasSectionsCriteria `json:"criteria" jsonschema:"Lookup criteria applied to the canvas sections."`
}

func NewLookupCanvasSectionsTool(httpClient *guardian.HTTPClient) core.PlatformToolExecutor {
	readOnly := true
	destructive := false
	idempotent := true
	openWorld := true

	return &slackTool{
		descriptor: core.ToolDescriptor{
			SourceSlug:  sourceSlack,
			HandlerName: "lookup_canvas_sections",
			Name:        toolNameLookupCanvasSections,
			Description: "Find sections inside a Slack canvas via canvases.sections.lookup using the server's Slack token from SLACK_BOT_TOKEN or SLACK_TOKEN.",
			InputSchema: core.BuildInputSchema[lookupCanvasSectionsInput](),
			Variables:   nil,
			Annotations: slackToolAnnotations(readOnly, destructive, idempotent, openWorld),
			Managed:     true,
			OwnerKind:   nil,
			OwnerID:     nil,
		},
		client: newAPIClient(defaultSlackAPIBaseURL, httpClient),
		callFn: callLookupCanvasSections,
	}
}

func callLookupCanvasSections(ctx context.Context, client *apiClient, env toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	var input lookupCanvasSectionsInput
	if err := decodePayload(payload, &input); err != nil {
		return err
	}

	canvasID, err := requireString("canvas_id", input.CanvasID)
	if err != nil {
		return err
	}
	if input.Criteria.ContainsText == nil && len(input.Criteria.SectionTypes) == 0 {
		return fmt.Errorf("criteria is required")
	}

	request := map[string]any{
		"canvas_id": canvasID,
		"criteria":  input.Criteria,
	}

	body, err := client.call(ctx, "canvases.sections.lookup", request, tokenPreferBot, env)
	if err != nil {
		return err
	}
	return writeResponse(wr, body)
}
