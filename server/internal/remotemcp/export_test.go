package remotemcp

import "time"

// SetBeforeClaim runs f between the claim's list and re-read, inside the locked update transaction.
func (s *Service) SetBeforeClaim(f func(holderPID uint32, previousURL string)) { s.beforeClaim = f }

// SetAfterChallengeScopes runs fn after a challenge-scope observation is written or debounced.
func (f *ProxyManager) SetAfterChallengeScopes(fn func()) { f.afterChallengeScopes = fn }

// SetBeforeProtectedResourceProbe runs fn synchronously when detached work is scheduled.
func (f *ProxyManager) SetBeforeProtectedResourceProbe(fn func()) {
	f.beforeProtectedResourceProbe = fn
}

// SetAfterProtectedResourceProbe runs fn after a detached on-use probe finishes.
func (f *ProxyManager) SetAfterProtectedResourceProbe(fn func()) { f.afterProtectedResourceProbe = fn }

// SetProtectedResourceProbeClock replaces the clock the on-use probe reads.
func (f *ProxyManager) SetProtectedResourceProbeClock(now func() time.Time) {
	f.protectedResourceProbes.now = now
}
