package catalog

import "github.com/speakeasy-api/gram/server/internal/authz"

// ProjectTarget names a project within its organization. Build it from the
// caller's verified session project or a loaded project row, never from
// request input alone.
func ProjectTarget(organizationID, projectID string) Target {
	return Target{
		Resource:  ResourceRef{OrganizationID: organizationID, Kind: authz.ResourceKindProject, ID: projectID},
		Ancestors: []ResourceRef{{OrganizationID: organizationID, Kind: authz.ResourceKindOrg, ID: organizationID}},
	}
}

// ProjectRead asks to view a project and what it contains.
func ProjectRead(target Target) Request {
	return Request{Permission: authz.ScopeProjectRead, Target: target}
}

// ProjectWrite asks to change a project or create something inside it.
func ProjectWrite(target Target) Request {
	return Request{Permission: authz.ScopeProjectWrite, Target: target}
}
