package identity

import "github.com/speakeasy-api/gram/server/internal/organizations/orgmetrics"

// SetOrganizationsCreated replaces the organizations created counter, so a
// test can read what membership sync recorded.
func (r *Resolver) SetOrganizationsCreated(m *orgmetrics.Created) { r.orgsCreated = m }
