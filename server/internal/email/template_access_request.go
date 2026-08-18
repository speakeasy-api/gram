package email

// AccessRequest is sent to organization administrators when a user requests
// access to a scope they don't have permission for.
type AccessRequest struct {
	// RequesterName is the display name of the user requesting access.
	RequesterName string
	// OrganizationName is the human-readable name of the organization.
	OrganizationName string
	// ManageAccessLink is the absolute URL to the access management page
	// where the admin can grant the requested scope. It carries query params
	// that pre-fill the grant dialog for the requester and scope.
	ManageAccessLink string
}

func (AccessRequest) Key() TemplateKey {
	return TemplateKeyAccessRequest
}

func (t AccessRequest) Variables() map[string]string {
	return map[string]string{
		"requester_name":     t.RequesterName,
		"organization_name":  t.OrganizationName,
		"manage_access_link": t.ManageAccessLink,
	}
}

func (AccessRequest) AddToAudience() bool {
	return false
}
