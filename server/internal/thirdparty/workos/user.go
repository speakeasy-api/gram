package workos

import (
	"context"
	"fmt"

	"github.com/workos/workos-go/v6/pkg/usermanagement"
)

// Member represents an active organization membership.
// RoleSlug is the slug of the member's assigned role.
type Member struct {
	ID             string
	UserID         string
	OrganizationID string
	Organization   string
	RoleSlug       string
	Status         string
	CreatedAt      string
	UpdatedAt      string
}

// User represents a WorkOS user with the fields used by Gram.
type User struct {
	ID                string
	FirstName         string
	LastName          string
	Email             string
	ProfilePictureURL string
}

// ListMembers lists all active organization memberships for the given org.
func (wc *Client) ListMembers(ctx context.Context, orgID string) ([]Member, error) {
	var all []Member
	after := ""

	for {
		resp, err := wc.um.ListOrganizationMemberships(ctx, usermanagement.ListOrganizationMembershipsOpts{
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
			all = append(all, convertMember(m))
		}

		if resp.ListMetadata.After == "" {
			break
		}
		after = resp.ListMetadata.After
	}

	return all, nil
}

// ListUsersInOrg returns all users in the given organization.
func (wc *Client) ListUsersInOrg(ctx context.Context, orgID string) ([]User, error) {
	var all []User
	after := ""

	for {
		resp, err := wc.um.ListUsers(ctx, usermanagement.ListUsersOpts{
			OrganizationID: orgID,
			Limit:          100,
			After:          after,
			Email:          "",
			Order:          "",
			Before:         "",
		})
		if err != nil {
			return nil, fmt.Errorf("list users in org: %w", err)
		}

		for _, u := range resp.Data {
			all = append(all, convertUser(u))
		}

		if resp.ListMetadata.After == "" {
			break
		}
		after = resp.ListMetadata.After
	}

	return all, nil
}

// GetUserByEmail returns the first WorkOS user for the given email.
func (wc *Client) GetUserByEmail(ctx context.Context, email string) (*User, error) {
	resp, err := wc.um.ListUsers(ctx, usermanagement.ListUsersOpts{
		Email:          email,
		OrganizationID: "",
		Limit:          1,
		Order:          "",
		Before:         "",
		After:          "",
	})
	if err != nil {
		return nil, fmt.Errorf("list users by email: %w", err)
	}

	if len(resp.Data) == 0 {
		return nil, nil
	}

	user := convertUser(resp.Data[0])
	return &user, nil
}

// GetUser returns a WorkOS user by ID.
func (wc *Client) GetUser(ctx context.Context, userID string) (*User, error) {
	u, err := wc.um.GetUser(ctx, usermanagement.GetUserOpts{User: userID})
	if err != nil {
		return nil, fmt.Errorf("get user: %w", err)
	}

	user := convertUser(u)
	return &user, nil
}

// ListUserMemberships returns all organization memberships for a user across all orgs.
// This is more efficient than calling GetOrgMembership per org since it batches the lookup.
func (wc *Client) ListUserMemberships(ctx context.Context, userID string) ([]Member, error) {
	var all []Member
	after := ""

	for {
		resp, err := wc.um.ListOrganizationMemberships(ctx, usermanagement.ListOrganizationMembershipsOpts{
			OrganizationID: "",
			UserID:         userID,
			Statuses:       nil,
			Limit:          100,
			Order:          "",
			Before:         "",
			After:          after,
		})
		if err != nil {
			return nil, fmt.Errorf("list user memberships: %w", err)
		}

		for _, m := range resp.Data {
			all = append(all, convertMember(m))
		}

		if resp.ListMetadata.After == "" {
			break
		}
		after = resp.ListMetadata.After
	}

	return all, nil
}

// GetOrgMembership returns the first membership matching a user and organization.
func (wc *Client) GetOrgMembership(ctx context.Context, workOSUserID, workOSOrgID string) (*Member, error) {
	resp, err := wc.um.ListOrganizationMemberships(ctx, usermanagement.ListOrganizationMembershipsOpts{
		OrganizationID: workOSOrgID,
		UserID:         workOSUserID,
		Statuses:       nil,
		Limit:          1,
		Order:          "",
		Before:         "",
		After:          "",
	})
	if err != nil {
		return nil, fmt.Errorf("list organization memberships: %w", err)
	}

	if len(resp.Data) == 0 {
		return nil, nil
	}

	m := convertMember(resp.Data[0])
	return &m, nil
}

// ListOrgUsers returns all users in the given organization as a map of userID → User.
func (wc *Client) ListOrgUsers(ctx context.Context, orgID string) (map[string]User, error) {
	users := make(map[string]User)
	after := ""

	for {
		resp, err := wc.um.ListUsers(ctx, usermanagement.ListUsersOpts{
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
			users[u.ID] = convertUser(u)
		}

		if resp.ListMetadata.After == "" {
			break
		}
		after = resp.ListMetadata.After
	}

	return users, nil
}

// UpdateMemberRole changes a member's role within an organization membership.
func (wc *Client) UpdateMemberRole(ctx context.Context, membershipID string, roleSlug string) (*Member, error) {
	m, err := wc.um.UpdateOrganizationMembership(ctx, membershipID, usermanagement.UpdateOrganizationMembershipOpts{
		RoleSlug:  roleSlug,
		RoleSlugs: nil,
	})
	if err != nil {
		return nil, fmt.Errorf("update member role: %w", err)
	}

	member := convertMember(m)
	return &member, nil
}

// DeleteOrganizationMembership deletes an organization membership by ID.
func (wc *Client) DeleteOrganizationMembership(ctx context.Context, membershipID string) error {
	err := wc.um.DeleteOrganizationMembership(ctx, usermanagement.DeleteOrganizationMembershipOpts{
		OrganizationMembership: membershipID,
	})
	if err != nil {
		return fmt.Errorf("delete organization membership: %w", err)
	}

	return nil
}

// ListOrgMemberships returns all organization memberships for an org.
func (wc *Client) ListOrgMemberships(ctx context.Context, orgID string) ([]Member, error) {
	var all []Member
	after := ""

	for {
		resp, err := wc.um.ListOrganizationMemberships(ctx, usermanagement.ListOrganizationMembershipsOpts{
			OrganizationID: orgID,
			Limit:          100,
			After:          after,
			UserID:         "",
			Statuses:       nil,
			Order:          "",
			Before:         "",
		})
		if err != nil {
			return nil, fmt.Errorf("list org memberships: %w", err)
		}

		for _, m := range resp.Data {
			all = append(all, convertMember(m))
		}

		if resp.ListMetadata.After == "" {
			break
		}
		after = resp.ListMetadata.After
	}

	return all, nil
}
