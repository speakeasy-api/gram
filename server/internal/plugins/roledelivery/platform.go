package roledelivery

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// ContainsPlatformTools reports whether a direct or wrapped toolset's latest
// nondeleted version contains a Platform tool. Automatic delivery excludes the
// entire toolset; explicit attachments and removal candidates remain unaffected.
func ContainsPlatformTools(ctx context.Context, tx pgx.Tx, org string, projectID uuid.UUID, toolsetID, mcpServerID uuid.NullUUID) (bool, error) {
	versions, err := pluginsrepo.New(tx).ListDeliveryToolsetToolURNs(ctx, pluginsrepo.ListDeliveryToolsetToolURNsParams{
		OrganizationID: org,
		ProjectID:      projectID,
		ToolsetID:      toolsetID,
		McpServerID:    mcpServerID,
	})
	if err != nil {
		return false, fmt.Errorf("load platform tool delivery eligibility: %w", err)
	}
	// SQLc decodes tool_urns through urn.Tool, including its validated parser.
	for _, tools := range versions {
		for _, tool := range tools {
			if tool.Kind == urn.ToolKindPlatform {
				return true, nil
			}
		}
	}
	return false, nil
}
