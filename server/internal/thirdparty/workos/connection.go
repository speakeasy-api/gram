package workos

import (
	"context"
	"encoding/json"

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

// DirectoryUser represents a WorkOS Directory Sync user.
type DirectoryUser struct {
	ID               string
	DirectoryID      string
	OrganizationID   string
	Email            string
	State            string // "active", "inactive"
	CustomAttributes json.RawMessage
	CreatedAt        string
	UpdatedAt        string
}

// ListDirectoryUsers fetches all provisioned users for a directory from WorkOS.
// https://workos.com/docs/reference/directory-sync/directory-user/list
func (wc *Client) ListDirectoryUsers(ctx context.Context, directoryID string) ([]DirectoryUser, error) {
	var all []DirectoryUser
	after := ""

	for {
		resp, err := wc.dsync.ListUsers(ctx, directorysync.ListUsersOpts{
			Directory: directoryID,
			Group:     "",
			Limit:     100,
			Order:     "",
			Before:    "",
			After:     after,
		})
		if err != nil {
			return nil, wrapSDKError(err, "list directory users")
		}

		for _, u := range resp.Data {
			all = append(all, DirectoryUser{
				ID:               u.ID,
				DirectoryID:      u.DirectoryID,
				OrganizationID:   u.OrganizationID,
				Email:            u.Email,
				State:            string(u.State),
				CustomAttributes: u.CustomAttributes,
				CreatedAt:        u.CreatedAt,
				UpdatedAt:        u.UpdatedAt,
			})
		}

		if resp.ListMetadata.After == "" {
			break
		}
		after = resp.ListMetadata.After
	}

	return all, nil
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
