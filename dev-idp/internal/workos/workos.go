// Package workos is dev-idp's tiny WorkOS client. It wraps just the
// usermanagement SDK calls dev-idp's `workos` mode handler needs:
// GetUser, GetUserByEmail, plus an IsNotFound helper.
package workos

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/workos/workos-go/v6/pkg/organizations"
	"github.com/workos/workos-go/v6/pkg/usermanagement"
	"github.com/workos/workos-go/v6/pkg/workos_errors"
)

const defaultBaseURL = "https://api.workos.com"

// User mirrors the fields dev-idp's workos handler reads off the WorkOS
// User shape. Kept narrow on purpose -- if a future caller needs more, add
// the field rather than re-exporting the SDK type.
type User struct {
	ID                string
	FirstName         string
	LastName          string
	Email             string
	ProfilePictureURL string
}

// Organization mirrors the WorkOS organization fields dev-idp consumes.
type Organization struct {
	ID        string
	Name      string
	CreatedAt string
	UpdatedAt string
}

// Member is one row of a user's organization-membership list. The
// embedded Organization name is what WorkOS returns in
// OrganizationMembership.OrganizationName.
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

// Client wraps the WorkOS user-management + organizations SDKs.
type Client struct {
	um   *usermanagement.Client
	orgs *organizations.Client
}

// Opts configures optional overrides for NewClient.
type Opts struct {
	// Endpoint overrides the WorkOS base URL. Empty uses the production URL.
	Endpoint string
	// HTTPClient overrides the default http.DefaultClient.
	HTTPClient *http.Client
}

func NewClient(apiKey string, opts ...Opts) *Client {
	var opt Opts
	if len(opts) > 0 {
		opt = opts[0]
	}

	um := usermanagement.NewClient(apiKey)
	orgs := &organizations.Client{APIKey: apiKey, HTTPClient: nil, Endpoint: "", JSONEncode: nil}
	if opt.HTTPClient != nil {
		um.HTTPClient = opt.HTTPClient
		orgs.HTTPClient = opt.HTTPClient
	}
	if opt.Endpoint != "" {
		um.Endpoint = opt.Endpoint
		orgs.Endpoint = opt.Endpoint
	} else {
		um.Endpoint = defaultBaseURL
	}

	return &Client{um: um, orgs: orgs}
}

// GetOrganization fetches a WorkOS organization by id.
func (c *Client) GetOrganization(ctx context.Context, orgID string) (*Organization, error) {
	o, err := c.orgs.GetOrganization(ctx, organizations.GetOrganizationOpts{Organization: orgID})
	if err != nil {
		return nil, fmt.Errorf("workos get organization: %w", err)
	}
	return &Organization{
		ID:        o.ID,
		Name:      o.Name,
		CreatedAt: o.CreatedAt,
		UpdatedAt: o.UpdatedAt,
	}, nil
}

// ListUserMemberships paginates through every membership a user has
// across all organizations they belong to.
func (c *Client) ListUserMemberships(ctx context.Context, userID string) ([]Member, error) {
	var all []Member
	after := ""
	for {
		resp, err := c.um.ListOrganizationMemberships(ctx, usermanagement.ListOrganizationMembershipsOpts{
			OrganizationID: "",
			UserID:         userID,
			Statuses:       nil,
			Limit:          100,
			Order:          "",
			Before:         "",
			After:          after,
		})
		if err != nil {
			return nil, fmt.Errorf("workos list user memberships: %w", err)
		}
		for _, m := range resp.Data {
			all = append(all, Member{
				ID:             m.ID,
				UserID:         m.UserID,
				OrganizationID: m.OrganizationID,
				Organization:   m.OrganizationName,
				RoleSlug:       m.Role.Slug,
				Status:         string(m.Status),
				CreatedAt:      m.CreatedAt,
				UpdatedAt:      m.UpdatedAt,
			})
		}
		if resp.ListMetadata.After == "" {
			break
		}
		after = resp.ListMetadata.After
	}
	return all, nil
}

// GetUser fetches a WorkOS user by ID. Returns an error wrapping a 404
// (detectable via IsNotFound) when the user does not exist.
func (c *Client) GetUser(ctx context.Context, id string) (*User, error) {
	u, err := c.um.GetUser(ctx, usermanagement.GetUserOpts{User: id})
	if err != nil {
		return nil, fmt.Errorf("workos get user: %w", err)
	}
	return convert(u), nil
}

// GetUserByEmail returns the first WorkOS user with the given email, or a
// not-found error if none exists. Empty email is rejected up front.
func (c *Client) GetUserByEmail(ctx context.Context, email string) (*User, error) {
	if email == "" {
		return nil, ErrNotFound
	}
	resp, err := c.um.ListUsers(ctx, usermanagement.ListUsersOpts{
		Email:          email,
		OrganizationID: "",
		Limit:          1,
		Order:          "",
		Before:         "",
		After:          "",
	})
	if err != nil {
		return nil, fmt.Errorf("workos list users by email: %w", err)
	}
	if len(resp.Data) == 0 {
		return nil, fmt.Errorf("%w: workos user with email %q", ErrNotFound, email)
	}
	return convert(resp.Data[0]), nil
}

// ErrNotFound is returned (or wrapped) when a lookup yields no result.
var ErrNotFound = errors.New("workos: not found")

// IsNotFound reports whether err is (or wraps) a not-found result. This
// includes both ErrNotFound (returned for empty list responses) and HTTP
// 404 errors from the SDK.
func IsNotFound(err error) bool {
	if errors.Is(err, ErrNotFound) {
		return true
	}
	var httpErr workos_errors.HTTPError
	if errors.As(err, &httpErr) && httpErr.Code == http.StatusNotFound {
		return true
	}
	return false
}

func convert(u usermanagement.User) *User {
	return &User{
		ID:                u.ID,
		FirstName:         u.FirstName,
		LastName:          u.LastName,
		Email:             u.Email,
		ProfilePictureURL: u.ProfilePictureURL,
	}
}
