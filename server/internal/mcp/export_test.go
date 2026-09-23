package mcp

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/agent/aitargets"
	agentrepo "github.com/speakeasy-api/gram/server/internal/agent/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpriskscan"
	"github.com/speakeasy-api/gram/server/internal/ratelimit"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/usersessions/assertion/workload"
)

// ErrWorkloadIssuerUntrusted and ErrWorkloadNotAdmitted expose the workload
// admission sentinels, so a test can tell which stage refused an assertion.
var (
	ErrWorkloadIssuerUntrusted = errWorkloadIssuerUntrusted
	ErrWorkloadNotAdmitted     = errWorkloadNotAdmitted
)

// AdmitWorkloadAssertion runs the workload grant's verification stages
// against real rows with the given verifier and an unlimited issuer lookup
// budget.
func AdmitWorkloadAssertion(
	ctx context.Context,
	db *pgxpool.Pool,
	verifier *workload.Verifier,
	endpoint *ResolvedMcpEndpoint,
	audiences []string,
	raw string,
) error {
	grant := &workloadGrant{
		issuers:    newWorkloadIssuerAdmission(workloadIssuerStoreLookup(db), allowAllWorkloadLookups),
		identities: workloadIdentityStoreLookup(db),
		verifier:   verifier,
	}
	_, err := admitWorkloadAssertion(ctx, grant, endpoint, audiences, raw)
	return err
}

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

// FailAIToolBlockedIDsRead makes the "does this organization block anything?"
// read behind the gateway block check fail with err.
func (s *Service) FailAIToolBlockedIDsRead(err error) {
	s.aiToolBlockReads.blockedTargetIDs = func(context.Context, *agentrepo.Queries, string) ([]string, error) {
		return nil, err
	}
}

// FailAIToolCatalogRead makes the scan-target catalog read behind the gateway
// block check fail with err, leaving the blocked-ids read intact.
func (s *Service) FailAIToolCatalogRead(err error) {
	s.aiToolBlockReads.catalog = func(context.Context, *agentrepo.Queries, string) (*aitargets.OrganizationList, error) {
		return nil, err
	}
}

// SetRemoteSessionRecheckPacing swaps the sweep's per-host limiter rate and claim batch for a test.
func (s *Service) SetRemoteSessionRecheckPacing(rate ratelimit.Rate, batch int32) {
	r := s.remoteSessionRecheck
	r.batch = batch
	if r.limiterStore != nil {
		r.limiter = ratelimit.New(r.limiterStore, "remote_session_recheck_host", rate)
	}
}

// SetRiskScanEvaluator replaces observation only in the test binary.
func (s *Service) SetRiskScanEvaluator(evaluator *mcpriskscan.Evaluator) {
	s.scanEvaluator = evaluator
}
