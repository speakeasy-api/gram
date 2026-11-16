package activities

// WaitForDeliveries blocks until revocation pushes started in the background
// finish.
func (p *ProcessWorkOSOrganizationEvents) WaitForDeliveries() { p.deliverer.Wait() }
