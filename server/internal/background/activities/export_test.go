package activities

import "github.com/speakeasy-api/gram/server/internal/organizations/orgmetrics"

// WaitForDeliveries blocks until revocation pushes started in the background
// finish.
func (p *ProcessWorkOSOrganizationEvents) WaitForDeliveries() { p.deliverer.Wait() }

// SetOrganizationsCreated replaces the organizations created counter, so a
// test can read what event processing recorded.
func (p *ProcessWorkOSOrganizationEvents) SetOrganizationsCreated(m *orgmetrics.Created) {
	p.orgsCreated = m
}
