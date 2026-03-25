package workos

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"github.com/workos/workos-go/v6/pkg/organizations"
	"github.com/workos/workos-go/v6/pkg/roles"
	"github.com/workos/workos-go/v6/pkg/usermanagement"
)

// RoleClient wraps WorkOS API calls for role and membership management.
// It is designed to have a caching layer added later.
type RoleClient struct {
	logger *slog.Logger
	apiKey string
	orgs   *organizations.Client
	um     *usermanagement.Client
}

func NewRoleClient(logger *slog.Logger, apiKey string) *RoleClient {
	if apiKey == "" || apiKey == "unset" {
		return nil
	}

	return &RoleClient{
		logger: logger,
		apiKey: apiKey,
		orgs:   &organizations.Client{APIKey: apiKey},
		um:     usermanagement.NewClient(apiKey),
	}
}

// --- Role operations ---

func (rc *RoleClient) ListRoles(ctx context.Context, orgID string) ([]roles.Role, error) {
	if rc == nil {
		return nil, errors.New("role client is not initialized")
	}

	resp, err := rc.orgs.ListOrganizationRoles(ctx, organizations.ListOrganizationRolesOpts{
		OrganizationID: orgID,
	})
	if err != nil {
		return nil, fmt.Errorf("list organization roles: %w", err)
	}

	return resp.Data, nil
}

type CreateRoleOpts struct {
	Name        string `json:"name"`
	Slug        string `json:"slug"`
	Description string `json:"description"`
}

// CreateRole creates a custom role for an organization via the WorkOS REST API.
// The Go SDK does not expose role CRUD, so we use raw HTTP against the /authorization/ endpoints.
func (rc *RoleClient) CreateRole(ctx context.Context, orgID string, opts CreateRoleOpts) (*roles.Role, error) {
	if rc == nil {
		return nil, errors.New("role client is not initialized")
	}

	body, err := json.Marshal(opts)
	if err != nil {
		return nil, fmt.Errorf("marshal create role request: %w", err)
	}

	var role roles.Role
	if err := rc.doAPI(ctx, http.MethodPost, "/authorization/organizations/"+orgID+"/roles", body, &role); err != nil {
		return nil, fmt.Errorf("create role: %w", err)
	}

	return &role, nil
}

type UpdateRoleOpts struct {
	Name        *string  `json:"name,omitempty"`
	Description *string  `json:"description,omitempty"`
	Permissions []string `json:"permissions,omitempty"`
}

// UpdateRole updates a role by slug via the WorkOS REST API (PATCH).
func (rc *RoleClient) UpdateRole(ctx context.Context, orgID string, roleSlug string, opts UpdateRoleOpts) (*roles.Role, error) {
	if rc == nil {
		return nil, errors.New("role client is not initialized")
	}

	body, err := json.Marshal(opts)
	if err != nil {
		return nil, fmt.Errorf("marshal update role request: %w", err)
	}

	var role roles.Role
	if err := rc.doAPI(ctx, http.MethodPatch, "/authorization/organizations/"+orgID+"/roles/"+roleSlug, body, &role); err != nil {
		return nil, fmt.Errorf("update role: %w", err)
	}

	return &role, nil
}

// DeleteRole deletes a role by slug via the WorkOS REST API.
func (rc *RoleClient) DeleteRole(ctx context.Context, orgID string, roleSlug string) error {
	if rc == nil {
		return errors.New("role client is not initialized")
	}

	if err := rc.doAPI(ctx, http.MethodDelete, "/authorization/organizations/"+orgID+"/roles/"+roleSlug, nil, nil); err != nil {
		return fmt.Errorf("delete role: %w", err)
	}

	return nil
}

// --- Membership operations ---

// ListMembers lists all organization memberships (active only) for the given org.
func (rc *RoleClient) ListMembers(ctx context.Context, orgID string) ([]usermanagement.OrganizationMembership, error) {
	if rc == nil {
		return nil, errors.New("role client is not initialized")
	}

	var all []usermanagement.OrganizationMembership
	after := ""

	for {
		resp, err := rc.um.ListOrganizationMemberships(ctx, usermanagement.ListOrganizationMembershipsOpts{
			OrganizationID: orgID,
			Statuses:       []usermanagement.OrganizationMembershipStatus{usermanagement.Active},
			Limit:          100,
			After:          after,
		})
		if err != nil {
			return nil, fmt.Errorf("list organization memberships: %w", err)
		}

		all = append(all, resp.Data...)

		if resp.ListMetadata.After == "" {
			break
		}
		after = resp.ListMetadata.After
	}

	return all, nil
}

// GetUser returns a WorkOS user by ID.
func (rc *RoleClient) GetUser(ctx context.Context, userID string) (*usermanagement.User, error) {
	if rc == nil {
		return nil, errors.New("role client is not initialized")
	}

	user, err := rc.um.GetUser(ctx, usermanagement.GetUserOpts{User: userID})
	if err != nil {
		return nil, fmt.Errorf("get user: %w", err)
	}

	return &user, nil
}

// UpdateMemberRole changes a member's role within an organization membership.
func (rc *RoleClient) UpdateMemberRole(ctx context.Context, membershipID string, roleSlug string) (*usermanagement.OrganizationMembership, error) {
	if rc == nil {
		return nil, errors.New("role client is not initialized")
	}

	membership, err := rc.um.UpdateOrganizationMembership(ctx, membershipID, usermanagement.UpdateOrganizationMembershipOpts{
		RoleSlug: roleSlug,
	})
	if err != nil {
		return nil, fmt.Errorf("update member role: %w", err)
	}

	return &membership, nil
}

// --- Internal helpers ---

const workosBaseURL = "https://api.workos.com"

// doAPI performs a raw HTTP request against the WorkOS REST API.
// Used for endpoints not covered by the Go SDK (role CRUD).
func (rc *RoleClient) doAPI(ctx context.Context, method, path string, body []byte, out any) error {
	var bodyReader io.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	}

	req, err := http.NewRequestWithContext(ctx, method, workosBaseURL+path, bodyReader)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+rc.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("workos api %s %s returned %d: %s", method, path, resp.StatusCode, string(respBody))
	}

	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}

	return nil
}
