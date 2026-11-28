package auth

import "github.com/speakeasy-api/gram/server/internal/organizations/orgmetrics"

// WaitForDeliveries blocks until revocation pushes started in the background
// finish.
func (s *Service) WaitForDeliveries() { s.deliverer.Wait() }

// SetOrganizationsCreated replaces the organizations created counter, so a
// test can read what provisioning recorded.
func (s *Service) SetOrganizationsCreated(m *orgmetrics.Created) { s.orgsCreated = m }
