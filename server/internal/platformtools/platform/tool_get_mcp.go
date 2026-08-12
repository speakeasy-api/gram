package platform

import (
	"context"
	"fmt"
	"io"

	"github.com/speakeasy-api/gram/server/internal/platformmcp"
	"github.com/speakeasy-api/gram/server/internal/platformtools"
	"github.com/speakeasy-api/gram/server/internal/platformtools/core"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

type GetMCP struct {
	reader platformmcp.Reader
}

func NewGetMCPTool(reader platformmcp.Reader) *GetMCP {
	return &GetMCP{reader: reader}
}

func (t *GetMCP) Descriptor() core.ToolDescriptor {
	return core.ToolDescriptor{
		SourceSlug:  platformtools.SourcePlatform,
		HandlerName: "get_mcp",
		Name:        platformtools.ToolNameGetMCP,
		Description: "Get an allowlisted summary of one configured MCP in an explicit project.",
		InputSchema: core.BuildInputSchema[platformmcp.GetMCPInput](),
		Variables:   nil,
		Annotations: core.ReadOnlyAnnotations(),
		Managed:     true,
		OwnerKind:   nil,
		OwnerID:     nil,
	}
}

func (t *GetMCP) Call(ctx context.Context, _ toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	if t.reader == nil {
		return fmt.Errorf("platform reader not configured")
	}

	input := platformmcp.GetMCPInput{ProjectID: "", MCPID: ""}
	if err := core.DecodeInput(payload, &input); err != nil {
		return err
	}
	if input.ProjectID == "" || input.MCPID == "" {
		return fmt.Errorf("project_id and mcp_id are required")
	}

	principal, err := principalFromContext(ctx)
	if err != nil {
		return err
	}

	output, err := t.reader.GetMCP(ctx, principal, input)
	if err != nil {
		return fmt.Errorf("get configured mcp: %w", err)
	}

	return core.EncodeResult(wr, output)
}
