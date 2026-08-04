package email

// AccessRequest is sent to organization administrators when a user requests
// access to a scope they don't have permission for.
type AccessRequest struct {
	// RequesterName is the display name of the user requesting access.
	RequesterName string
	// RequesterEmail is the email address of the user requesting access.
	RequesterEmail string
	// OrganizationName is the human-readable name of the organization.
	OrganizationName string
	// Scope is the RBAC scope being requested (e.g. "mcp:connect").
	Scope string
	// ResourceName is an optional human-readable name for the resource being
	// accessed (e.g. a project name or MCP server name).
	ResourceName string
	// Message is an optional message from the requester explaining why they
	// need access.
	Message string
	// ManageAccessLink is the absolute URL to the access management page
	// where the admin can grant the requested scope. It carries query params
	// that pre-fill the grant dialog for the requester and scope.
	ManageAccessLink string
	// RolesWithScope is a comma-separated list of role names whose grants
	// already include the requested scope. Empty when no role covers it.
	RolesWithScope string
}

func (AccessRequest) TransactionalID() TransactionalID {
	return transactionalIDAccessRequest
}

func (t AccessRequest) Variables() map[string]string {
	return map[string]string{
		"requester_name":     t.RequesterName,
		"requester_email":    t.RequesterEmail,
		"organization_name":  t.OrganizationName,
		"scope":              t.Scope,
		"resource_name":      t.ResourceName,
		"message":            t.Message,
		"manage_access_link": t.ManageAccessLink,
		"roles_with_scope":   t.RolesWithScope,
	}
}

func (AccessRequest) AddToAudience() bool {
	return false
}
