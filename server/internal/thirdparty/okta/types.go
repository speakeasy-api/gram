package okta

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"
)

// Client is the Okta Management API surface used by Gram.
type Client interface {
	ListApps(ctx context.Context, req ListAppsRequest) ([]App, error)
	GetApp(ctx context.Context, appID string) (*App, error)
	ListAppUsers(ctx context.Context, req ListAppUsersRequest) ([]AppUser, error)
	ListAppGroups(ctx context.Context, req ListAppGroupsRequest) ([]AppGroup, error)
	ListGroups(ctx context.Context, req ListGroupsRequest) ([]Group, error)
	VerifyScopes(ctx context.Context, required []string) (*ScopeVerification, error)
}

// App is an Okta application.
type App struct {
	// ID is the Okta application id.
	ID string

	// Label is the admin-facing display label.
	Label string

	// Name is the Okta application template name (for example "oidc_client").
	Name string

	// SignOnMode is the Okta sign-on mode (for example "OPENID_CONNECT").
	SignOnMode string

	// Status is ACTIVE or INACTIVE.
	Status string

	// Features lists enabled provisioning features.
	Features []string

	// Created is when the application was created.
	Created time.Time

	// LastUpdated is when the application was last modified.
	LastUpdated time.Time
}

// AppUser is a user assignment to an application.
type AppUser struct {
	// ID is the Okta user id.
	ID string

	// Scope is USER for a direct assignment or GROUP for a group-derived one.
	Scope string

	// Status is the assignment status (for example "PROVISIONED").
	Status string

	// UserName is the application-scoped user name credential when present.
	UserName string

	// Created is when the assignment was created.
	Created time.Time

	// LastUpdated is when the assignment was last modified.
	LastUpdated time.Time
}

// AppGroup is a group assignment to an application.
type AppGroup struct {
	// ID is the Okta group id.
	ID string

	// Priority orders group assignments; lower wins.
	Priority int

	// LastUpdated is when the assignment was last modified.
	LastUpdated time.Time
}

// Group is an Okta group.
type Group struct {
	// ID is the Okta group id.
	ID string

	// Type is OKTA_GROUP, APP_GROUP, or BUILT_IN.
	Type string

	// Name is the group profile name.
	Name string

	// Description is the group profile description.
	Description string

	// Created is when the group was created.
	Created time.Time

	// LastUpdated is when the group profile was last modified.
	LastUpdated time.Time
}

// ListAppsRequest filters an application listing.
type ListAppsRequest struct {
	// Query matches application label or name by prefix.
	Query string

	// Status restricts results to ACTIVE or INACTIVE applications; empty lists all.
	Status string

	// Limit is the page size; zero uses the Okta default.
	Limit int
}

// ListAppUsersRequest lists user assignments for one application.
type ListAppUsersRequest struct {
	// AppID is the Okta application id.
	AppID string

	// Limit is the page size; zero uses the Okta default.
	Limit int
}

// ListAppGroupsRequest lists group assignments for one application.
type ListAppGroupsRequest struct {
	// AppID is the Okta application id.
	AppID string

	// Limit is the page size; zero uses the Okta default.
	Limit int
}

// ListGroupsRequest filters a group listing.
type ListGroupsRequest struct {
	// Search is plain text matched against group names by prefix.
	Search string

	// Limit is the page size; zero uses the Okta default.
	Limit int
}

// ScopeVerification reports which required scopes the Okta application granted.
type ScopeVerification struct {
	// Granted is every scope present in the access token; never nil.
	Granted []string

	// Missing is every required scope absent from the access token.
	Missing []string

	// DPoPBound is true when Okta issued a DPoP token type.
	DPoPBound bool

	// ExpiresAt is when the verification token expires; zero when none was issued.
	ExpiresAt time.Time
}

// OK reports whether every required scope was granted.
func (v *ScopeVerification) OK() bool {
	return len(v.Missing) == 0
}

// ErrTooManyPages is returned when a listing exceeds the configured page cap.
var ErrTooManyPages = errors.New("okta: listing exceeded page cap")

// ErrResponseTooLarge is returned when a response body exceeds the read limit.
var ErrResponseTooLarge = errors.New("okta: response body exceeds size limit")

// APIError is returned when Okta responds with a 4xx or 5xx status.
type APIError struct {
	// Method is the HTTP method of the failed request.
	Method string

	// Path is the request path without query string.
	Path string

	// StatusCode is the HTTP status Okta returned.
	StatusCode int

	// ErrorCode is the Okta errorCode or OAuth error field when present.
	ErrorCode string

	// Summary is the Okta errorSummary or OAuth error_description when present.
	Summary string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("okta api %s %s: status %d: %s %s", e.Method, e.Path, e.StatusCode, e.ErrorCode, e.Summary)
}

// IsNotFound reports whether err is an APIError with a 404 status code.
func IsNotFound(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound
}
