package authclients

import (
	"time"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/authclients/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/visibility"
)

// buildAuthClientView never carries stored header values: a header reports
// only whether it has one.
func buildAuthClientView(client repo.McpAuthClient, headers []repo.ListAuthClientHeadersRow, servers []repo.ListAuthClientServersRow) *types.AuthClient {
	headerViews := make([]*types.AuthClientHeader, 0, len(headers))
	for _, h := range headers {
		headerViews = append(headerViews, &types.AuthClientHeader{
			Name:                   h.Name,
			Description:            conv.FromPGText[string](h.Description),
			IsRequired:             h.IsRequired,
			IsSecret:               h.IsSecret,
			ValueFromRequestHeader: conv.FromPGText[string](h.ValueFromRequestHeader),
			HasStoredValue:         h.HasStoredValue,
		})
	}

	serverViews := make([]*types.AuthClientServer, 0, len(servers))
	for _, srv := range servers {
		serverViews = append(serverViews, &types.AuthClientServer{
			ID:        srv.ID.String(),
			ProjectID: srv.ProjectID.String(),
			Name:      conv.FromPGText[string](srv.Name),
			Slug:      conv.FromPGText[string](srv.Slug),
			Public:    srv.Visibility == visibility.Public,
		})
	}

	var projectID *string
	if client.ProjectID.Valid {
		projectID = new(client.ProjectID.UUID.String())
	}

	var registrationID *string
	if client.RemoteSessionClientID.Valid {
		registrationID = new(client.RemoteSessionClientID.UUID.String())
	}

	return &types.AuthClient{GithubApp: nil,
		ID:                    client.ID.String(),
		ProjectID:             projectID,
		ClientType:            client.ClientType,
		Source:                types.AuthClientSource(client.Source),
		Name:                  client.Name,
		Enabled:               client.Enabled,
		RemoteSessionClientID: registrationID,
		Personal:              client.ClientType == authz.AuthClientTypeRemoteSession,
		Headers:               headerViews,
		Servers:               serverViews,
		CreatedAt:             client.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:             client.UpdatedAt.Time.Format(time.RFC3339),
	}
}
