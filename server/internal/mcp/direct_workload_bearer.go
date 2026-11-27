package mcp

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/google/uuid"
	"github.com/hashicorp/golang-lru/v2/simplelru"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/agents/runtimepolicy"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth/deviceagenttoken"
	"github.com/speakeasy-api/gram/server/internal/auth/principalcredential"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/orgserving"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	assertioncore "github.com/speakeasy-api/gram/server/internal/usersessions/assertion"
	"github.com/speakeasy-api/gram/server/internal/usersessions/assertion/workload"
	"github.com/speakeasy-api/gram/server/internal/usersessions/jwks"
)

const (
	// DirectWorkloadProjectGatewayAudience is the exact JWT audience a
	// direct-bearer workload JWT must carry to authenticate at a Project Gateway.
	DirectWorkloadProjectGatewayAudience = "urn:speakeasy:mcp:project-gateway"

	// DirectWorkloadInferenceAudience is the exact JWT audience a direct-bearer
	// workload JWT must carry to authenticate at the project-scoped inference
	// endpoint.
	DirectWorkloadInferenceAudience = "urn:speakeasy:inference:workload"
)

const (
	// directWorkloadCacheTTL bounds how long a verified direct-bearer workload
	// JWT's Stage-1 result (issuer trust, signature, reuse policy, and subject
	// admission) is reused before re-checking Postgres and the verifier. Matches
	// sessionLivenessTTL so admission withdrawal or reuse-policy disable takes
	// effect within 5 s, while live agent/owner/grant checks still run per
	// request.
	directWorkloadCacheTTL = 5 * time.Second

	// directWorkloadCacheSize caps per-process Stage-1 cache entries, matching
	// sessionLivenessMemoSize.
	directWorkloadCacheSize = 10_000
)

// errDirectWorkloadUnavailable marks a direct-bearer workload verification that
// could not reach a decision because a dependency (database, Redis, or JWKS)
// was unavailable.
var errDirectWorkloadUnavailable = errors.New("direct workload bearer verification unavailable")

// errDirectWorkloadRateLimited marks a direct-bearer workload verification that
// was refused by the issuer or device-key lookup rate limiter.
var errDirectWorkloadRateLimited = errors.New("direct workload bearer rate limited")

// verifiedDirectWorkload is the verified Stage-1 outcome of a direct-bearer
// workload JWT: the trusted issuer row, exact admitted external subject, token
// expiration, and token digest identifier.
type verifiedDirectWorkload struct {
	issuerID  uuid.UUID
	subject   string
	expiresAt time.Time
	tokenID   string
}

type directWorkloadCacheKey struct {
	tokenHash      [sha256.Size]byte
	organizationID string
	projectID      uuid.UUID
	audience       string
}

type cachedDirectWorkload struct {
	verified    verifiedDirectWorkload
	cachedUntil time.Time
}

// directWorkloadCache memoizes Stage-1 direct-bearer workload verification
// (issuer resolution, signature verification, reuse opt-in, and subject
// admission) for at most directWorkloadCacheTTL, bounded by token expiry.
// Stage-2 authorization (agent assignment, lifecycle, owner eligibility, and
// live grants) is never cached. Expiration is evaluated strictly against now
// so tests control the clock without wall-clock races.
type directWorkloadCache struct {
	mu      sync.Mutex
	entries *simplelru.LRU[directWorkloadCacheKey, cachedDirectWorkload]
	now     func() time.Time
}

func newDirectWorkloadCache(now func() time.Time) *directWorkloadCache {
	if now == nil {
		now = time.Now
	}
	entries, err := simplelru.NewLRU[directWorkloadCacheKey, cachedDirectWorkload](directWorkloadCacheSize, nil)
	if err != nil {
		panic(fmt.Sprintf("new direct workload cache: %v", err))
	}
	return &directWorkloadCache{
		mu:      sync.Mutex{},
		entries: entries,
		now:     now,
	}
}

func (c *directWorkloadCache) setClock(now func() time.Time) {
	if now == nil {
		now = time.Now
	}
	c.mu.Lock()
	c.now = now
	c.mu.Unlock()
}

func (c *directWorkloadCache) get(key directWorkloadCacheKey) (verifiedDirectWorkload, bool) {
	var zero verifiedDirectWorkload
	if c == nil || c.entries == nil {
		return zero, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	entry, ok := c.entries.Get(key)
	if !ok {
		return zero, false
	}
	now := c.now()
	if !now.Before(entry.cachedUntil) || !now.Before(entry.verified.expiresAt) {
		c.entries.Remove(key)
		return zero, false
	}
	return entry.verified, true
}

func (c *directWorkloadCache) put(key directWorkloadCacheKey, verified verifiedDirectWorkload) {
	if c == nil || c.entries == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	now := c.now()
	if !now.Before(verified.expiresAt) {
		return
	}
	cachedUntil := now.Add(directWorkloadCacheTTL)
	if verified.expiresAt.Before(cachedUntil) {
		cachedUntil = verified.expiresAt
	}
	c.entries.Add(key, cachedDirectWorkload{
		verified:    verified,
		cachedUntil: cachedUntil,
	})
}

// isDirectWorkloadBearer reports whether raw is an asymmetric compact JWS
// routed to the direct-bearer workload authenticator. Every Speakeasy-minted
// user-session, chat-session, and assistant token uses HS256, API keys are
// opaque, and principal credentials are excluded by their typ header, so this
// predicate partitions bearer credentials without overlap.
func isDirectWorkloadBearer(raw string) bool {
	if raw == "" || principalcredential.IsToken(raw) {
		return false
	}
	_, err := assertioncore.ParseSigned(raw, workloadAssertionMaxBytes)
	return err == nil
}

// authenticateDirectWorkloadBearer runs Stage-1 verification for a workload JWT
// presented directly as an Authorization bearer:
//  1. Reject non-bearer / Speakeasy-minted asymmetric token types and require
//     exact membership of expectedAudience (multiple aud values are permitted).
//  2. Consult the short-lived Stage-1 cache keyed by token digest, tenancy, and
//     expected audience.
//  3. Resolve the issuer within tenancy and require allow_assertion_reuse=true
//     so single-use issuers can never be used as reusable direct bearers.
//  4. Verify signature, issuer, subject, audience, lifetime, and expiry via
//     workload.Verifier.Verify.
//  5. Check tenant-scoped subject admission via workloadidentity.IsAdmitted.
func (s *Service) authenticateDirectWorkloadBearer(
	ctx context.Context,
	tenancy workloadTenancy,
	expectedAudience string,
	raw string,
) (verifiedDirectWorkload, error) {
	if s.workloadGrant == nil {
		return verifiedDirectWorkload{}, refuseWorkloadGrant("workload_grant_unconfigured", errors.New("workload assertion grant is not configured"))
	}
	if principalcredential.IsToken(raw) || deviceagenttoken.IsToken(raw) {
		return verifiedDirectWorkload{}, refuseWorkloadGrant(string(workload.ReasonTypeNotBearer), errors.New("speakeasy asymmetric token is not a workload assertion"))
	}
	token, err := assertioncore.ParseSigned(raw, workloadAssertionMaxBytes)
	if err != nil {
		return verifiedDirectWorkload{}, refuseWorkloadGrant(string(workload.ReasonMalformed), err)
	}
	var claims jwt.Claims
	if err := token.UnsafeClaimsWithoutVerification(&claims); err != nil {
		return verifiedDirectWorkload{}, refuseWorkloadGrant(string(workload.ReasonMalformed), err)
	}
	switch {
	case claims.Issuer == "":
		return verifiedDirectWorkload{}, refuseWorkloadGrant("issuer_untrusted", errors.New("iss is required"))
	case claims.Subject == "" || claims.Subject == claims.Issuer:
		return verifiedDirectWorkload{}, refuseWorkloadGrant("assertion_subject_invalid", errors.New("sub is empty or equals iss"))
	case !slices.Contains(claims.Audience, expectedAudience):
		return verifiedDirectWorkload{}, refuseWorkloadGrant(string(workload.ReasonAudienceMismatch), errors.New("aud does not include expected endpoint audience"))
	}

	tokenHash := sha256.Sum256([]byte(raw))
	cacheKey := directWorkloadCacheKey{
		tokenHash:      tokenHash,
		organizationID: tenancy.OrganizationID,
		projectID:      tenancy.ProjectID,
		audience:       expectedAudience,
	}
	if cached, ok := s.directWorkloadCache.get(cacheKey); ok {
		return cached, nil
	}

	issuer, err := s.workloadGrant.issuers.admit(ctx, tenancy, claims.Issuer)
	switch {
	case errors.Is(err, errWorkloadIssuerUntrusted):
		return verifiedDirectWorkload{}, refuseWorkloadGrant("issuer_untrusted", err)
	case errors.Is(err, errWorkloadIssuerLookupRateLimited):
		refusal := &workloadGrantError{reason: "issuer_lookup_rate_limited", outcome: workloadGrantRateLimited, retryAfter: 0, err: err}
		if limited, ok := errors.AsType[*workloadIssuerRateLimitedError](err); ok {
			refusal.retryAfter = limited.retryAfter
		}
		return verifiedDirectWorkload{}, refusal
	case err != nil:
		return verifiedDirectWorkload{}, workloadGrantStageUnavailable("issuer_lookup_unavailable", err)
	}
	if !issuer.AllowAssertionReuse.Valid || !issuer.AllowAssertionReuse.Bool {
		return verifiedDirectWorkload{}, refuseWorkloadGrant("assertion_reuse_disabled", errors.New("workload issuer does not allow assertion reuse"))
	}

	keyID := ""
	if len(token.Headers) == 1 {
		keyID = token.Headers[0].KeyID
	}
	source, err := s.workloadGrant.assertionKeySource(ctx, tenancy, &issuer, claims.Subject, keyID)
	if err != nil {
		return verifiedDirectWorkload{}, err
	}

	result, err := s.workloadGrant.verifier.Verify(ctx, raw, workload.Expectation{
		Issuer:              issuer.Issuer,
		Subject:             claims.Subject,
		KeySource:           source,
		ReplayIssuer:        tenancy.UserSessionIssuerID.String(),
		ReplayParty:         issuer.ID.String(),
		Audiences:           []string{expectedAudience},
		MaxLifetime:         workloadAssertionMaxLifetime,
		AllowAssertionReuse: true,
	})
	if err != nil {
		reason := workload.ReasonOf(err)
		if errors.Is(err, jwks.ErrRefreshRateLimited) || errors.Is(err, jwks.ErrFetchRateLimited) {
			return verifiedDirectWorkload{}, workloadGrantStageUnavailable("issuer_keys_rate_limited", err)
		}
		switch reason {
		case workload.ReasonKeyUnresolvable, workload.ReasonReplayStoreUnavailable:
			return verifiedDirectWorkload{}, workloadGrantStageUnavailable(string(reason), err)
		case workload.ReasonVerifierMisconfigured, "":
			return verifiedDirectWorkload{}, fmt.Errorf("verify direct workload bearer: %w", err)
		default:
			return verifiedDirectWorkload{}, refuseWorkloadGrant(string(reason), err)
		}
	}

	err = admitWorkloadIdentity(ctx, s.workloadGrant.identities, tenancy, issuer.ID, claims.Subject)
	switch {
	case errors.Is(err, errWorkloadNotAdmitted):
		return verifiedDirectWorkload{}, refuseWorkloadGrant("subject_not_admitted", err)
	case err != nil:
		return verifiedDirectWorkload{}, workloadGrantStageUnavailable("subject_admission_unavailable", err)
	}

	subject := urn.NewWorkloadSubject(issuer.ID, claims.Subject)
	if _, _, err := subject.Workload(); err != nil {
		return verifiedDirectWorkload{}, refuseWorkloadGrant("assertion_subject_invalid", err)
	}

	verified := verifiedDirectWorkload{
		issuerID:  issuer.ID,
		subject:   claims.Subject,
		expiresAt: result.ExpiresAt,
		tokenID:   base64.RawURLEncoding.EncodeToString(tokenHash[:]),
	}
	s.directWorkloadCache.put(cacheKey, verified)
	return verified, nil
}

// admitDirectWorkload runs Stage-2 live workload authorization (agent
// assignment, lifecycle, owner eligibility, and live R/A/O policy loading) for
// a verified direct-bearer workload JWT, yielding the same actor and auth
// context shape as a jwt-bearer exchanged workload session.
func (s *Service) admitDirectWorkload(
	ctx context.Context,
	organizationID string,
	projectID uuid.UUID,
	projectSlug *string,
	verified verifiedDirectWorkload,
	credential workloadSessionCredential,
) (context.Context, urn.SessionSubject, error) {
	subject := urn.NewWorkloadSubject(verified.issuerID, verified.subject)
	sessionID := "workload-bearer:" + verified.tokenID
	authCtx, err := s.sessionAuthContext(ctx, organizationID, &projectID, sessionID)
	if err != nil {
		return ctx, urn.SessionSubject{}, err
	}
	authCtx.ProjectSlug = projectSlug

	ctx = contextvalues.WithAuthenticatedActor(ctx, authCtx, urn.NewWorkloadPrincipal(verified.issuerID, verified.subject))
	ctx, err = s.admitWorkloadSession(ctx, organizationID, subject, credential)
	if err != nil {
		return ctx, urn.SessionSubject{}, err
	}
	return ctx, subject, nil
}

// authenticateIssuerGateDirectWorkload authenticates a direct-bearer workload
// JWT at an issuer-gated MCP endpoint. Only Project Gateways accept direct
// workload bearers; every other MCP endpoint refuses them without falling
// through to another credential strategy.
func (s *Service) authenticateIssuerGateDirectWorkload(
	ctx context.Context,
	authToken string,
	endpoint *ResolvedMcpEndpoint,
) (context.Context, *urn.SessionSubject, error) {
	if !endpoint.IsProjectGateway() {
		return ctx, nil, fmt.Errorf("%w: direct workload bearer tokens are accepted at project gateways only", errCredentialRejected)
	}

	gateway, err := gatewayTargetFromEndpoint(endpoint)
	if err != nil {
		return ctx, nil, fmt.Errorf("%w: %w", errCredentialRejected, err)
	}
	target, ok := gateway.authorizationTarget(endpoint.ProjectID)
	if !ok {
		return ctx, nil, fmt.Errorf("%w: gateway cannot carry a workload session policy", errCredentialRejected)
	}

	verified, err := s.authenticateDirectWorkloadBearer(ctx, endpoint.workloadTenancy(), DirectWorkloadProjectGatewayAudience, authToken)
	if err != nil {
		if refusal, ok := errors.AsType[*workloadGrantError](err); ok {
			switch refusal.outcome {
			case workloadGrantRateLimited:
				return ctx, nil, fmt.Errorf("%w: %w", errDirectWorkloadRateLimited, err)
			case workloadGrantUnavailable:
				return ctx, nil, fmt.Errorf("%w: %w", errDirectWorkloadUnavailable, err)
			case workloadGrantRefused:
				return ctx, nil, fmt.Errorf("%w: %w", errCredentialRejected, err)
			}
		}
		return ctx, nil, fmt.Errorf("authenticate direct workload bearer: %w", err)
	}

	version := runtimepolicy.IssuedDelegatedPolicyVersion
	delegatedGrants, err := encodeAgentSessionPolicy(*target, version)
	if err != nil {
		return ctx, nil, fmt.Errorf("encode direct workload gateway policy: %w", err)
	}
	credential := workloadSessionCredential{
		DerivesPolicyAtAdmission: false,
		DelegatedGrants:          delegatedGrants,
		DelegatedGrantsVersion:   int32(version),
	}

	newCtx, subject, err := s.admitDirectWorkload(ctx, endpoint.OrganizationID, endpoint.ProjectID, nil, verified, credential)
	if err != nil {
		return ctx, nil, err
	}
	newCtx, err = s.requireWorkloadSessionAuthorization(newCtx, endpoint, workloadSessionReachResource)
	if err != nil {
		return ctx, nil, err
	}
	newCtx = withWorkloadSessionReach(newCtx, workloadSessionReachResource)

	agentID, ok := authz.AdmittedWorkloadAgent(newCtx)
	if !ok {
		return ctx, nil, fmt.Errorf("%w: workload admission has no agent principal", errCredentialRejected)
	}
	newCtx = s.identityValidator.StampDirectWorkload(newCtx, agentID, verified.expiresAt)
	return newCtx, &subject, nil
}

// authorizeDirectWorkloadInference authenticates a direct-bearer workload JWT
// on the project-scoped inference endpoint. Unscoped /chat/completions passes
// uuid.Nil and always rejects direct workload bearers without falling through.
func (s *Service) authorizeDirectWorkloadInference(
	ctx context.Context,
	token string,
	projectID uuid.UUID,
) (context.Context, bool, error) {
	if projectID == uuid.Nil {
		return ctx, true, oops.E(oops.CodeUnauthorized, nil, "invalid inference credential")
	}

	project, err := projectsrepo.New(s.db).GetProjectByID(ctx, projectID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ctx, true, oops.C(oops.CodeUnauthorized)
	}
	if err != nil {
		return ctx, true, fmt.Errorf("authorize workload inference: %w", err)
	}

	// Project-scoped inference has no user_session_issuer in the route; key
	// per-tenant rate-limit and replay scopes by the resolved project UUID.
	tenancy := workloadTenancy{
		OrganizationID:      project.OrganizationID,
		ProjectID:           project.ID,
		UserSessionIssuerID: project.ID,
	}
	verified, err := s.authenticateDirectWorkloadBearer(ctx, tenancy, DirectWorkloadInferenceAudience, token)
	if err != nil {
		if refusal, ok := errors.AsType[*workloadGrantError](err); ok {
			switch refusal.outcome {
			case workloadGrantRateLimited:
				return ctx, true, oops.E(oops.CodeRateLimitExceeded, err, "too many requests; retry later")
			case workloadGrantUnavailable:
				return ctx, true, oops.E(oops.CodeUnavailable, err, "inference credential validation unavailable")
			case workloadGrantRefused:
				return ctx, true, oops.E(oops.CodeUnauthorized, err, "invalid inference credential")
			}
		}
		return ctx, true, fmt.Errorf("authorize workload inference: %w", err)
	}

	organization, err := s.orgsRepo.GetOrganizationMetadata(ctx, project.OrganizationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ctx, true, oops.C(oops.CodeUnauthorized)
	}
	if err != nil {
		return ctx, true, fmt.Errorf("authorize workload inference: %w", err)
	}
	if refusal := orgserving.Check(organization); refusal != orgserving.RefusalNone {
		s.logger.DebugContext(ctx, "workload inference refused for unserved organization",
			attr.SlogOrganizationID(project.OrganizationID),
			attr.SlogReason(string(refusal)),
		)
		return ctx, true, oops.E(oops.CodeUnauthorized, nil, "invalid inference credential")
	}

	version := runtimepolicy.InferenceIssuedPolicyVersion
	grants, err := encodeInferenceWorkloadPolicy(project.ID, version)
	if err != nil {
		return ctx, true, fmt.Errorf("authorize workload inference: %w", err)
	}
	credential := workloadSessionCredential{
		DerivesPolicyAtAdmission: false,
		DelegatedGrants:          grants,
		DelegatedGrantsVersion:   int32(version),
	}

	ctx, _, err = s.admitDirectWorkload(ctx, project.OrganizationID, project.ID, &project.Slug, verified, credential)
	if err != nil {
		return ctx, true, fmt.Errorf("authorize workload inference: %w", err)
	}
	if err := s.authz.Require(ctx, authz.InferenceExecuteCheck(project.ID.String())); err != nil {
		return ctx, true, fmt.Errorf("authorize workload inference: %w", err)
	}
	return ctx, true, nil
}
