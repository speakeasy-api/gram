package deployments

import (
	"context"
	"fmt"
	"io"

	"github.com/speakeasy-api/gram/server/gen/deployments"
	"github.com/speakeasy-api/gram/server/internal/platformtools/core"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

type GetDeploymentLogs struct {
	deployments DeploymentsService
}

type getDeploymentLogsInput struct {
	DeploymentID string  `json:"deployment_id" jsonschema:"The deployment ID."`
	Cursor       *string `json:"cursor,omitempty" jsonschema:"Cursor for pagination."`
}

func NewGetDeploymentLogsTool(deploymentsSvc DeploymentsService) *GetDeploymentLogs {
	return &GetDeploymentLogs{deployments: deploymentsSvc}
}

func (s *GetDeploymentLogs) Descriptor() core.ToolDescriptor {
	return core.ToolDescriptor{
		SourceSlug:  "deployments",
		HandlerName: "get_deployment_logs",
		Name:        "platform_get_deployment_logs",
		Description: "Fetch event logs and overall status for a specific deployment in the current project.",
		InputSchema: core.BuildInputSchema[getDeploymentLogsInput](
			core.WithPropertyFormat("deployment_id", "uuid"),
		),
		Variables:   nil,
		Annotations: readOnlyToolAnnotations(),
		Managed:     true,
		OwnerKind:   nil,
		OwnerID:     nil,
	}
}

func (s *GetDeploymentLogs) Call(ctx context.Context, _ toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	if s.deployments == nil {
		return fmt.Errorf("deployments service not configured")
	}

	input := getDeploymentLogsInput{DeploymentID: "", Cursor: nil}
	if err := decodeToolInput(payload, &input); err != nil {
		return err
	}
	if input.DeploymentID == "" {
		return fmt.Errorf("deployment_id is required")
	}

	result, err := s.deployments.GetDeploymentLogs(ctx, &deployments.GetDeploymentLogsPayload{
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
