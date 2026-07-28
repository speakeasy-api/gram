package mv

import (
	"time"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
)

// BuildRemoteMcpServerView converts a repo server row into the API response
// type. Headers are managed through their own endpoints and are not embedded
// here.
func BuildRemoteMcpServerView(server repo.RemoteMcpServer) *types.RemoteMcpServer {
	return &types.RemoteMcpServer{
		ID:            server.ID.String(),
		ProjectID:     server.ProjectID.String(),
		Name:          conv.FromPGText[string](server.Name),
		Slug:          conv.FromPGText[string](server.Slug),
		URL:           server.Url,
		TransportType: server.TransportType,
		CreatedAt:     server.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:     server.UpdatedAt.Time.Format(time.RFC3339),
	}
}

// BuildRemoteMcpServerHeaderView converts a repo header row into the API
// response type. The row's value should already be decrypted and redacted
// before being passed to this function.
func BuildRemoteMcpServerHeaderView(header repo.RemoteMcpServerHeader) *types.RemoteMcpServerHeader {
	return &types.RemoteMcpServerHeader{
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

// BuildRemoteMcpServerHeaderListView converts repo header rows into the API
// response type. The rows' values should already be decrypted and redacted
// before being passed to this function.
func BuildRemoteMcpServerHeaderListView(headers []repo.RemoteMcpServerHeader) []*types.RemoteMcpServerHeader {
	result := make([]*types.RemoteMcpServerHeader, len(headers))
	for i, header := range headers {
		result[i] = BuildRemoteMcpServerHeaderView(header)
	}

	return result
}
