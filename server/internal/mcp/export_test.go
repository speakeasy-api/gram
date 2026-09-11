package mcp

import (
	"context"
	"time"

	"github.com/speakeasy-api/gram/server/internal/ratelimit"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
)

// VerifyRemoteGrant exposes the grant hook to tests.
func (s *Service) VerifyRemoteGrant(ctx context.Context, grant remotesessions.RemoteGrant) time.Time {
	return s.verifyRemoteGrant(ctx, grant)
}

// VerifyRemoteGrantOn runs the probe step against an endpoint the test resolved itself.
// It preserves ctx, including its deadline; unlike VerifyRemoteGrant it does not
// start detached work with a fresh callback budget.
func (s *Service) VerifyRemoteGrantOn(ctx context.Context, endpoint *ResolvedMcpEndpoint, grant remotesessions.RemoteGrant) {
	challengeState, err := s.authnChallengeCache.Get(ctx, "authnChallenge:"+grant.ParentChallengeID)
	if err != nil {
		return
	}
	s.probeRemoteGrant(ctx, s.logger, endpoint, challengeState, grant)
}

// RemoteChallengeManager is the manager the service registered its grant hook on.
func (s *Service) RemoteChallengeManager() *remotesessions.ChallengeManager {
	return s.remoteChallengeMgr
}

// SetRemoteSessionRecheckPacing swaps the sweep's per-host limiter rate and claim batch for a test.
func (s *Service) SetRemoteSessionRecheckPacing(rate ratelimit.Rate, batch int32) {
	r := s.remoteSessionRecheck
	r.batch = batch
	if r.limiterStore != nil {
		r.limiter = ratelimit.New(r.limiterStore, "remote_session_recheck_host", rate)
	}
}

// RemoteSessionRecheckLease is the claim lease the sweep applies for interval.
func RemoteSessionRecheckLease(interval time.Duration) time.Duration {
	return remotesessions.RecheckLease(interval)
}
