package platform

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/platformmcp"
	"github.com/speakeasy-api/gram/server/internal/platformtools"
	"github.com/speakeasy-api/gram/server/internal/platformtools/core"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

type GetMCP struct {
	reader platformmcp.Reader
}

// getMCPInput is this executor's own contract. It takes no project: the
// executor is served only to a project's managed assistant, so the project is
// always the assistant's own, as find_mcp does.
type getMCPInput struct {
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
		Description: "Get an allowlisted summary of one configured MCP in the assistant's project. " + platformmcp.GetMCPToolPagingNote,
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
	if decoded.MCPID == "" {
		return fmt.Errorf("mcp_id is required")
	}
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return fmt.Errorf("platform get MCP requires project auth context")
	}
	// Caller scope wins: the model cannot name another project.
	input := platformmcp.GetMCPInput{ProjectSlug: "", ProjectID: authCtx.ProjectID.String(), MCPID: decoded.MCPID, ToolCursor: decoded.ToolCursor}

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
