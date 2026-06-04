package deployments

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/speakeasy-api/gram/server/gen/deployments"
	"github.com/speakeasy-api/gram/server/gen/types"
)

// DeploymentsService is the subset of the deployments management service that
// the managed assistant's deployment tools call. The concrete deployments
// service satisfies it; tools pass nil auth tokens because the assistant
// runtime supplies auth context out of band.
type DeploymentsService interface {
	GetDeploymentLogs(ctx context.Context, payload *deployments.GetDeploymentLogsPayload) (*deployments.GetDeploymentLogsResult, error)
}

func readOnlyToolAnnotations() *types.ToolAnnotations {
	readOnly := true
	destructive := false
	idempotent := true
	openWorld := false
	return &types.ToolAnnotations{
		Title:           nil,
		ReadOnlyHint:    &readOnly,
		DestructiveHint: &destructive,
		IdempotentHint:  &idempotent,
		OpenWorldHint:   &openWorld,
	}
}

func decodeToolInput(payload io.Reader, dst any) error {
	body, err := io.ReadAll(payload)
	if err != nil {
		return fmt.Errorf("read request body: %w", err)
	}
	if len(body) == 0 {
		return nil
	}
	if err := json.Unmarshal(body, dst); err != nil {
		return fmt.Errorf("decode request body: %w", err)
	}
	return nil
}

func encodeToolResult(wr io.Writer, result any) error {
	if err := json.NewEncoder(wr).Encode(result); err != nil {
		return fmt.Errorf("encode response: %w", err)
	}
	return nil
}
