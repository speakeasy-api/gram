package workos

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/hashicorp/go-retryablehttp"
	"github.com/workos/workos-go/v6/pkg/organizations"
	"github.com/workos/workos-go/v6/pkg/usermanagement"

	"github.com/speakeasy-api/gram/server/internal/o11y"
)

const workosBaseURL = "https://api.workos.com"

// Role represents an organization role as returned by WorkOS.
type Role struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Slug        string `json:"slug"`
	Description string `json:"description"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

// Member represents an active organization membership.
// RoleSlug is the slug of the member's assigned role.
type Member struct {
	ID             string
	UserID         string
	OrganizationID string
	RoleSlug       string
	CreatedAt      string
}

// User represents a WorkOS user with the fields used by Gram.
type User struct {
	ID        string
	FirstName string
	LastName  string
	Email     string
}

// --- Errors ---

// APIError is returned by do when the WorkOS API responds with a 4xx or 5xx status.
// Callers can use errors.As to inspect the StatusCode for specific handling (e.g. 409 conflict).
type APIError struct {
	Method     string
	Path       string
	StatusCode int
	Body       string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("workos api %s %s: status %d: %s", e.Method, e.Path, e.StatusCode, e.Body)
}

// RoleClient wraps WorkOS API calls for role and membership management.
// It is designed to have a caching layer added later.
type RoleClient struct {
	apiKey     string
	endpoint   string // base URL for raw HTTP calls; defaults to workosBaseURL
	httpClient *http.Client
	orgs       *organizations.Client
	um         *usermanagement.Client
}

// RoleClientOpts configures optional overrides for NewRoleClient.
// Zero values use production defaults. Primarily used in tests.
type RoleClientOpts struct {
	// Endpoint overrides the WorkOS base URL for both raw HTTP and SDK calls.
	Endpoint string
	// HTTPClient overrides the default retryable HTTP client.
	HTTPClient *http.Client
}

func NewRoleClient(apiKey string, opts ...RoleClientOpts) *RoleClient {
	if apiKey == "" || apiKey == "unset" {
		return nil
	}

	var opt RoleClientOpts
	if len(opts) > 0 {
		opt = opts[0]
	}

	endpoint := workosBaseURL
	if opt.Endpoint != "" {
		endpoint = opt.Endpoint
	}

	httpClient := opt.HTTPClient
	if httpClient == nil {
		rc := retryablehttp.NewClient()
		rc.HTTPClient.Timeout = 30 * time.Second
		httpClient = rc.StandardClient()
	}

	um := usermanagement.NewClient(apiKey)
	um.HTTPClient = httpClient
	if opt.Endpoint != "" {
		um.Endpoint = opt.Endpoint
	}

	return &RoleClient{
		apiKey:     apiKey,
		endpoint:   endpoint,
		httpClient: httpClient,
		orgs:       &organizations.Client{APIKey: apiKey, HTTPClient: httpClient, Endpoint: opt.Endpoint, JSONEncode: nil},
		um:         um,
	}
}

func (rc *RoleClient) ListRoles(ctx context.Context, orgID string) ([]Role, error) {
	if rc == nil {
		return nil, errors.New("role client is not initialized")
	}

	resp, err := rc.orgs.ListOrganizationRoles(ctx, organizations.ListOrganizationRolesOpts{
		OrganizationID: orgID,
	})
	if err != nil {
		return nil, fmt.Errorf("list organization roles: %w", err)
	}

	out := make([]Role, len(resp.Data))
	for i, r := range resp.Data {
		out[i] = Role{
			ID:          r.ID,
			Name:        r.Name,
			Slug:        r.Slug,
			Description: r.Description,
			CreatedAt:   r.CreatedAt,
			UpdatedAt:   r.UpdatedAt,
		}
	}
	return out, nil
}

type CreateRoleOpts struct {
	Name        string `json:"name"`
	Slug        string `json:"slug"`
	Description string `json:"description"`
}

// CreateRole creates a custom role for an organization via the WorkOS REST API.
// The Go SDK does not expose role CRUD, so we use raw HTTP against the /authorization/ endpoints.
func (rc *RoleClient) CreateRole(ctx context.Context, orgID string, opts CreateRoleOpts) (*Role, error) {
	if rc == nil {
		return nil, errors.New("role client is not initialized")
	}

	path, err := url.JoinPath("/authorization/organizations", orgID, "roles")
	if err != nil {
		return nil, fmt.Errorf("build path: %w", err)
	}

	body, err := json.Marshal(opts)
	if err != nil {
		return nil, fmt.Errorf("marshal create role request: %w", err)
	}

	var role Role
	if err := rc.do(ctx, http.MethodPost, path, body, &role); err != nil {
		return nil, fmt.Errorf("create role: %w", err)
	}

	return &role, nil
}

type UpdateRoleOpts struct {
	Name        *string `json:"name,omitempty"`
	Description *string `json:"description,omitempty"`
}

// UpdateRole updates a role by slug via the WorkOS REST API (PATCH).
func (rc *RoleClient) UpdateRole(ctx context.Context, orgID string, roleSlug string, opts UpdateRoleOpts) (*Role, error) {
	if rc == nil {
		return nil, errors.New("role client is not initialized")
	}

	path, err := url.JoinPath("/authorization/organizations", orgID, "roles", roleSlug)
	if err != nil {
		return nil, fmt.Errorf("build path: %w", err)
	}

	body, err := json.Marshal(opts)
	if err != nil {
		return nil, fmt.Errorf("marshal update role request: %w", err)
	}

	var role Role
	if err := rc.do(ctx, http.MethodPatch, path, body, &role); err != nil {
		return nil, fmt.Errorf("update role: %w", err)
	}

	return &role, nil
}

// DeleteRole deletes a role by slug via the WorkOS REST API.
func (rc *RoleClient) DeleteRole(ctx context.Context, orgID string, roleSlug string) error {
	if rc == nil {
		return errors.New("role client is not initialized")
	}

	path, err := url.JoinPath("/authorization/organizations", orgID, "roles", roleSlug)
	if err != nil {
		return fmt.Errorf("build path: %w", err)
	}

	if err := rc.do(ctx, http.MethodDelete, path, nil, nil); err != nil {
		return fmt.Errorf("delete role: %w", err)
	}

	return nil
}

// --- Membership operations ---

// ListMembers lists all active organization memberships for the given org.
func (rc *RoleClient) ListMembers(ctx context.Context, orgID string) ([]Member, error) {
	if rc == nil {
		return nil, errors.New("role client is not initialized")
	}

	var all []Member
	after := ""

	for {
		resp, err := rc.um.ListOrganizationMemberships(ctx, usermanagement.ListOrganizationMembershipsOpts{
			OrganizationID: orgID,
			UserID:         "",
			Statuses:       []usermanagement.OrganizationMembershipStatus{usermanagement.Active},
			Limit:          100,
			Order:          "",
			Before:         "",
			After:          after,
		})
		if err != nil {
			return nil, fmt.Errorf("list organization memberships: %w", err)
		}

		for _, m := range resp.Data {
			all = append(all, Member{
				ID:             m.ID,
				UserID:         m.UserID,
				OrganizationID: m.OrganizationID,
				RoleSlug:       m.Role.Slug,
				CreatedAt:      m.CreatedAt,
			})
		}

		if resp.ListMetadata.After == "" {
			break
		}
		after = resp.ListMetadata.After
	}

	return all, nil
}

// GetUser returns a WorkOS user by ID.
func (rc *RoleClient) GetUser(ctx context.Context, userID string) (*User, error) {
	if rc == nil {
		return nil, errors.New("role client is not initialized")
	}

	u, err := rc.um.GetUser(ctx, usermanagement.GetUserOpts{User: userID})
	if err != nil {
		return nil, fmt.Errorf("get user: %w", err)
	}

	return &User{ID: u.ID, FirstName: u.FirstName, LastName: u.LastName, Email: u.Email}, nil
}

// ListOrgUsers returns all users in the given organization as a map of userID → User.
func (rc *RoleClient) ListOrgUsers(ctx context.Context, orgID string) (map[string]User, error) {
	if rc == nil {
		return nil, errors.New("role client is not initialized")
	}

	users := make(map[string]User)
	after := ""

	for {
		resp, err := rc.um.ListUsers(ctx, usermanagement.ListUsersOpts{
			Email:          "",
			OrganizationID: orgID,
			Limit:          100,
			Order:          "",
			Before:         "",
			After:          after,
		})
		if err != nil {
			return nil, fmt.Errorf("list org users: %w", err)
		}

		for _, u := range resp.Data {
			users[u.ID] = User{ID: u.ID, FirstName: u.FirstName, LastName: u.LastName, Email: u.Email}
		}

		if resp.ListMetadata.After == "" {
			break
		}
		after = resp.ListMetadata.After
	}

	return users, nil
}

// UpdateMemberRole changes a member's role within an organization membership.
func (rc *RoleClient) UpdateMemberRole(ctx context.Context, membershipID string, roleSlug string) (*Member, error) {
	if rc == nil {
		return nil, errors.New("role client is not initialized")
	}

	m, err := rc.um.UpdateOrganizationMembership(ctx, membershipID, usermanagement.UpdateOrganizationMembershipOpts{
		RoleSlug:  roleSlug,
		RoleSlugs: nil,
	})
	if err != nil {
		return nil, fmt.Errorf("update member role: %w", err)
	}

	return &Member{ID: m.ID, UserID: m.UserID, OrganizationID: m.OrganizationID, RoleSlug: m.Role.Slug, CreatedAt: m.CreatedAt}, nil
}

// do performs a raw HTTP request against the WorkOS REST API.
// Pass a non-nil out pointer to decode a JSON response body; pass nil to discard it (e.g. DELETE).
func (rc *RoleClient) do(ctx context.Context, method, path string, body []byte, out any) error {
	req, err := rc.newRequest(ctx, method, path, body)
	if err != nil {
		return err
	}

	resp, err := rc.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("send request: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return resp.Body.Close() })

	if resp.StatusCode >= http.StatusBadRequest {
		respBody, _ := io.ReadAll(resp.Body)
		return &APIError{Method: method, Path: path, StatusCode: resp.StatusCode, Body: string(respBody)}
	}

	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}

	return nil
}

// newRequest builds an authenticated HTTP request targeting the WorkOS API.
func (rc *RoleClient) newRequest(ctx context.Context, method, path string, body []byte) (*http.Request, error) {
	reqURL, err := url.JoinPath(rc.endpoint, path)
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}

	var bodyReader io.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	}

	req, err := http.NewRequestWithContext(ctx, method, reqURL, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+rc.apiKey)
	req.Header.Set("Content-Type", "application/json")

	return req, nil
}
