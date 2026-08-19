package platform

import (
	"context"
	"fmt"
	"io"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/platformmcp"
	"github.com/speakeasy-api/gram/server/internal/platformtools"
	"github.com/speakeasy-api/gram/server/internal/platformtools/core"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

type FindMCP struct {
	reader platformmcp.Reader
}

func NewFindMCPTool(reader platformmcp.Reader) *FindMCP {
	return &FindMCP{reader: reader}
}

func (t *FindMCP) Descriptor() core.ToolDescriptor {
	return core.ToolDescriptor{
		SourceSlug:  platformtools.SourcePlatform,
		HandlerName: "find_mcp",
		Name:        platformtools.ToolNameFindMCP,
		Description: "Find configured MCPs in the assistant's project. Results contain persisted allowlisted inventory facts only.",
		InputSchema: core.BuildInputSchema[platformmcp.FindMCPInput](),
		Variables:   nil,
		Annotations: core.ReadOnlyAnnotations(),
		Managed:     true,
		OwnerKind:   nil,
		OwnerID:     nil,
	}
}

func (t *FindMCP) Call(ctx context.Context, _ toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	if t.reader == nil {
		return fmt.Errorf("platform reader not configured")
	}
	input := platformmcp.FindMCPInput{
		ProjectID:   "",
		ProjectSlug: "",
		Query:       "",
		Cursor:      "",
	}
	if err := core.DecodeInput(payload, &input); err != nil {
		return err
	}
	principal, err := principalFromContext(ctx)
	if err != nil {
		return err
	}
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return fmt.Errorf("platform find MCP requires project auth context")
	}
	// This compatibility executor is served only to a project's managed
	// assistant. It mirrors the descriptor adapter's target policy: caller
	// scope wins over any project a model supplied.
	input.ProjectID = authCtx.ProjectID.String()
	input.ProjectSlug = ""
	output, err := t.reader.FindMCP(ctx, principal, input)
	if err != nil {
		return fmt.Errorf("find configured MCPs: %w", err)
	}
	return core.EncodeResult(wr, output)
}
