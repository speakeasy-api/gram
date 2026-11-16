package auth

// WaitForDeliveries blocks until revocation pushes started in the background
// finish.
func (s *Service) WaitForDeliveries() { s.deliverer.Wait() }
