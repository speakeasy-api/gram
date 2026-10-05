package remotemcp

// SetBeforeClaim runs f between the claim's list and re-read, inside the locked update transaction.
func (s *Service) SetBeforeClaim(f func(holderPID uint32, previousURL string)) { s.beforeClaim = f }

// SetAfterChallengeScopes runs fn after a challenge-scope observation is written or debounced.
func (f *ProxyManager) SetAfterChallengeScopes(fn func()) { f.afterChallengeScopes = fn }
