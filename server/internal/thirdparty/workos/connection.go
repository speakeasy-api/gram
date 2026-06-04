package workos

import (
	"context"
	"fmt"
	"net/url"
)

// Connection represents a WorkOS SSO connection.
type Connection struct {
	ID             string `json:"id"`
	OrganizationID string `json:"organization_id"`
	ConnectionType string `json:"connection_type"`
	Name           string `json:"name"`
	State          string `json:"state"` // "active", "inactive", "draft"
	ExternalKey    string `json:"external_key"`
	CreatedAt      string `json:"created_at"`
	UpdatedAt      string `json:"updated_at"`
}

// ListConnectionsResponse is the paginated response from the WorkOS List Connections API.
type ListConnectionsResponse struct {
	Data []Connection `json:"data"`
}

// ListConnections fetches SSO connections for an organization from WorkOS.
// https://workos.com/docs/reference/sso/connection#list-connections
func (wc *Client) ListConnections(ctx context.Context, organizationID string) ([]Connection, error) {
	params := url.Values{}
	params.Set("organization_id", organizationID)

	var out ListConnectionsResponse
	if err := wc.do(ctx, "GET", "/connections?"+params.Encode(), nil, &out); err != nil {
		return nil, fmt.Errorf("list connections: %w", err)
	}

	return out.Data, nil
}

// Directory represents a WorkOS Directory Sync directory.
type Directory struct {
	ID             string `json:"id"`
	OrganizationID string `json:"organization_id"`
	Type           string `json:"type"`
	Name           string `json:"name"`
	State          string `json:"state"` // "linked", "unlinked", "deleting"
	ExternalKey    string `json:"external_key"`
	CreatedAt      string `json:"created_at"`
	UpdatedAt      string `json:"updated_at"`
}

// ListDirectoriesResponse is the paginated response from the WorkOS List Directories API.
type ListDirectoriesResponse struct {
	Data []Directory `json:"data"`
}

// ListDirectories fetches directory sync directories for an organization from WorkOS.
// https://workos.com/docs/reference/directory-sync/directory#list-directories
func (wc *Client) ListDirectories(ctx context.Context, organizationID string) ([]Directory, error) {
	params := url.Values{}
	params.Set("organization_id", organizationID)

	var out ListDirectoriesResponse
	if err := wc.do(ctx, "GET", "/directory_sync/directories?"+params.Encode(), nil, &out); err != nil {
		return nil, fmt.Errorf("list directories: %w", err)
	}

	return out.Data, nil
}

// HasActiveConnection returns true if the organization has at least one active SSO connection.
func HasActiveConnection(connections []Connection) bool {
	for _, c := range connections {
		if c.State == "active" {
			return true
		}
	}
	return false
}

// HasActiveDirectory returns true if the organization has at least one linked directory.
func HasActiveDirectory(directories []Directory) bool {
	for _, d := range directories {
		if d.State == "linked" {
			return true
		}
	}
	return false
}
