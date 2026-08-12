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

type ListProjects struct {
	reader platformmcp.Reader
}

func NewListProjectsTool(reader platformmcp.Reader) *ListProjects {
	return &ListProjects{reader: reader}
}

func (t *ListProjects) Descriptor() core.ToolDescriptor {
	return core.ToolDescriptor{
		SourceSlug:  platformtools.SourcePlatform,
		HandlerName: "list_projects",
		Name:        platformtools.ToolNameListProjects,
		Description: "List projects in the current organization. Results contain only project identifiers, names, and slugs.",
		InputSchema: core.BuildInputSchema[platformmcp.ListProjectsInput](),
		Variables:   nil,
		Annotations: core.ReadOnlyAnnotations(),
		Managed:     true,
		OwnerKind:   nil,
		OwnerID:     nil,
	}
}

func (t *ListProjects) Call(ctx context.Context, _ toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	if t.reader == nil {
		return fmt.Errorf("platform reader not configured")
	}

	input := platformmcp.ListProjectsInput{Limit: 0}
	if err := core.DecodeInput(payload, &input); err != nil {
		return err
	}

	principal, err := principalFromContext(ctx)
	if err != nil {
		return err
	}

	output, err := t.reader.ListProjects(ctx, principal, input)
	if err != nil {
		return fmt.Errorf("list projects: %w", err)
	}

	return core.EncodeResult(wr, output)
}
