package workos

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"

	"github.com/workos/workos-go/v6/pkg/organizations"
)

// Role represents an organization role as returned by WorkOS.
type Role struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Slug        string `json:"slug"`
	Description string `json:"description"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

type CreateRoleOpts struct {
	Name        string `json:"name"`
	Slug        string `json:"slug"`
	Description string `json:"description"`
}

type UpdateRoleOpts struct {
	Name        *string `json:"name,omitempty"`
	Description *string `json:"description,omitempty"`
}

func (rc *Client) ListRoles(ctx context.Context, orgID string) ([]Role, error) {
	if rc == nil {
		return nil, errors.New("workos client is not initialized")
	}

	resp, err := rc.orgs.ListOrganizationRoles(ctx, organizations.ListOrganizationRolesOpts{
		OrganizationID: orgID,
	})
	if err != nil {
		return nil, fmt.Errorf("list organization roles: %w", err)
	}

	out := make([]Role, len(resp.Data))
	for i, r := range resp.Data {
		out[i] = Role{ID: r.ID, Name: r.Name, Slug: r.Slug, Description: r.Description, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt}
	}
	return out, nil
}

// CreateRole creates a custom role for an organization via the WorkOS REST API.
// The Go SDK does not expose role CRUD, so we use raw HTTP against the /authorization/ endpoints.
func (rc *Client) CreateRole(ctx context.Context, orgID string, opts CreateRoleOpts) (*Role, error) {
	if rc == nil {
		return nil, errors.New("workos client is not initialized")
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

// UpdateRole updates a role by slug via the WorkOS REST API (PATCH).
func (rc *Client) UpdateRole(ctx context.Context, orgID string, roleSlug string, opts UpdateRoleOpts) (*Role, error) {
	if rc == nil {
		return nil, errors.New("workos client is not initialized")
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
func (rc *Client) DeleteRole(ctx context.Context, orgID string, roleSlug string) error {
	if rc == nil {
		return errors.New("workos client is not initialized")
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
