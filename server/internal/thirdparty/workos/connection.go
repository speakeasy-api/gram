package workos

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/workos/workos-go/v6/pkg/directorysync"
	"github.com/workos/workos-go/v6/pkg/sso"
)

const connectionsWriteUnavailableMessage = "This endpoint is part of the Connections API migration capabilities, which are not enabled for your environment. Contact support@workos.com to enable them."

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

// CreateOIDCConnectionInput contains the transient values needed to configure a WorkOS OIDC connection.
type CreateOIDCConnectionInput struct {
	// OrganizationID is the WorkOS organization that owns the connection.
	OrganizationID string

	// Name is the connection's human-readable name.
	Name string

	// DiscoveryEndpoint is the identity provider's OIDC discovery URL.
	DiscoveryEndpoint string

	// ClientID is the identity provider's public OAuth client identifier.
	ClientID string

	// ClientSecret is written through to WorkOS and must not be retained after the request.
	ClientSecret string
}

type connectionResponse struct {
	ID             string `json:"id"`
	OrganizationID string `json:"organization_id"`
	ConnectionType string `json:"connection_type"`
	Name           string `json:"name"`
	State          string `json:"state"`
	CreatedAt      string `json:"created_at"`
	UpdatedAt      string `json:"updated_at"`
}

type createOIDCConnectionRequest struct {
	OrganizationID string                  `json:"organization_id"`
	Name           string                  `json:"name"`
	ConnectionType string                  `json:"connection_type"`
	OIDCOptions    createOIDCOptions       `json:"oidc_options"`
	AttributeMaps  createOIDCAttributeMaps `json:"attribute_maps"`
}

type createOIDCOptions struct {
	DiscoveryEndpoint         string `json:"discovery_endpoint"`
	ClientID                  string `json:"client_id"`
	ClientSecret              string `json:"client_secret"`
	TokenAuthenticationMethod string `json:"token_authentication_method"`
	PKCE                      bool   `json:"pkce"`
}

type createOIDCAttributeMaps struct {
	StandardAttributes map[string]string `json:"standard_attributes"`
}

// CreateOIDCConnection creates a GenericOIDC connection using the field names
// confirmed against WorkOS POST /connections validation on 2026-09-15.
func (wc *Client) CreateOIDCConnection(ctx context.Context, input CreateOIDCConnectionInput) (Connection, error) {
	payload := createOIDCConnectionRequest{
		OrganizationID: input.OrganizationID,
		Name:           input.Name,
		ConnectionType: "GenericOIDC",
		OIDCOptions: createOIDCOptions{
			DiscoveryEndpoint:         input.DiscoveryEndpoint,
			ClientID:                  input.ClientID,
			ClientSecret:              input.ClientSecret,
			TokenAuthenticationMethod: "client_secret_post",
			PKCE:                      true,
		},
		AttributeMaps: createOIDCAttributeMaps{
			StandardAttributes: map[string]string{
				"email":      "email",
				"first_name": "first_name",
				"last_name":  "last_name",
				"groups":     "groups",
			},
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return Connection{}, fmt.Errorf("encode WorkOS OIDC connection: %w", err)
	}
	var response connectionResponse
	if err := wc.doWithoutRetry(ctx, http.MethodPost, "/connections", body, &response); err != nil {
		var apiErr *APIError
		if input.ClientSecret != "" && errors.As(err, &apiErr) {
			apiErr.Body = redactCredential(apiErr.Body, input.ClientSecret)
		}
		return Connection{}, err
	}
	return connectionFromResponse(response), nil
}

func redactCredential(value, credential string) string {
	if credential == "" {
		return value
	}
	value = strings.ReplaceAll(value, credential, "[redacted]")
	encoded, err := json.Marshal(credential)
	if err == nil && len(encoded) >= 2 {
		value = strings.ReplaceAll(value, string(encoded[1:len(encoded)-1]), "[redacted]")
	}
	return value
}

// GetConnection retrieves a WorkOS connection by its stable connection ID.
func (wc *Client) GetConnection(ctx context.Context, connectionID string) (Connection, error) {
	if strings.TrimSpace(connectionID) == "" {
		return Connection{}, errors.New("get WorkOS connection: connection ID is required")
	}
	var response connectionResponse
	path := "/connections/" + url.PathEscape(connectionID)
	if err := wc.do(ctx, http.MethodGet, path, nil, &response); err != nil {
		return Connection{}, err
	}
	return connectionFromResponse(response), nil
}

// IsConnectionsWriteUnavailable reports the exact capability-gated 404 observed
// from WorkOS, which permits callers to fall back to Admin Portal setup.
func IsConnectionsWriteUnavailable(err error) bool {
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusNotFound {
		return false
	}
	var response struct {
		Message string `json:"message"`
	}
	return json.Unmarshal([]byte(apiErr.Body), &response) == nil && response.Message == connectionsWriteUnavailableMessage
}

func connectionFromResponse(response connectionResponse) Connection {
	return Connection(response)
}

// ListConnections fetches SSO connections for an organization from WorkOS.
// https://workos.com/docs/reference/sso/connection#list-connections
func (wc *Client) ListConnections(ctx context.Context, organizationID string) ([]Connection, error) {
	out := make([]Connection, 0)
	seen := make(map[string]struct{})
	after := ""
	for {
		resp, err := wc.sso.ListConnections(ctx, sso.ListConnectionsOpts{
			OrganizationID: organizationID,
			ConnectionType: "",
			Domain:         "",
			Limit:          100,
			Order:          "",
			Before:         "",
			After:          after,
		})
		if err != nil {
			return nil, wrapSDKError(err, "list connections")
		}
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
		if resp.ListMetadata.After == "" {
			break
		}
		if _, exists := seen[resp.ListMetadata.After]; exists {
			return nil, fmt.Errorf("list connections: repeated pagination cursor %q", resp.ListMetadata.After)
		}
		seen[resp.ListMetadata.After] = struct{}{}
		after = resp.ListMetadata.After
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
