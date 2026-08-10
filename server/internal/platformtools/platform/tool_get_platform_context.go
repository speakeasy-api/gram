package platform

import (
	"context"
	"io"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/platformtools"
	"github.com/speakeasy-api/gram/server/internal/platformtools/core"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

type GetPlatformContext struct{}

type getPlatformContextInput struct{}

// getPlatformContextResult is the assistant-channel variant of
// platformmcp.PlatformContext: there is no OAuth connection to report, and the
// calling assistant benefits from knowing its own project alongside the
// organization.
type getPlatformContextResult struct {
	OrganizationID string `json:"organization_id"`
	ProjectID      string `json:"project_id,omitempty"`
	ReadOnly       bool   `json:"read_only"`
}

func NewGetPlatformContextTool() *GetPlatformContext {
	return &GetPlatformContext{}
}

func (t *GetPlatformContext) Descriptor() core.ToolDescriptor {
	return core.ToolDescriptor{
		SourceSlug:  platformtools.SourcePlatform,
		HandlerName: "get_platform_context",
		Name:        platformtools.ToolNameGetPlatformContext,
		Description: "Show the organization and project bound to this session. All platform tools on this server are read-only.",
		InputSchema: core.BuildInputSchema[getPlatformContextInput](),
		Variables:   nil,
		Annotations: core.ReadOnlyAnnotations(),
		Managed:     true,
		OwnerKind:   nil,
		OwnerID:     nil,
	}
}

func (t *GetPlatformContext) Call(ctx context.Context, _ toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	input := getPlatformContextInput{}
	if err := core.DecodeInput(payload, &input); err != nil {
		return err
	}

	principal, err := principalFromContext(ctx)
	if err != nil {
		return err
	}

	projectID := ""
	if authCtx, ok := contextvalues.GetAuthContext(ctx); ok && authCtx != nil && authCtx.ProjectID != nil {
		projectID = authCtx.ProjectID.String()
	}

	return core.EncodeResult(wr, getPlatformContextResult{
		OrganizationID: principal.OrganizationID,
		ProjectID:      projectID,
		ReadOnly:       true,
	})
}
