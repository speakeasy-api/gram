package workos

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/workos/workos-go/v6/pkg/usermanagement"
)

// Member represents an active organization membership.
// RoleSlugs contains all role slugs assigned to this membership.
type Member struct {
	ID             string
	UserID         string
	OrganizationID string
	Organization   string
	RoleSlugs      []string
	Status         string
	CreatedAt      string
	UpdatedAt      string
}

// User represents a WorkOS user with the fields used by Speakeasy.
type User struct {
	ID                string
	FirstName         string
	LastName          string
	Email             string
	ProfilePictureURL string
	ExternalID        string
	CreatedAt         string
	UpdatedAt         string
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

func (wc *Client) UpdateUserExternalID(ctx context.Context, workosUserID, externalID string) error {
	if _, err := wc.um.UpdateUser(ctx, usermanagement.UpdateUserOpts{
		User:             workosUserID,
		Email:            "",
		FirstName:        "",
		LastName:         "",
		EmailVerified:    false,
		Password:         "",
		PasswordHash:     "",
		PasswordHashType: "",
		ExternalID:       externalID,
		Metadata:         nil,
	}); err != nil {
		return fmt.Errorf("update user external ID: %w", err)
	}

	return nil
}

// ListUserMemberships returns all active organization memberships for a user
// across all orgs. Inactive memberships (e.g. SCIM-deactivated users) are
// excluded so login-time syncs never re-provision deactivated access.
// This is more efficient than calling GetOrgMembership per org since it batches the lookup.
func (wc *Client) ListUserMemberships(ctx context.Context, userID string) ([]Member, error) {
	var all []Member
	after := ""

	for {
		resp, err := wc.um.ListOrganizationMemberships(ctx, usermanagement.ListOrganizationMembershipsOpts{
			OrganizationID: "",
			UserID:         userID,
			Statuses:       []usermanagement.OrganizationMembershipStatus{usermanagement.Active},
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

// UpdateMemberRoles replaces all roles on a membership.
func (wc *Client) UpdateMemberRoles(ctx context.Context, membershipID string, roleSlugs []string) (*Member, error) {
	m, err := wc.um.UpdateOrganizationMembership(ctx, membershipID, usermanagement.UpdateOrganizationMembershipOpts{
		RoleSlug:  "",
		RoleSlugs: roleSlugs,
	})
	if err != nil {
		return nil, fmt.Errorf("update member roles: %w", err)
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

// ListOrgMemberships returns all active organization memberships for an org.
// Inactive memberships (e.g. SCIM-deactivated users) are excluded so backfill
// and reconcile syncs never re-provision deactivated access.
func (wc *Client) ListOrgMemberships(ctx context.Context, orgID string) ([]Member, error) {
	var all []Member
	after := ""

	for {
		resp, err := wc.um.ListOrganizationMemberships(ctx, usermanagement.ListOrganizationMembershipsOpts{
			OrganizationID: orgID,
			Limit:          100,
			After:          after,
			UserID:         "",
			Statuses:       []usermanagement.OrganizationMembershipStatus{usermanagement.Active},
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

// RevokeSession ends a WorkOS AuthKit session.
func (wc *Client) RevokeSession(ctx context.Context, sessionID string) error {
	if err := wc.um.RevokeSession(ctx, usermanagement.RevokeSessionOpts{SessionID: sessionID}); err != nil {
		return fmt.Errorf("revoke workos session: %w", err)
	}
	return nil
}

// maxUserSessionPages caps WorkOS session pagination for a single user.
const maxUserSessionPages = 50

// ListUserSessionIDs lists all WorkOS AuthKit session IDs for workosUserID.
func (wc *Client) ListUserSessionIDs(ctx context.Context, workosUserID string) ([]string, error) {
	if strings.TrimSpace(workosUserID) == "" {
		return nil, fmt.Errorf("list workos user sessions: missing workos user ID")
	}

	var ids []string
	after := ""
	for range maxUserSessionPages {
		resp, err := wc.um.ListSessions(ctx, workosUserID, usermanagement.ListSessionsOpts{
			Limit:  100,
			Before: "",
			After:  after,
			Order:  usermanagement.Desc,
		})
		if err != nil {
			return nil, wrapSDKError(err, "list workos user sessions")
		}
		for _, sess := range resp.Data {
			if sess.ID != "" {
				ids = append(ids, sess.ID)
			}
		}
		if resp.ListMetadata.After == "" || len(resp.Data) == 0 {
			return ids, nil
		}
		after = resp.ListMetadata.After
	}

	return nil, fmt.Errorf("list workos user sessions: exceeded %d pages", maxUserSessionPages)
}

// SetUserBlacklistedMetadata updates the WorkOS user's custom "blacklisted"
// metadata key without disturbing other metadata keys on the user.
func (wc *Client) SetUserBlacklistedMetadata(ctx context.Context, workosUserID string, blacklisted bool) error {
	if strings.TrimSpace(workosUserID) == "" {
		return fmt.Errorf("set workos user blacklisted metadata: missing workos user ID")
	}
	value := "false"
	if blacklisted {
		value = "true"
	}
	if _, err := wc.um.UpdateUser(ctx, usermanagement.UpdateUserOpts{
		User:             workosUserID,
		Email:            "",
		FirstName:        "",
		LastName:         "",
		EmailVerified:    false,
		Password:         "",
		PasswordHash:     "",
		PasswordHashType: "",
		ExternalID:       "",
		Metadata: map[string]*string{
			"blacklisted": &value,
		},
	}); err != nil {
		return wrapSDKError(err, "set workos user blacklisted metadata")
	}
	return nil
}

type radarListEntryRequest struct {
	Entry string `json:"entry"`
}

// AddRadarEmailBlocklistEntry adds email to the WorkOS Radar email blocklist
// via POST /radar/lists/email/block. WorkOS returns 200/201/204 on success
// (including when the entry is already present).
func (wc *Client) AddRadarEmailBlocklistEntry(ctx context.Context, email string) error {
	normalized := strings.ToLower(strings.TrimSpace(email))
	if normalized == "" {
		return fmt.Errorf("add workos radar email blocklist entry: missing email")
	}
	body, err := json.Marshal(radarListEntryRequest{Entry: normalized})
	if err != nil {
		return fmt.Errorf("marshal workos radar email blocklist entry: %w", err)
	}
	if err := wc.do(ctx, http.MethodPost, "/radar/lists/email/block", body, nil); err != nil {
		return fmt.Errorf("add workos radar email blocklist entry: %w", err)
	}
	return nil
}
