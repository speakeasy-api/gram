// The workload assertion grant: a workload presents the identity token its
// platform issued and receives a resource-scoped Speakeasy session (RFC 7523 §2.1).
// It holds no client registration and receives no refresh token; when the
// session lapses it presents a fresh platform token.

package mcp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel/metric"

	"github.com/speakeasy-api/gram/server/internal/agents/runtimepolicy"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oautherr"
	"github.com/speakeasy-api/gram/server/internal/oauthwire"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/ratelimit"
	"github.com/speakeasy-api/gram/server/internal/urn"
	assertioncore "github.com/speakeasy-api/gram/server/internal/usersessions/assertion"
	"github.com/speakeasy-api/gram/server/internal/usersessions/assertion/workload"
	"github.com/speakeasy-api/gram/server/internal/usersessions/jwks"
	"github.com/speakeasy-api/gram/server/internal/usersessions/replay"
	usersessions_repo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
	"github.com/speakeasy-api/gram/server/internal/workloadidentity"
	workloadidentity_repo "github.com/speakeasy-api/gram/server/internal/workloadidentity/repo"
)

// workloadAssertionMaxLifetime bounds how far ahead an accepted assertion's
// exp may lie. Platform identity tokens are short-lived, and the replay guard
// holds each identifier for this long, so the bound is kept tight.
const workloadAssertionMaxLifetime = time.Hour

// workloadAssertionMaxBytes bounds the assertion read before verification,
// matching the verifier's own limit.
const workloadAssertionMaxBytes = 8 * 1024

// workloadSessionLifetime is how long a workload session stays authorized. It
// sits inside the 300 s to 86,400 s range clients such as Claude Tag cache, and
// bounds how often each workload exchanges.
const workloadSessionLifetime = 15 * time.Minute

// workloadGrantUnavailableRetryAfter is the Retry-After sent when a stage the
// grant depends on is unavailable rather than refusing.
const workloadGrantUnavailableRetryAfter = 5 * time.Second

var (
	// Key refreshes are rare rotations; the fleet-wide budget lets each
	// replica converge while bounding random-kid probes.
	workloadKeyRefreshRate = ratelimit.PerMinute(10)

	// Cold and forced key fetches share one budget per authorization server.
	workloadKeyFetchRate = ratelimit.PerMinute(30)
)

// workloadGrant holds the stages of the workload assertion grant.
type workloadGrant struct {
	issuers    *workloadIssuerAdmission
	identities workloadIdentityLookup
	verifier   *workload.Verifier
}

// newWorkloadGrant assembles the grant over the database and the shared Redis
// client, or returns nil when there is no Redis. The replay guard cannot
// promise single use without a shared store, so a surface built without one
// refuses the grant rather than accepting replays.
func newWorkloadGrant(db *pgxpool.Pool, redisClient *redis.Client, policy *guardian.Policy, meterProvider metric.MeterProvider, logger *slog.Logger) *workloadGrant {
	if redisClient == nil {
		return nil
	}
	store := ratelimit.NewRedisStore(redisClient)
	keys, err := jwks.NewKeyResolver(
		jwks.NewResolver(policy, meterProvider, logger),
		jwks.NewMemoryCache(),
		ratelimit.New(store, "workload_assertion_jwks_refresh", workloadKeyRefreshRate),
		ratelimit.New(store, "workload_assertion_jwks_fetch", workloadKeyFetchRate),
		logger,
	)
	if err != nil {
		logger.ErrorContext(context.Background(), "workload assertion key resolver unavailable, the workload grant will be refused", attr.SlogError(err))
		return nil
	}
	guard, err := replay.NewRedisGuard(redisClient, "workload_assertion_jti", assertioncore.ReplayHoldFor(workloadAssertionMaxLifetime))
	if err != nil {
		logger.ErrorContext(context.Background(), "workload assertion replay guard unavailable, the workload grant will be refused", attr.SlogError(err))
		return nil
	}
	verifier, err := workload.NewVerifier(keys, guard)
	if err != nil {
		logger.ErrorContext(context.Background(), "workload assertion verifier unavailable, the workload grant will be refused", attr.SlogError(err))
		return nil
	}
	return &workloadGrant{
		issuers:    newWorkloadIssuerAdmission(workloadIssuerStoreLookup(db), newWorkloadIssuerLookupBudget(redisClient, meterProvider)),
		identities: workloadIdentityStoreLookup(db),
		verifier:   verifier,
	}
}

// workloadIssuerStoreLookup resolves an assertion's iss to a workload issuer in
// the tenancy's project or the organization above it.
func workloadIssuerStoreLookup(db workloadidentity_repo.DBTX) workloadIssuerLookup {
	return func(ctx context.Context, tenancy workloadTenancy, issuerURL string) (workloadidentity_repo.WorkloadIssuer, bool, error) {
		issuer, err := workloadidentity.ResolveIssuerByURL(ctx, db, workloadidentity.ResolveIssuerParams{
			OrganizationID: tenancy.OrganizationID,
			ProjectID:      uuid.NullUUID{UUID: tenancy.ProjectID, Valid: tenancy.ProjectID != uuid.Nil},
			IssuerURL:      issuerURL,
		})
		switch {
		case errors.Is(err, workloadidentity.ErrIssuerNotFound):
			var notFound workloadidentity_repo.WorkloadIssuer
			return notFound, false, nil
		case err != nil:
			return workloadidentity_repo.WorkloadIssuer{}, false, fmt.Errorf("resolve workload issuer by url: %w", err)
		}
		return issuer, true, nil
	}
}

// workloadIdentityStoreLookup reports whether the tenant admits a workload.
func workloadIdentityStoreLookup(db workloadidentity_repo.DBTX) workloadIdentityLookup {
	return func(ctx context.Context, identity workloadIdentity) (bool, error) {
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
	}
}

// workloadGrantOutcome is how a refused grant answers on the wire. A string
// so it can be logged, or carried as a metric dimension, by value.
type workloadGrantOutcome string

const (
	// workloadGrantRefused answers invalid_grant, identically for every
	// issuer and reason.
	workloadGrantRefused workloadGrantOutcome = "refused"
	// workloadGrantUnavailable answers 503: a stage reached no decision.
	workloadGrantUnavailable workloadGrantOutcome = "unavailable"
	// workloadGrantRateLimited answers 429 with the limiter's Retry-After.
	workloadGrantRateLimited workloadGrantOutcome = "rate_limited"
)

// workloadGrantError is a stage's refusal of the grant. The reason is a
// stable diagnostic code for logs; the wire never carries it.
type workloadGrantError struct {
	reason     string
	outcome    workloadGrantOutcome
	retryAfter time.Duration
	err        error
}

func (e *workloadGrantError) Error() string { return fmt.Sprintf("%s: %v", e.reason, e.err) }
func (e *workloadGrantError) Unwrap() error { return e.err }

func refuseWorkloadGrant(reason string, err error) error {
	return &workloadGrantError{reason: reason, outcome: workloadGrantRefused, retryAfter: 0, err: err}
}

func workloadGrantStageUnavailable(reason string, err error) error {
	return &workloadGrantError{reason: reason, outcome: workloadGrantUnavailable, retryAfter: workloadGrantUnavailableRetryAfter, err: err}
}

// presentedWorkload is what an assertion claims before and after verification:
// the iss and sub it presented, and the issuer row iss resolved to once it
// has. It is what a rejection log line records.
type presentedWorkload struct {
	issuerURL string
	subject   string
	issuerID  uuid.UUID
}

// admitWorkloadAssertion runs the grant's verification stages in order:
// resolve iss to a workload issuer in the grant's tenancy, verify the
// assertion against that issuer's key set, then check the tenant admits the
// subject. Admission is the security boundary; every earlier stage only
// establishes that the platform minted the token.
//
// iss is read unverified to find the issuer, and sub to seed the expected
// subject; Verify checks both against the signed assertion before admission.
func admitWorkloadAssertion(
	ctx context.Context,
	grant *workloadGrant,
	tenancy workloadTenancy,
	audiences []string,
	raw string,
) (presentedWorkload, error) {
	presented := presentedWorkload{issuerURL: "", subject: "", issuerID: uuid.Nil}

	token, err := assertioncore.ParseSigned(raw, workloadAssertionMaxBytes)
	if err != nil {
		return presented, refuseWorkloadGrant(string(workload.ReasonMalformed), err)
	}
	var claims jwt.Claims
	if err := token.UnsafeClaimsWithoutVerification(&claims); err != nil {
		return presented, refuseWorkloadGrant(string(workload.ReasonMalformed), err)
	}
	presented.issuerURL = claims.Issuer
	presented.subject = claims.Subject

	switch {
	case len(claims.Audience) != 1:
		// A token naming several audiences is valid at each of them; the
		// grant accepts only one minted for this authorization server alone.
		return presented, refuseWorkloadGrant("assertion_audience_not_single", fmt.Errorf("aud names %d values", len(claims.Audience)))
	case claims.Subject == "" || claims.Subject == claims.Issuer:
		return presented, refuseWorkloadGrant("assertion_subject_invalid", errors.New("sub is empty or equals iss"))
	}

	issuer, err := grant.issuers.admit(ctx, tenancy, claims.Issuer)
	switch {
	case errors.Is(err, errWorkloadIssuerUntrusted):
		return presented, refuseWorkloadGrant("issuer_untrusted", err)
	case errors.Is(err, errWorkloadIssuerLookupRateLimited):
		refusal := &workloadGrantError{reason: "issuer_lookup_rate_limited", outcome: workloadGrantRateLimited, retryAfter: 0, err: err}
		if limited, ok := errors.AsType[*workloadIssuerRateLimitedError](err); ok {
			refusal.retryAfter = limited.retryAfter
		}
		return presented, refusal
	case err != nil:
		return presented, workloadGrantStageUnavailable("issuer_lookup_unavailable", err)
	}
	presented.issuerID = issuer.ID

	source, err := workloadIssuerKeySource(tenancy, &issuer)
	if err != nil {
		return presented, refuseWorkloadGrant("issuer_jwks_uri_invalid", err)
	}

	if _, err := grant.verifier.Verify(ctx, raw, workload.Expectation{
		Issuer:       issuer.Issuer,
		Subject:      claims.Subject,
		KeySource:    source,
		ReplayIssuer: tenancy.UserSessionIssuerID.String(),
		ReplayParty:  issuer.ID.String(),
		Audiences:    audiences,
		MaxLifetime:  workloadAssertionMaxLifetime,
	}); err != nil {
		reason := workload.ReasonOf(err)
		switch {
		case errors.Is(err, jwks.ErrRefreshRateLimited), errors.Is(err, jwks.ErrFetchRateLimited):
			// The key set could not be consulted, which decides nothing about
			// the assertion's kid.
			return presented, workloadGrantStageUnavailable("issuer_keys_rate_limited", err)
		}
		switch reason {
		case workload.ReasonKeyUnresolvable, workload.ReasonReplayStoreUnavailable:
			return presented, workloadGrantStageUnavailable(string(reason), err)
		case workload.ReasonVerifierMisconfigured, "":
			return presented, fmt.Errorf("verify workload assertion: %w", err)
		default:
			return presented, refuseWorkloadGrant(string(reason), err)
		}
	}

	err = admitWorkloadIdentity(ctx, grant.identities, tenancy, issuer.ID, claims.Subject)
	switch {
	case errors.Is(err, errWorkloadNotAdmitted):
		return presented, refuseWorkloadGrant("subject_not_admitted", err)
	case err != nil:
		return presented, workloadGrantStageUnavailable("subject_admission_unavailable", err)
	}

	return presented, nil
}

// workloadAssertionGrantAdvertised reports whether the endpoint's metadata
// lists the grant: the deployment serves it, and the endpoint can carry the
// agent session policy a workload session is minted with.
//
// Deliberately decided from the resolved endpoint alone. This runs on
// `/.well-known` metadata, which every MCP client hits during discovery
// without authenticating, so it reads no rollout state and makes no lookup of
// its own. The organization's rollout is evaluated at the token endpoint,
// which is where a grant is accepted or refused; metadata is advisory, and
// clients cache it for their whole process lifetime anyway.
func (s *Service) workloadAssertionGrantAdvertised(endpoint *ResolvedMcpEndpoint) bool {
	if s.workloadGrant == nil {
		return false
	}
	_, ok := agentAuthorizationTarget(endpoint)
	return ok
}

// handleWorkloadAssertionGrant exchanges a workload's platform-issued
// identity token for a resource-scoped session. It is the JWT bearer grant's
// clientless branch: a request presenting client authentication is the ID-JAG
// exchange and never reaches it.
//
// The wire never distinguishes an untrusted issuer from an unadmitted subject,
// or either from a failed signature: every refusal is the same invalid_grant,
// because telling them apart would read the tenant's trust policy back to an
// unauthenticated caller. The logs do tell them apart, by reason code.
func (s *Service) handleWorkloadAssertionGrant(
	ctx context.Context,
	w http.ResponseWriter,
	r *http.Request,
	endpoint *ResolvedMcpEndpoint,
	creds presentedClientCredentials,
	baseURL string,
	logger *slog.Logger,
) error {
	if refused, err := s.refuseUnservedWorkloadGrant(ctx, w, r, creds, endpoint.OrganizationID, logger); refused {
		return err
	}

	assertion := r.PostForm.Get(oauthwire.ParamAssertion)
	resources := r.PostForm[oauthwire.ParamResource]
	switch {
	case assertion == "":
		return writeTokenError(ctx, w, logger, http.StatusBadRequest, oautherr.CodeInvalidRequest, "assertion is required")
	case len(resources) == 0:
		return writeTokenError(ctx, w, logger, http.StatusBadRequest, oautherr.CodeInvalidRequest, "resource is required")
	}
	canonicalResource, err := endpoint.RootURL(baseURL)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "build workload grant resource identifier").LogError(ctx, logger)
	}
	if err := oauthwire.ValidateResourceIndicators(resources, canonicalResource); err != nil {
		return writeTokenOAuthError(ctx, w, logger, http.StatusBadRequest, err)
	}

	urls, err := s.requestAuthorizationServerURLs(ctx, endpoint, baseURL)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "build workload assertion audiences").LogError(ctx, logger)
	}
	presented, err := admitWorkloadAssertion(ctx, s.workloadGrant, endpoint.workloadTenancy(), []string{urls.Issuer, urls.Token}, assertion)
	if err != nil {
		return s.writeWorkloadGrantRefusal(ctx, w, logger, presented, err)
	}

	subject := urn.NewWorkloadSubject(presented.issuerID, presented.subject)
	if _, _, err := subject.Workload(); err != nil {
		return s.writeWorkloadGrantRefusal(ctx, w, logger, presented, refuseWorkloadGrant("assertion_subject_invalid", err))
	}
	target, ok := agentAuthorizationTarget(endpoint)
	if !ok {
		return s.writeWorkloadGrantRefusal(ctx, w, logger, presented, refuseWorkloadGrant("endpoint_not_agent_addressable", errors.New("endpoint cannot carry an agent session policy")))
	}
	version := runtimepolicy.CurrentDelegatedPolicyVersion
	delegatedGrants, err := encodeAgentSessionPolicy(*target, version)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "encode workload session policy").LogError(ctx, logger)
	}
	// The same check the MCP side applies to the stored row, so a session it
	// would refuse is never written.
	credential, err := loadWorkloadSessionCredential(
		endpoint, subject, subject, pgtype.Text{String: endpoint.OrganizationID, Valid: true},
		delegatedGrants, pgtype.Int4{Int32: int32(version), Valid: true},
	)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "validate workload session policy").LogError(ctx, logger)
	}
	issuerURL, err := s.issuerURL(endpoint, baseURL)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "build issuer URL").LogError(ctx, logger)
	}

	projectID := endpoint.ProjectID
	return s.issueWorkloadGrantSession(ctx, w, logger, workloadGrantIssuance{
		presented:  presented,
		credential: credential,
		projectID:  &projectID,
		endpoint:   endpoint,
		session: workloadSessionIssuance(workloadSessionTarget{
			userSessionIssuerID: endpoint.UserSessionIssuerID,
			projectID:           endpoint.ProjectID,
			organizationID:      endpoint.OrganizationID,
			issuer:              issuerURL,
			audience:            canonicalResource,
		}, subject, credential),
	})
}

// refuseUnservedWorkloadGrant refuses a workload grant the deployment or the
// organization does not serve, and reports whether it did.
//
// A deployment without the grant's dependencies answers as it did before the
// grant existed. A workload acts through its assigned agent's policy, which
// the MCP side honours only under the agent authorization rollout, so minting
// without it would issue sessions refused on first use.
func (s *Service) refuseUnservedWorkloadGrant(
	ctx context.Context,
	w http.ResponseWriter,
	r *http.Request,
	creds presentedClientCredentials,
	organizationID string,
	logger *slog.Logger,
) (bool, error) {
	unresolved := presentedWorkload{issuerURL: "", subject: "", issuerID: uuid.Nil}
	if s.workloadGrant == nil {
		return true, refuseClientlessTokenGrant(ctx, w, r, creds, logger)
	}
	rolloutEnabled, _, err := s.agentAuthorizationRollout(ctx, logger, organizationID)
	switch {
	case err != nil:
		return true, s.writeWorkloadGrantRefusal(ctx, w, logger, unresolved, workloadGrantStageUnavailable("agent_rollout_unavailable", err))
	case !rolloutEnabled:
		return true, s.writeWorkloadGrantRefusal(ctx, w, logger, unresolved, refuseWorkloadGrant("agent_rollout_disabled", errWorkloadRolloutDisabled))
	}
	return false, nil
}

// workloadGrantIssuance is a workload grant whose assertion verified, ready
// for admission and minting.
type workloadGrantIssuance struct {
	// presented is the identity the assertion presented, for logs.
	presented presentedWorkload

	// credential is the ceiling the session is minted with.
	credential workloadSessionCredential

	// projectID is the project admission acts in, or nil for none.
	projectID *uuid.UUID

	// endpoint is the one MCP server a grant naming a resource is minted for;
	// admission also checks the agent may connect to it. Nil for a session
	// minted for all of an issuer's MCP servers, each of which checks on use.
	endpoint *ResolvedMcpEndpoint

	// session is the session issueSession mints.
	session sessionIssuance
}

// workloadSessionTarget is where a workload session is minted.
type workloadSessionTarget struct {
	// userSessionIssuerID is the issuer the session row belongs to.
	userSessionIssuerID uuid.UUID

	// projectID and organizationID scope the issuer lookup that bounds the
	// session's lifetime; projectID is uuid.Nil for an organization issuer.
	projectID uuid.UUID

	// organizationID is the organization the session acts in.
	organizationID string

	// issuer is the access token's `iss`.
	issuer string

	// audience is the access token's audience.
	audience string
}

// workloadSessionIssuance is the session a workload grant mints at target:
// for subject with credential as its ceiling, clientless, never refreshable,
// and authorized for exactly as long as its access token.
func workloadSessionIssuance(target workloadSessionTarget, subject urn.SessionSubject, credential workloadSessionCredential) sessionIssuance {
	lifetime := workloadSessionLifetime
	return sessionIssuance{
		UserSessionIssuerID:    target.userSessionIssuerID,
		ProjectID:              target.projectID,
		OrganizationID:         target.organizationID,
		Issuer:                 target.issuer,
		Audience:               target.audience,
		Resource:               "",
		Refreshable:            false,
		StoresRefreshHash:      false,
		Subject:                subject,
		AuthorizerUserID:       pgtype.Text{String: "", Valid: false},
		DelegatedGrants:        credential.DelegatedGrants,
		DelegatedGrantsVersion: pgtype.Int4{Int32: credential.DelegatedGrantsVersion, Valid: true},
		ToolSelection:          nil,
		AuthorizationExpiresAt: nil,
		DesiredSessionDuration: &lifetime,
		Replayable:             false,
	}
}

// issueWorkloadGrantSession admits a verified workload grant and mints its
// session.
//
// Admission is the one the MCP side runs on every request with the session,
// so a workload with no live assigned agent is refused here rather than on
// first use, where the client would exchange again and loop. The session does
// not exist yet, but authz treats an AuthContext without a session as an
// internal call, so a namespaced id makes this context session-like.
func (s *Service) issueWorkloadGrantSession(ctx context.Context, w http.ResponseWriter, logger *slog.Logger, grant workloadGrantIssuance) error {
	organizationID := grant.session.OrganizationID
	subject := grant.session.Subject

	authCtx, err := s.sessionAuthContext(ctx, organizationID, grant.projectID, "workload-grant:"+uuid.NewString())
	if err != nil {
		return s.writeWorkloadGrantRefusal(ctx, w, logger, grant.presented, workloadGrantStageUnavailable("agent_admission_unavailable", err))
	}
	authorizationCtx := contextvalues.WithAuthenticatedActor(ctx, authCtx, urn.NewWorkloadPrincipal(grant.presented.issuerID, grant.presented.subject))
	authorizationCtx, err = s.admitWorkloadSession(authorizationCtx, organizationID, subject, grant.credential)
	if err == nil && grant.endpoint != nil {
		authorizationCtx, err = s.requireWorkloadSessionAuthorization(authorizationCtx, grant.endpoint, workloadSessionReachResource)
	}
	switch {
	case errors.Is(err, errWorkloadRolloutDisabled), errors.Is(err, errCredentialRejected), err != nil && isCredentialDenial(err):
		return s.writeWorkloadGrantRefusal(ctx, w, logger, grant.presented, refuseWorkloadGrant("agent_admission_denied", err))
	case err != nil:
		return s.writeWorkloadGrantRefusal(ctx, w, logger, grant.presented, workloadGrantStageUnavailable("agent_admission_unavailable", err))
	}

	// A database that cannot start or commit the transaction is an outage,
	// not a verdict on the assertion: the caller is told to retry rather than
	// handed a permanent failure.
	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return s.writeWorkloadGrantRefusal(ctx, w, logger, grant.presented, workloadGrantStageUnavailable("session_persist_unavailable", err))
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	minted, err := s.issueSession(authorizationCtx, nil, usersessions_repo.New(dbtx), grant.session, logger)
	if err != nil {
		return err
	}
	if err := dbtx.Commit(ctx); err != nil {
		return s.writeWorkloadGrantRefusal(ctx, w, logger, grant.presented, workloadGrantStageUnavailable("session_persist_unavailable", err))
	}

	if err := writeTokenSuccess(ctx, w, logger, minted.Body); err != nil {
		return err
	}
	logger.InfoContext(ctx, "workload session issued",
		attr.SlogOAuthGrant(oauthwire.GrantTypeJWTBearer),
		attr.SlogWorkloadIssuerID(grant.presented.issuerID.String()),
		attr.SlogWorkloadSubject(grant.presented.subject),
		attr.SlogOAuthTokenAudience([]string{minted.Audience}),
		attr.SlogUserSessionID(minted.ID.String()),
	)
	return nil
}

// writeWorkloadGrantRefusal logs a refused grant with its reason and the
// presented identity, then answers without either. The assertion itself is
// never logged.
func (s *Service) writeWorkloadGrantRefusal(ctx context.Context, w http.ResponseWriter, logger *slog.Logger, presented presentedWorkload, err error) error {
	refusal, ok := errors.AsType[*workloadGrantError](err)
	if !ok {
		return oops.E(oops.CodeUnexpected, err, "workload assertion grant failed").LogError(ctx, logger)
	}

	issuerID := "unresolved"
	if presented.issuerID != uuid.Nil {
		issuerID = presented.issuerID.String()
	}
	logger.InfoContext(ctx, "workload assertion grant refused",
		attr.SlogOAuthGrant(oauthwire.GrantTypeJWTBearer),
		attr.SlogOAuthFailureReason(refusal.reason),
		attr.SlogWorkloadIssuerID(issuerID),
		attr.SlogWorkloadAssertionIssuer(presented.issuerURL),
		attr.SlogWorkloadSubject(presented.subject),
		attr.SlogError(refusal.err),
	)

	switch refusal.outcome {
	case workloadGrantRefused:
		return writeTokenError(ctx, w, logger, http.StatusBadRequest, oautherr.CodeInvalidGrant, "assertion is invalid")
	case workloadGrantRateLimited:
		w.Header().Set("Retry-After", retryAfterSeconds(refusal.retryAfter))
		return writeTokenError(ctx, w, logger, http.StatusTooManyRequests, oautherr.CodeTemporarilyUnavailable, "too many requests; retry later")
	case workloadGrantUnavailable:
		w.Header().Set("Retry-After", retryAfterSeconds(refusal.retryAfter))
		return writeTokenError(ctx, w, logger, http.StatusServiceUnavailable, oautherr.CodeTemporarilyUnavailable, "the token endpoint is temporarily unavailable")
	default:
		return oops.E(oops.CodeUnexpected, err, "unknown workload grant outcome").LogError(ctx, logger)
	}
}

// retryAfterSeconds renders a Retry-After delay in whole seconds, never less
// than one.
func retryAfterSeconds(d time.Duration) string {
	return strconv.Itoa(max(1, int(math.Ceil(d.Seconds()))))
}
