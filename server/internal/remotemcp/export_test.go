package remotemcp

// SetBeforeClaim runs f between the claim's list and re-read, inside the locked update transaction.
func (s *Service) SetBeforeClaim(f func(previousURL string)) { s.beforeClaim = f }
