package workos

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/workos/workos-go/v6/pkg/directorysync"
	"github.com/workos/workos-go/v6/pkg/sso"
)

const connectionsWriteUnavailableMessage = "This endpoint is part of the Connections API migration capabilities, which are not enabled for your environment. Contact support@workos.com to enable them."

const connectionsCapabilityCacheTTL = time.Hour

// Connection represents a WorkOS SSO connection.
type Connection struct {
	ID             string
	OrganizationID string
	ConnectionType string
	Name           string
	State          string // "active", "inactive", "draft", "validating"

	// CallbackEndpoint is WorkOS's top-level callback_endpoint value.
	CallbackEndpoint string

	// OIDCDiscoveryEndpoint is the identity provider's OIDC discovery URL.
	OIDCDiscoveryEndpoint string

	// OIDCRedirectURI is WorkOS's oidc_options.redirect_uri value and is the
	// authoritative callback URL for OIDC provider configuration. Both
	// oidc_options.redirect_uri and callback_endpoint were confirmed in a live
	// GET /connections/{id} WorkOS response on 2026-09-15.
	OIDCRedirectURI string

	CreatedAt string
	UpdatedAt string
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
	ID               string                        `json:"id"`
	OrganizationID   string                        `json:"organization_id"`
	ConnectionType   string                        `json:"connection_type"`
	Name             string                        `json:"name"`
	State            string                        `json:"state"`
	CallbackEndpoint string                        `json:"callback_endpoint"`
	OIDCOptions      connectionOIDCOptionsResponse `json:"oidc_options"`
	CreatedAt        string                        `json:"created_at"`
	UpdatedAt        string                        `json:"updated_at"`
}

type connectionOIDCOptionsResponse struct {
	DiscoveryEndpoint string `json:"discovery_endpoint"`
	RedirectURI       string `json:"redirect_uri"`
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

type updateOIDCConnectionRequest struct {
	OIDCOptions updateOIDCOptions `json:"oidc_options"`
}

type updateOIDCOptions struct {
	DiscoveryEndpoint string `json:"discovery_endpoint"`
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
				"first_name": "given_name",
				"last_name":  "family_name",
				"groups":     "groups",
			},
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return emptyConnection(), fmt.Errorf("encode WorkOS OIDC connection: %w", err)
	}
	var response connectionResponse
	if err := wc.doWithoutRetry(ctx, http.MethodPost, "/connections", body, &response); err != nil {
		var apiErr *APIError
		if input.ClientSecret != "" && errors.As(err, &apiErr) {
			apiErr.Body = redactCredential(apiErr.Body, input.ClientSecret)
		}
		return emptyConnection(), err
	}
	return connectionFromResponse(response), nil
}

// UpdateOIDCConnectionDiscoveryEndpoint updates only the OIDC discovery URL
// for an existing WorkOS connection.
func (wc *Client) UpdateOIDCConnectionDiscoveryEndpoint(ctx context.Context, connectionID, discoveryEndpoint string) (Connection, error) {
	if err := validateOIDCConnectionDiscoveryEndpointUpdate(connectionID, discoveryEndpoint); err != nil {
		return emptyConnection(), err
	}

	body, err := json.Marshal(updateOIDCConnectionRequest{
		OIDCOptions: updateOIDCOptions{
			DiscoveryEndpoint: discoveryEndpoint,
		},
	})
	if err != nil {
		return emptyConnection(), fmt.Errorf("encode WorkOS OIDC connection update: %w", err)
	}

	var response connectionResponse
	path := "/connections/" + url.PathEscape(connectionID)
	if err := wc.doWithoutRetry(ctx, http.MethodPatch, path, body, &response); err != nil {
		return emptyConnection(), err
	}

	return connectionFromResponse(response), nil
}

func validateOIDCConnectionDiscoveryEndpointUpdate(connectionID, discoveryEndpoint string) error {
	if strings.TrimSpace(connectionID) == "" {
		return errors.New("update WorkOS OIDC connection: connection ID is required")
	}
	if strings.TrimSpace(discoveryEndpoint) == "" {
		return errors.New("update WorkOS OIDC connection: discovery endpoint is required")
	}
	return nil
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
		return emptyConnection(), errors.New("get WorkOS connection: connection ID is required")
	}
	var response connectionResponse
	path := "/connections/" + url.PathEscape(connectionID)
	if err := wc.do(ctx, http.MethodGet, path, nil, &response); err != nil {
		return emptyConnection(), err
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

// ConnectionsAPIAvailable probes whether this WorkOS environment permits
// connection creation. Successful capability determinations are cached because
// the capability is environment-wide rather than organization-specific.
func (wc *Client) ConnectionsAPIAvailable(ctx context.Context) (bool, error) {
	wc.connectionsCapability.Lock()
	defer wc.connectionsCapability.Unlock()

	now := time.Now()
	if wc.connectionsCapability.set && now.Before(wc.connectionsCapability.expiresAt) {
		return wc.connectionsCapability.available, nil
	}

	err := wc.doWithoutRetry(ctx, http.MethodPost, "/connections", []byte(`{}`), nil)
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		if err == nil {
			return false, errors.New("probe WorkOS Connections API: empty request unexpectedly succeeded")
		}
		return false, fmt.Errorf("probe WorkOS Connections API: %w", err)
	}

	available := false
	var response struct {
		Code string `json:"code"`
	}
	switch {
	case apiErr.StatusCode == http.StatusBadRequest && json.Unmarshal([]byte(apiErr.Body), &response) == nil && response.Code == "connection_type_or_options_required":
		available = true
	case IsConnectionsWriteUnavailable(apiErr):
		available = false
	default:
		return false, fmt.Errorf("probe WorkOS Connections API: %w", apiErr)
	}

	wc.connectionsCapability.available = available
	wc.connectionsCapability.expiresAt = now.Add(connectionsCapabilityCacheTTL)
	wc.connectionsCapability.set = true
	return available, nil
}

func connectionFromResponse(response connectionResponse) Connection {
	return Connection{
		ID:                    response.ID,
		OrganizationID:        response.OrganizationID,
		ConnectionType:        response.ConnectionType,
		Name:                  response.Name,
		State:                 response.State,
		CallbackEndpoint:      response.CallbackEndpoint,
		OIDCDiscoveryEndpoint: response.OIDCOptions.DiscoveryEndpoint,
		OIDCRedirectURI:       response.OIDCOptions.RedirectURI,
		CreatedAt:             response.CreatedAt,
		UpdatedAt:             response.UpdatedAt,
	}
}

func emptyConnection() Connection {
	return Connection{
		ID:                    "",
		OrganizationID:        "",
		ConnectionType:        "",
		Name:                  "",
		State:                 "",
		CallbackEndpoint:      "",
		OIDCDiscoveryEndpoint: "",
		OIDCRedirectURI:       "",
		CreatedAt:             "",
		UpdatedAt:             "",
	}
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
				ID:                    c.ID,
				OrganizationID:        c.OrganizationID,
				ConnectionType:        string(c.ConnectionType),
				Name:                  c.Name,
				State:                 string(c.State),
				CallbackEndpoint:      "",
				OIDCDiscoveryEndpoint: "",
				OIDCRedirectURI:       "",
				CreatedAt:             c.CreatedAt,
				UpdatedAt:             c.UpdatedAt,
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

// DirectoryGroup represents a WorkOS Directory Sync group.
type DirectoryGroup struct {
	ID             string
	DirectoryID    string
	OrganizationID string
	Name           string
	CreatedAt      string
	UpdatedAt      string
}

// ListDirectoryGroups fetches all provisioned groups for a directory from WorkOS.
// https://workos.com/docs/reference/directory-sync/directory-group/list
func (wc *Client) ListDirectoryGroups(ctx context.Context, directoryID string) ([]DirectoryGroup, error) {
	var all []DirectoryGroup
	after := ""

	for {
		resp, err := wc.dsync.ListGroups(ctx, directorysync.ListGroupsOpts{
			Directory: directoryID,
			User:      "",
			Limit:     100,
			Order:     "",
			Before:    "",
			After:     after,
		})
		if err != nil {
			return nil, wrapSDKError(err, "list directory groups")
		}

		for _, group := range resp.Data {
			all = append(all, DirectoryGroup{
				ID:             group.ID,
				DirectoryID:    group.DirectoryID,
				OrganizationID: group.OrganizationID,
				Name:           group.Name,
				CreatedAt:      group.CreatedAt,
				UpdatedAt:      group.UpdatedAt,
			})
		}

		if resp.ListMetadata.After == "" {
			break
		}
		after = resp.ListMetadata.After
	}

	return all, nil
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
