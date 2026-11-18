// Package staff identifies the Speakeasy staff organization of a deployment.
//
// Each partition has its own database, so the staff organization is a
// different row in each one (speakeasy-team in the US). Its ID is
// configuration (GRAM_STAFF_ORGANIZATION_ID), never a constant.
package staff

// Organization is the deployment's Speakeasy staff organization. Its members
// are Speakeasy staff: customer audit feeds mask them, and the organization
// itself bypasses standard OpenRouter credit limits, gets new hooks releases
// first, and may set organization whitelist status.
//
// The zero value is an unconfigured staff organization, which matches no
// organization, so every staff behavior is off.
type Organization struct {
	// id is the staff organization's ID, empty when unconfigured.
	id string
}

// NewOrganization returns the staff organization with the given ID. An empty
// ID returns the unconfigured staff organization.
func NewOrganization(id string) Organization {
	return Organization{id: id}
}

// ID returns the staff organization's ID, empty when unconfigured.
func (o Organization) ID() string {
	return o.id
}

// Configured reports whether the deployment has a staff organization.
func (o Organization) Configured() bool {
	return o.id != ""
}

// Is reports whether organizationID is the staff organization. It is false
// for every ID when the staff organization is unconfigured, including an
// empty organizationID.
func (o Organization) Is(organizationID string) bool {
	return o.Configured() && organizationID == o.id
}
