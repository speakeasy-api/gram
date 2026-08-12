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

type ListProjectMCPs struct {
	reader platformmcp.Reader
}

func NewListProjectMCPsTool(reader platformmcp.Reader) *ListProjectMCPs {
	return &ListProjectMCPs{reader: reader}
}

func (t *ListProjectMCPs) Descriptor() core.ToolDescriptor {
	return core.ToolDescriptor{
		SourceSlug:  platformtools.SourcePlatform,
		HandlerName: "list_project_mcps",
		Name:        platformtools.ToolNameListProjectMCPs,
		Description: "List the configured MCPs in an explicit project. Results contain allowlisted MCP summaries only.",
		InputSchema: core.BuildInputSchema[platformmcp.ListProjectMCPsInput](),
		Variables:   nil,
		Annotations: core.ReadOnlyAnnotations(),
		Managed:     true,
		OwnerKind:   nil,
		OwnerID:     nil,
	}
}

func (t *ListProjectMCPs) Call(ctx context.Context, _ toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	if t.reader == nil {
		return fmt.Errorf("platform reader not configured")
	}

	input := platformmcp.ListProjectMCPsInput{ProjectID: "", Limit: 0}
	if err := core.DecodeInput(payload, &input); err != nil {
		return err
	}
	if input.ProjectID == "" {
		return fmt.Errorf("project_id is required")
	}

	principal, err := principalFromContext(ctx)
	if err != nil {
		return err
	}

	output, err := t.reader.ListProjectMCPs(ctx, principal, input)
	if err != nil {
		return fmt.Errorf("list project mcps: %w", err)
	}

	return core.EncodeResult(wr, output)
}
