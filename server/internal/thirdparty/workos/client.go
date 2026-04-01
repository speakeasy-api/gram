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
	Organization   string
	RoleSlug       string
	Status         string
	CreatedAt      string
	UpdatedAt      string
}

// User represents a WorkOS user with the fields used by Gram.
type User struct {
	ID        string
	FirstName string
	LastName  string
	Email     string
}

type InvitationState string

const (
	InvitationStatePending  InvitationState = "pending"
	InvitationStateAccepted InvitationState = "accepted"
	InvitationStateExpired  InvitationState = "expired"
	InvitationStateRevoked  InvitationState = "revoked"
)

// Invitation represents a WorkOS invitation with the fields used by Gram.
type Invitation struct {
	ID                  string
	Email               string
	State               InvitationState
	AcceptedAt          string
	RevokedAt           string
	Token               string
	AcceptInvitationURL string
	OrganizationID      string
	InviterUserID       string
	ExpiresAt           string
	CreatedAt           string
	UpdatedAt           string
}

type SendInvitationOpts struct {
	Email          string `json:"email"`
	OrganizationID string `json:"organization_id,omitempty"`
	ExpiresInDays  int    `json:"expires_in_days,omitempty"`
	InviterUserID  string `json:"inviter_user_id,omitempty"`
	RoleSlug       string `json:"role_slug,omitempty"`
}

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

// Client wraps WorkOS API calls for role and membership management.
// It is designed to have a caching layer added later.
type Client struct {
	apiKey     string
	endpoint   string // base URL for raw HTTP calls; defaults to workosBaseURL
	httpClient *http.Client
	orgs       *organizations.Client
	um         *usermanagement.Client
}

// ClientOpts configures optional overrides for New.
// Zero values use production defaults. Primarily used in tests.
type ClientOpts struct {
	// Endpoint overrides the WorkOS base URL for both raw HTTP and SDK calls.
	Endpoint string
	// HTTPClient overrides the default retryable HTTP client.
	HTTPClient *http.Client
}

func NewClient(apiKey string, opts ...ClientOpts) (*Client, error) {
	if apiKey == "" || apiKey == "unset" {
		return nil, errors.New("no API key provided to initialize WorkOS client")
	}

	var opt ClientOpts
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

	return &Client{
		apiKey:     apiKey,
		endpoint:   endpoint,
		httpClient: httpClient,
		orgs:       &organizations.Client{APIKey: apiKey, HTTPClient: httpClient, Endpoint: opt.Endpoint, JSONEncode: nil},
		um:         um,
	}, nil
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

type UpdateRoleOpts struct {
	Name        *string `json:"name,omitempty"`
	Description *string `json:"description,omitempty"`
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

// --- Membership operations ---

// ListMembers lists all active organization memberships for the given org.
func (rc *Client) ListMembers(ctx context.Context, orgID string) ([]Member, error) {
	if rc == nil {
		return nil, errors.New("workos client is not initialized")
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
func (rc *Client) ListUsersInOrg(ctx context.Context, orgID string) ([]User, error) {
	if rc == nil {
		return nil, errors.New("workos client is not initialized")
	}

	var all []User
	after := ""

	for {
		resp, err := rc.um.ListUsers(ctx, usermanagement.ListUsersOpts{
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
func (rc *Client) GetUserByEmail(ctx context.Context, email string) (*User, error) {
	if rc == nil {
		return nil, errors.New("workos client is not initialized")
	}

	resp, err := rc.um.ListUsers(ctx, usermanagement.ListUsersOpts{
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

	u := resp.Data[0]
	user := convertUser(u)
	return &user, nil
}

// GetUser returns a WorkOS user by ID.
func (rc *Client) GetUser(ctx context.Context, userID string) (*User, error) {
	if rc == nil {
		return nil, errors.New("workos client is not initialized")
	}

	u, err := rc.um.GetUser(ctx, usermanagement.GetUserOpts{User: userID})
	if err != nil {
		return nil, fmt.Errorf("get user: %w", err)
	}

	user := convertUser(u)
	return &user, nil
}

// GetOrgMembership returns the first membership matching a user and organization.
func (rc *Client) GetOrgMembership(ctx context.Context, workOSUserID, workOSOrgID string) (*Member, error) {
	if rc == nil {
		return nil, errors.New("workos client is not initialized")
	}

	resp, err := rc.um.ListOrganizationMemberships(ctx, usermanagement.ListOrganizationMembershipsOpts{
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

// SendInvitation creates an invitation for a user to join an organization.
func (rc *Client) SendInvitation(ctx context.Context, opts SendInvitationOpts) (*Invitation, error) {
	if rc == nil {
		return nil, errors.New("workos client is not initialized")
	}

	inv, err := rc.um.SendInvitation(ctx, usermanagement.SendInvitationOpts{
		Email:          opts.Email,
		OrganizationID: opts.OrganizationID,
		ExpiresInDays:  opts.ExpiresInDays,
		InviterUserID:  opts.InviterUserID,
		RoleSlug:       opts.RoleSlug,
	})
	if err != nil {
		return nil, fmt.Errorf("send invitation: %w", err)
	}

	converted := convertInvitation(inv)
	return &converted, nil
}

// ListInvitations returns all invitations for an organization.
func (rc *Client) ListInvitations(ctx context.Context, orgID string) ([]Invitation, error) {
	if rc == nil {
		return nil, errors.New("workos client is not initialized")
	}

	var all []Invitation
	after := ""

	for {
		resp, err := rc.um.ListInvitations(ctx, usermanagement.ListInvitationsOpts{
			OrganizationID: orgID,
			Limit:          100,
			After:          after,
			Email:          "",
			Order:          "",
			Before:         "",
		})
		if err != nil {
			return nil, fmt.Errorf("list invitations: %w", err)
		}

		for _, inv := range resp.Data {
			all = append(all, convertInvitation(inv))
		}

		if resp.ListMetadata.After == "" {
			break
		}
		after = resp.ListMetadata.After
	}

	return all, nil
}

// RevokeInvitation revokes an invitation by ID.
func (rc *Client) RevokeInvitation(ctx context.Context, invitationID string) (*Invitation, error) {
	if rc == nil {
		return nil, errors.New("workos client is not initialized")
	}

	inv, err := rc.um.RevokeInvitation(ctx, usermanagement.RevokeInvitationOpts{Invitation: invitationID})
	if err != nil {
		return nil, fmt.Errorf("revoke invitation: %w", err)
	}

	converted := convertInvitation(inv)
	return &converted, nil
}

// ResendInvitation resends an invitation by ID.
func (rc *Client) ResendInvitation(ctx context.Context, invitationID string) (*Invitation, error) {
	if rc == nil {
		return nil, errors.New("workos client is not initialized")
	}

	inv, err := rc.um.ResendInvitation(ctx, usermanagement.ResendInvitationOpts{Invitation: invitationID})
	if err != nil {
		return nil, fmt.Errorf("resend invitation: %w", err)
	}

	converted := convertInvitation(inv)
	return &converted, nil
}

// FindInvitationByToken resolves an invitation from its token.
func (rc *Client) FindInvitationByToken(ctx context.Context, token string) (*Invitation, error) {
	if rc == nil {
		return nil, errors.New("workos client is not initialized")
	}

	inv, err := rc.um.FindInvitationByToken(ctx, usermanagement.FindInvitationByTokenOpts{InvitationToken: token})
	if err != nil {
		return nil, fmt.Errorf("find invitation by token: %w", err)
	}

	converted := convertInvitation(inv)
	return &converted, nil
}

// GetInvitation returns an invitation by ID.
func (rc *Client) GetInvitation(ctx context.Context, invitationID string) (*Invitation, error) {
	if rc == nil {
		return nil, errors.New("workos client is not initialized")
	}

	inv, err := rc.um.GetInvitation(ctx, usermanagement.GetInvitationOpts{Invitation: invitationID})
	if err != nil {
		return nil, fmt.Errorf("get invitation: %w", err)
	}

	converted := convertInvitation(inv)
	return &converted, nil
}

// DeleteOrganizationMembership deletes an organization membership by ID.
func (rc *Client) DeleteOrganizationMembership(ctx context.Context, membershipID string) error {
	if rc == nil {
		return errors.New("workos client is not initialized")
	}

	err := rc.um.DeleteOrganizationMembership(ctx, usermanagement.DeleteOrganizationMembershipOpts{
		OrganizationMembership: membershipID,
	})
	if err != nil {
		return fmt.Errorf("delete organization membership: %w", err)
	}

	return nil
}

// ListOrgMemberships returns all organization memberships for an org.
func (rc *Client) ListOrgMemberships(ctx context.Context, orgID string) ([]Member, error) {
	if rc == nil {
		return nil, errors.New("workos client is not initialized")
	}

	var all []Member
	after := ""

	for {
		resp, err := rc.um.ListOrganizationMemberships(ctx, usermanagement.ListOrganizationMembershipsOpts{
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

// ListOrgUsers returns all users in the given organization as a map of userID → User.
func (rc *Client) ListOrgUsers(ctx context.Context, orgID string) (map[string]User, error) {
	if rc == nil {
		return nil, errors.New("workos client is not initialized")
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
func (rc *Client) UpdateMemberRole(ctx context.Context, membershipID string, roleSlug string) (*Member, error) {
	if rc == nil {
		return nil, errors.New("workos client is not initialized")
	}

	m, err := rc.um.UpdateOrganizationMembership(ctx, membershipID, usermanagement.UpdateOrganizationMembershipOpts{
		RoleSlug:  roleSlug,
		RoleSlugs: nil,
	})
	if err != nil {
		return nil, fmt.Errorf("update member role: %w", err)
	}

	member := convertMember(m)
	return &member, nil
}

func convertUser(u usermanagement.User) User {
	return User{ID: u.ID, FirstName: u.FirstName, LastName: u.LastName, Email: u.Email}
}

func convertMember(m usermanagement.OrganizationMembership) Member {
	return Member{
		ID:             m.ID,
		UserID:         m.UserID,
		OrganizationID: m.OrganizationID,
		Organization:   m.OrganizationName,
		RoleSlug:       m.Role.Slug,
		Status:         string(m.Status),
		CreatedAt:      m.CreatedAt,
		UpdatedAt:      m.UpdatedAt,
	}
}

func convertInvitation(inv usermanagement.Invitation) Invitation {
	return Invitation{
		ID:                  inv.ID,
		Email:               inv.Email,
		State:               InvitationState(inv.State),
		AcceptedAt:          inv.AcceptedAt,
		RevokedAt:           inv.RevokedAt,
		Token:               inv.Token,
		AcceptInvitationURL: inv.AcceptInvitationUrl,
		OrganizationID:      inv.OrganizationID,
		InviterUserID:       inv.InviterUserID,
		ExpiresAt:           inv.ExpiresAt,
		CreatedAt:           inv.CreatedAt,
		UpdatedAt:           inv.UpdatedAt,
	}
}

// do performs a raw HTTP request against the WorkOS REST API.
// Pass a non-nil out pointer to decode a JSON response body; pass nil to discard it (e.g. DELETE).
func (rc *Client) do(ctx context.Context, method, path string, body []byte, out any) error {
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
func (rc *Client) newRequest(ctx context.Context, method, path string, body []byte) (*http.Request, error) {
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
