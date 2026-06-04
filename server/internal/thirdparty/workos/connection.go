package workos

import (
	"context"

	"github.com/workos/workos-go/v6/pkg/directorysync"
	"github.com/workos/workos-go/v6/pkg/sso"
)

// Connection represents a WorkOS SSO connection.
type Connection struct {
	ID             string
	OrganizationID string
	ConnectionType string
	Name           string
	State          string // "active", "inactive", "draft", "validating"
	CreatedAt      string
	UpdatedAt      string
}

// ListConnections fetches SSO connections for an organization from WorkOS.
// https://workos.com/docs/reference/sso/connection#list-connections
func (wc *Client) ListConnections(ctx context.Context, organizationID string) ([]Connection, error) {
	resp, err := wc.sso.ListConnections(ctx, sso.ListConnectionsOpts{
		OrganizationID: organizationID,
		ConnectionType: "",
		Domain:         "",
		Limit:          0,
		Order:          "",
		Before:         "",
		After:          "",
	})
	if err != nil {
		return nil, wrapSDKError(err, "list connections")
	}

	out := make([]Connection, 0, len(resp.Data))
	for _, c := range resp.Data {
		out = append(out, Connection{
			ID:             c.ID,
			OrganizationID: c.OrganizationID,
			ConnectionType: string(c.ConnectionType),
			Name:           c.Name,
			State:          string(c.State),
			CreatedAt:      c.CreatedAt,
			UpdatedAt:      c.UpdatedAt,
		})
	}
	return out, nil
}

// Directory represents a WorkOS Directory Sync directory.
type Directory struct {
	ID             string
	OrganizationID string
	Type           string
	Name           string
	State          string // "linked", "unlinked", "invalid_credentials"
	CreatedAt      string
	UpdatedAt      string
}

// ListDirectories fetches directory sync directories for an organization from WorkOS.
// https://workos.com/docs/reference/directory-sync/directory#list-directories
func (wc *Client) ListDirectories(ctx context.Context, organizationID string) ([]Directory, error) {
	resp, err := wc.dsync.ListDirectories(ctx, directorysync.ListDirectoriesOpts{
		OrganizationID: organizationID,
		Search:         "",
		Limit:          0,
		Order:          "",
		Before:         "",
		After:          "",
	})
	if err != nil {
		return nil, wrapSDKError(err, "list directories")
	}

	out := make([]Directory, 0, len(resp.Data))
	for _, d := range resp.Data {
		out = append(out, Directory{
			ID:             d.ID,
			OrganizationID: d.OrganizationID,
			Type:           string(d.Type),
			Name:           d.Name,
			State:          string(d.State),
			CreatedAt:      d.CreatedAt,
			UpdatedAt:      d.UpdatedAt,
		})
	}
	return out, nil
}

// HasActiveConnection returns true if the organization has at least one active SSO connection.
func HasActiveConnection(connections []Connection) bool {
	for _, c := range connections {
		if c.State == string(sso.Active) {
			return true
		}
	}
	return false
}

// HasActiveDirectory returns true if the organization has at least one linked directory.
func HasActiveDirectory(directories []Directory) bool {
	for _, d := range directories {
		if d.State == string(directorysync.Linked) {
			return true
		}
	}
	return false
}
