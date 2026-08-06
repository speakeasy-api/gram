package mv

import (
	"time"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/unproxiedmcp/repo"
)

// BuildUnproxiedMcpServerView converts a repo server row into the API
// response type.
func BuildUnproxiedMcpServerView(server repo.UnproxiedMcpServer) *types.UnproxiedMcpServer {
	return &types.UnproxiedMcpServer{
		ID:          server.ID.String(),
		ProjectID:   server.ProjectID.String(),
		Name:        conv.FromPGText[string](server.Name),
		Slug:        conv.FromPGText[string](server.Slug),
		URL:         server.Url,
		Description: conv.FromPGText[string](server.Description),
		CreatedAt:   server.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:   server.UpdatedAt.Time.Format(time.RFC3339),
	}
}
