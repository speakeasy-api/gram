package mcpaccess

import (
	"errors"
	"net/url"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/oops"
)

// ServerPermissionDeniedMessage is the user-facing MCP server access denial.
const ServerPermissionDeniedMessage = "access to this MCP server was restricted because your permissions do not include mcp:connect. Ask an organization administrator to grant access."

// ToolPermissionDeniedMessage is the user-facing MCP tool access denial.
const ToolPermissionDeniedMessage = "you do not have permission to use this MCP tool. Contact your organization's administrator to request access."

// ServerPermissionDenied replaces a generic forbidden error at an MCP server
// access boundary while preserving all other errors. When requestAccessURL is
// present, the message links directly to the authorization challenge grant flow.
func ServerPermissionDenied(err error, requestAccessURL string) error {
	message := ServerPermissionDeniedMessage
	if requestAccessURL = strings.TrimSpace(requestAccessURL); requestAccessURL != "" {
		message += "\n\nRequest access:\n" + requestAccessURL
	}

	return permissionDenied(err, message)
}

// ToolPermissionDenied replaces a generic forbidden error at an MCP tool-call
// boundary while preserving all other errors.
func ToolPermissionDenied(err error) error {
	return permissionDenied(err, ToolPermissionDeniedMessage)
}

// AuthorizationChallengesURL returns the organization-scoped grant flow used
// to resolve the authorization challenge recorded by a denied mcp:connect.
func AuthorizationChallengesURL(siteURL *url.URL, organizationSlug string) string {
	if siteURL == nil || strings.TrimSpace(organizationSlug) == "" {
		return ""
	}

	return siteURL.JoinPath(organizationSlug, "access", "challenges").String()
}

// RequestAccessURLParams holds optional parameters for building a request access URL.
type RequestAccessURLParams struct {
	// Scope is the RBAC scope being requested (e.g. "mcp:connect").
	Scope string
	// ResourceID is an optional identifier for the specific resource.
	ResourceID string
	// ResourceName is a human-readable name for the resource.
	ResourceName string
}

// RequestAccessURL returns a URL that allows end users to request access to a
// scope by sending an email notification to organization administrators.
func RequestAccessURL(siteURL *url.URL, organizationSlug string, params RequestAccessURLParams) string {
	if siteURL == nil || strings.TrimSpace(organizationSlug) == "" {
		return ""
	}

	u := siteURL.JoinPath(organizationSlug, "request-access")
	q := u.Query()
	if params.Scope != "" {
		q.Set("scope", params.Scope)
	}
	if params.ResourceID != "" {
		q.Set("resource_id", params.ResourceID)
	}
	if params.ResourceName != "" {
		q.Set("resource_name", params.ResourceName)
	}
	u.RawQuery = q.Encode()

	return u.String()
}

func permissionDenied(err error, message string) error {
	var shareableErr *oops.ShareableError
	if !errors.As(err, &shareableErr) || shareableErr.Code != oops.CodeForbidden {
		return err
	}

	return oops.E(oops.CodeForbidden, err, "%s", message)
}
