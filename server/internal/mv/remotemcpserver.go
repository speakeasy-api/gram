package mv

import (
	"time"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
)

// BuildRemoteMcpServerView converts a repo server row and its headers into the
// API response type. Headers should already be decrypted/redacted before being
// passed to this function.
func BuildRemoteMcpServerView(server repo.RemoteMcpServer, headers []repo.RemoteMcpServerHeader) *types.RemoteMcpServer {
	return &types.RemoteMcpServer{
		ID:            server.ID.String(),
		ProjectID:     server.ProjectID.String(),
		URL:           server.Url,
		TransportType: server.TransportType,
		Headers:       buildRemoteMcpServerHeaderViews(headers),
		CreatedAt:     server.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:     server.UpdatedAt.Time.Format(time.RFC3339),
	}
}

func buildRemoteMcpServerHeaderViews(headers []repo.RemoteMcpServerHeader) []*types.RemoteMcpServerHeader {
	result := make([]*types.RemoteMcpServerHeader, len(headers))
	for i, header := range headers {
		result[i] = &types.RemoteMcpServerHeader{
			ID:                     header.ID.String(),
			Name:                   header.Name,
			Description:            conv.FromPGText[string](header.Description),
			IsRequired:             header.IsRequired,
			IsSecret:               header.IsSecret,
			Value:                  conv.FromPGText[string](header.Value),
			ValueFromRequestHeader: conv.FromPGText[string](header.ValueFromRequestHeader),
			CreatedAt:              header.CreatedAt.Time.Format(time.RFC3339),
			UpdatedAt:              header.UpdatedAt.Time.Format(time.RFC3339),
		}
	}

	return result
}
