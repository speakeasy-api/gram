package insights

import (
	"context"
	"fmt"
	"io"

	"github.com/speakeasy-api/gram/server/gen/deployments"
	"github.com/speakeasy-api/gram/server/internal/platformtools/core"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

type GetDeploymentLogs struct {
	provider func() DeploymentsService
}

type getDeploymentLogsInput struct {
	DeploymentID string  `json:"deployment_id" jsonschema:"The ID of the deployment to fetch logs for."`
	Cursor       *string `json:"cursor,omitempty" jsonschema:"Cursor for pagination."`
}

func NewGetDeploymentLogsTool(provider func() DeploymentsService) *GetDeploymentLogs {
	return &GetDeploymentLogs{provider: provider}
}

func (s *GetDeploymentLogs) Descriptor() core.ToolDescriptor {
	return core.ToolDescriptor{
		SourceSlug:  "insights",
		HandlerName: "get_deployment_logs",
		Name:        "platform_get_deployment_logs",
		Description: "Fetch build/processing logs for a deployment in the current project. Requires a deployment ID.",
		InputSchema: core.BuildInputSchema[getDeploymentLogsInput](),
		Variables:   nil,
		Annotations: readOnlyToolAnnotations(),
		Managed:     true,
		OwnerKind:   nil,
		OwnerID:     nil,
	}
}

func (s *GetDeploymentLogs) Call(ctx context.Context, _ toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	svc := s.provider()
	if svc == nil {
		return fmt.Errorf("deployments service not configured")
	}

	input := getDeploymentLogsInput{DeploymentID: "", Cursor: nil}
	if err := decodeToolInput(payload, &input); err != nil {
		return err
	}
	if input.DeploymentID == "" {
		return fmt.Errorf("deployment_id is required")
	}

	result, err := svc.GetDeploymentLogs(ctx, &deployments.GetDeploymentLogsPayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		DeploymentID:     input.DeploymentID,
		Cursor:           input.Cursor,
	})
	if err != nil {
		return fmt.Errorf("get deployment logs: %w", err)
	}

	return encodeToolResult(wr, result)
}
