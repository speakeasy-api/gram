package platform

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/platformmcp"
	"github.com/speakeasy-api/gram/server/internal/platformtools"
	"github.com/speakeasy-api/gram/server/internal/platformtools/core"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

type GetMCP struct {
	reader platformmcp.Reader
}

// getMCPInput is this executor's own contract. It names the project by ID,
// which platformmcp.GetMCPInput resolves from a project slug only on the
// Platform MCP surface.
type getMCPInput struct {
	ProjectID  string `json:"project_id" jsonschema:"project ID that owns the MCP"`
	MCPID      string `json:"mcp_id" jsonschema:"configured MCP ID"`
	ToolCursor string `json:"tool_cursor,omitempty" jsonschema:"tool_exposure.next_tool_cursor from the previous get_mcp read of this same server; omit for the first page"`
}

func NewGetMCPTool(reader platformmcp.Reader) *GetMCP {
	return &GetMCP{reader: reader}
}

func (t *GetMCP) Descriptor() core.ToolDescriptor {
	return core.ToolDescriptor{
		SourceSlug:  platformtools.SourcePlatform,
		HandlerName: "get_mcp",
		Name:        platformtools.ToolNameGetMCP,
		Description: "Get an allowlisted summary of one configured MCP in an explicit project. " + platformmcp.GetMCPToolPagingNote,
		InputSchema: core.BuildInputSchema[getMCPInput](),
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

	var decoded getMCPInput
	if err := core.DecodeInput(payload, &decoded); err != nil {
		return err
	}
	if decoded.ProjectID == "" || decoded.MCPID == "" {
		return fmt.Errorf("project_id and mcp_id are required")
	}
	input := platformmcp.GetMCPInput{ProjectSlug: "", ProjectID: decoded.ProjectID, MCPID: decoded.MCPID, ToolCursor: decoded.ToolCursor}

	principal, err := principalFromContext(ctx)
	if err != nil {
		return err
	}

	output, err := t.reader.GetMCP(ctx, principal, input)
	if err != nil {
		// A stale or conflicting tool_cursor is the caller's to correct by
		// re-reading, so it fails with a code and message the caller can act
		// on rather than a generic tool failure.
		if exposure, ok := errors.AsType[*platformmcp.MCPToolExposureError](err); ok {
			code := oops.CodeBadRequest
			if exposure.Code == "conflict" {
				code = oops.CodeConflict
			}
			return oops.E(code, err, "%s", exposure.Message)
		}
		return fmt.Errorf("get configured mcp: %w", err)
	}

	return core.EncodeResult(wr, output)
}
