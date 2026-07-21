package mcpaccess

import (
	"errors"

	"github.com/speakeasy-api/gram/server/internal/oops"
)

// ServerPermissionDeniedMessage is the user-facing MCP server access denial.
const ServerPermissionDeniedMessage = "you do not have permission to use this MCP server. Contact your organization's administrator to request access."

// ToolPermissionDeniedMessage is the user-facing MCP tool access denial.
const ToolPermissionDeniedMessage = "you do not have permission to use this MCP tool. Contact your organization's administrator to request access."

// ServerPermissionDenied replaces a generic forbidden error at an MCP server
// access boundary while preserving all other errors.
func ServerPermissionDenied(err error) error {
	return permissionDenied(err, ServerPermissionDeniedMessage)
}

// ToolPermissionDenied replaces a generic forbidden error at an MCP tool-call
// boundary while preserving all other errors.
func ToolPermissionDenied(err error) error {
	return permissionDenied(err, ToolPermissionDeniedMessage)
}

func permissionDenied(err error, message string) error {
	var shareableErr *oops.ShareableError
	if !errors.As(err, &shareableErr) || shareableErr.Code != oops.CodeForbidden {
		return err
	}

	return oops.E(oops.CodeForbidden, err, "%s", message)
}
