package remotesessions

import (
	"context"
	"encoding/json"
	"time"

	"github.com/speakeasy-api/gram/server/internal/remotesessions/remotesessionmetrics"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"

	"github.com/google/uuid"
)

// WaitIdentityRestatements blocks until every detached identity restatement has finished.
func (s *RefreshService) WaitIdentityRestatements() { s.restatements.Wait() }

func (m *ChallengeManager) WaitIdentityRestatements() { m.refresher.WaitIdentityRestatements() }

// MaxEnrichmentBytes exposes the enrichment cap to e2e tests.
const MaxEnrichmentBytes = maxEnrichmentBytes

// PlanIssuerMetadataRefresh exposes the on-use decision to tests.
func PlanIssuerMetadataRefresh(use IssuerMetadataUse, now time.Time) (reproject, fetch bool) {
	plan := planIssuerMetadataRefresh(use, now)
	return plan.reproject, plan.fetch
}

// IssuerMetadataUseFromRow builds the flow-time view of a stored row the way the flow queries do.
func IssuerMetadataUseFromRow(row repo.RemoteSessionIssuer) IssuerMetadataUse {
	return issuerMetadataUseFromRow(row)
}

// SetBeforeAdmit runs f before every use is admitted; tests hold a producer there to race Shutdown.
func (r *IssuerMetadataRefresher) SetBeforeAdmit(f func()) { r.beforeAdmit = f }

// Reproject exposes the scoped operation to integration tests.
func (r *IssuerMetadataRefresher) Reproject(ctx context.Context, candidate IssuerMetadataRefreshCandidate) (remotesessionmetrics.IssuerMetadataRefreshOutcome, error) {
	existing, outcome, err := r.load(ctx, candidate)
	if err != nil || outcome != "" {
		return r.record(ctx, candidate.IssuerURL, outcome), err
	}
	return r.reproject(ctx, existing)
}

// Refresh exposes the scoped operation to integration tests.
func (r *IssuerMetadataRefresher) Refresh(ctx context.Context, candidate IssuerMetadataRefreshCandidate) (remotesessionmetrics.IssuerMetadataRefreshOutcome, error) {
	existing, outcome, err := r.load(ctx, candidate)
	if err != nil || outcome != "" {
		return r.record(ctx, candidate.IssuerURL, outcome), err
	}
	return r.refresh(ctx, existing)
}

// SetIssuerMetadataRefreshSeam replaces the AIM-260 seam so a test can observe the refresh request a 404 makes.
func (e *SessionEnricher) SetIssuerMetadataRefreshSeam(fn func(context.Context, uuid.UUID)) {
	e.requestIssuerMetadataRefresh = fn
}

// JWTAccessTokenTarget exposes the inputs to local JWT access-token enrichment to external tests.
type JWTAccessTokenTarget struct {
	IssuerID         uuid.UUID
	IssuerURL        string
	JWKSURI          string
	ExternalClientID string
	Resource         string

	// ResourceIndicatorUnsupported is the issuer's explicit resource_indicator_supported=false.
	ResourceIndicatorUnsupported bool
}

// JWTAccessTokenResult exposes the security-relevant local verification result to external tests.
type JWTAccessTokenResult struct {
	Ran          bool
	Status       string
	Reason       string
	Subject      string
	Email        string
	DisplayName  string
	Source       string
	Scopes       []string
	ScopePresent bool

	// Claims are the retained members an adopted identity carries.
	Claims map[string]json.RawMessage
}

// JWTAccessToken runs local access-token verification through the production implementation.
func (e *SessionEnricher) JWTAccessToken(ctx context.Context, target JWTAccessTokenTarget, raw string) JWTAccessTokenResult {
	result := e.jwtAccessToken(ctx, enrichmentTarget{
		issuerID:                     target.IssuerID,
		issuerURL:                    target.IssuerURL,
		jwksURI:                      target.JWKSURI,
		externalClientID:             target.ExternalClientID,
		resource:                     target.Resource,
		resourceIndicatorUnsupported: target.ResourceIndicatorUnsupported,
	}, raw)
	out := JWTAccessTokenResult{
		Ran: result.ran, Status: result.Status, Reason: result.Reason,
		Scopes: result.scopes, ScopePresent: result.scopePresent,
	}
	if result.identity != nil {
		out.Subject = result.identity.Subject
		out.Email = result.identity.Email
		out.DisplayName = result.identity.DisplayName
		out.Source = result.identity.Source
		out.Claims = result.identity.Claims
	}
	return out
}
