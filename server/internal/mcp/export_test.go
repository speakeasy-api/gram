package mcp

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/ratelimit"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/usersessions/clientauth"
	"github.com/speakeasy-api/gram/server/internal/usersessions/jwks"
	"github.com/speakeasy-api/gram/server/internal/workloadidentity"
	workloadidentity_repo "github.com/speakeasy-api/gram/server/internal/workloadidentity/repo"
)

// ErrWorkloadIssuerUntrusted and ErrWorkloadNotAdmitted expose the workload
// admission sentinels, so a test can tell which stage refused an assertion.
var (
	ErrWorkloadIssuerUntrusted = errWorkloadIssuerUntrusted
	ErrWorkloadNotAdmitted     = errWorkloadNotAdmitted
)

// AdmitWorkloadAssertion runs the workload grant's stages in token endpoint
// order against real rows: resolve iss to a workload issuer in the endpoint's
// tenancy, verify against that issuer's key set, then check admission.
//
// iss and sub are read unverified to find the issuer; Verify then requires both
// to match. The read uses the verifier's algorithm allowlist.
func AdmitWorkloadAssertion(
	ctx context.Context,
	db *pgxpool.Pool,
	verifier *clientauth.Verifier,
	endpoint *ResolvedMcpEndpoint,
	audiences clientauth.Audiences,
	raw string,
) error {
	parsed, err := jwt.ParseSigned(raw, jwks.AllowedSignatureAlgorithms())
	if err != nil {
		return fmt.Errorf("parse workload assertion: %w", err)
	}
	var claims jwt.Claims
	if err := parsed.UnsafeClaimsWithoutVerification(&claims); err != nil {
		return fmt.Errorf("read workload assertion claims: %w", err)
	}

	projectID := uuid.NullUUID{UUID: endpoint.ProjectID, Valid: endpoint.ProjectID != uuid.Nil}

	admission := newWorkloadIssuerAdmission(
		func(ctx context.Context, endpoint *ResolvedMcpEndpoint, issuerURL string) (workloadidentity_repo.WorkloadIssuer, bool, error) {
			issuer, err := workloadidentity.ResolveIssuerByURL(ctx, db, workloadidentity.ResolveIssuerParams{
				OrganizationID: endpoint.OrganizationID,
				ProjectID:      projectID,
				IssuerURL:      issuerURL,
			})
			switch {
			case errors.Is(err, workloadidentity.ErrIssuerNotFound):
				return workloadidentity_repo.WorkloadIssuer{}, false, nil
			case err != nil:
				return workloadidentity_repo.WorkloadIssuer{}, false, fmt.Errorf("resolve workload issuer by url: %w", err)
			}
			return issuer, true, nil
		},
		allowAllWorkloadLookups,
	)

	issuer, err := admission.admit(ctx, endpoint, claims.Issuer)
	if err != nil {
		return err
	}

	source, err := workloadIssuerKeySource(endpoint, &issuer)
	if err != nil {
		return err
	}

	expectation := clientauth.WorkloadExpectation(
		issuer.Issuer,
		claims.Subject,
		source,
		endpoint.UserSessionIssuerID.String(),
		issuer.ID.String(),
		claims.Subject,
		audiences,
		clientauth.DefaultMaxLifetime,
	)
	if _, err := verifier.Verify(ctx, clientauth.Assertion{Value: raw, Type: clientauth.AssertionType}, expectation); err != nil {
		return fmt.Errorf("verify workload assertion: %w", err)
	}

	return admitWorkloadIdentity(ctx, func(ctx context.Context, identity workloadIdentity) (bool, error) {
		admitted, err := workloadidentity.IsAdmitted(ctx, db, workloadidentity.AdmissionParams{
			OrganizationID:   identity.OrganizationID,
			ProjectID:        identity.ProjectID,
			WorkloadIssuerID: identity.WorkloadIssuerID,
			Subject:          identity.ExternalSubject,
		})
		if err != nil {
			return false, fmt.Errorf("check workload admission: %w", err)
		}
		return admitted, nil
	}, endpoint, issuer.ID, claims.Subject)
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

// SetRemoteSessionRecheckPacing swaps the sweep's per-host limiter rate and claim batch for a test.
func (s *Service) SetRemoteSessionRecheckPacing(rate ratelimit.Rate, batch int32) {
	r := s.remoteSessionRecheck
	r.batch = batch
	if r.limiterStore != nil {
		r.limiter = ratelimit.New(r.limiterStore, "remote_session_recheck_host", rate)
	}
}
