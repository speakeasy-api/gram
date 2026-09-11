// Live validation for the consent page: probe the routed credential upstream and record the verdict.

package mcp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel/metric"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpversions"
	"github.com/speakeasy-api/gram/server/internal/mcp/tunnelrouting"
	mcpservers_repo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/ratelimit"
	"github.com/speakeasy-api/gram/server/internal/remotemcp"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// errRemoteSessionUnroutable marks a credential no upstream on this endpoint is ever handed.
var errRemoteSessionUnroutable = errors.New("remote session routes to no upstream on this endpoint")

// errValidationRateLimited marks a verify refused before any probe ran.
var errValidationRateLimited = errors.New("remote session validation rate limited")

// validationRate caps verifies per consent challenge.
var validationRate = ratelimit.PerMinute(6)

// validationLimitedNotice is the fixed card copy for a rate-limited verify.
const validationLimitedNotice = "Try again in a moment"

// newValidationLimiter builds the per-challenge verify limiter; nil without Redis.
func newValidationLimiter(redisClient *redis.Client, meterProvider metric.MeterProvider) *ratelimit.Limiter {
	if redisClient == nil {
		return nil
	}
	return ratelimit.New(ratelimit.NewRedisStore(redisClient), "remote_session_validate", validationRate, ratelimit.WithMetrics(meterProvider))
}

// validationTarget is the upstream a credential is presented to: the name a verdict quotes, and the builder that dials it.
type validationTarget struct {
	name  string
	build memberProxyBuilder
}

// validateRemoteSession probes one card's grant through the runtime routing path and records the verdict.
func (s *Service) validateRemoteSession(
	ctx context.Context,
	logger *slog.Logger,
	endpoint *ResolvedMcpEndpoint,
	challengeState AuthnChallengeState,
	client remotesessions.Client,
) error {
	if s.validationLimiter != nil {
		res, lerr := s.validationLimiter.Allow(ctx, challengeState.ID)
		switch {
		case lerr != nil:
			logger.WarnContext(ctx, "remote session validation limiter unavailable; allowing", attr.SlogError(lerr))
		case !res.Allowed:
			logger.InfoContext(ctx, "remote session validation rate limited")
			return errValidationRateLimited
		}
	}
	return s.probeRemoteSession(ctx, logger, endpoint, challengeState, client, nil)
}

// probeRemoteSession presents the card's credential to its upstream and records
// the verdict. Callers pace it: the consent action through the per-challenge
// limiter, the connect path by running it once per committed grant.
func (s *Service) probeRemoteSession(
	ctx context.Context,
	logger *slog.Logger,
	endpoint *ResolvedMcpEndpoint,
	challengeState AuthnChallengeState,
	client remotesessions.Client,
	expectedGrant *remotesessions.RemoteGrant,
) error {
	subject := *challengeState.Subject
	logger = logger.With(attr.SlogRemoteSessionClientID(client.ID.String()))

	tokens, err := s.remoteChallengeMgr.ResolveAvailableAccessTokens(ctx, endpoint.ProjectID, endpoint.OrganizationID, endpoint.UserSessionIssuerID, subject)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "resolve upstream tokens for validation").LogError(ctx, logger)
	}
	entry, usable := tokens[client.RemoteSessionIssuerID]
	if !usable || entry.RemoteSessionClientID != client.ID {
		return oops.E(oops.CodeBadRequest, nil, "Connect this service before verifying it.").LogWarn(ctx, logger)
	}
	if expectedGrant != nil && expectedGrant.RemoteSessionID != uuid.Nil &&
		(entry.RemoteSessionID != expectedGrant.RemoteSessionID || !entry.RemoteSessionResolvedFromUpdatedAt.Equal(expectedGrant.RemoteSessionUpdatedAt)) {
		logger.InfoContext(ctx, "new remote grant not verified: credential changed before probe")
		return nil
	}
	// Route the complete credential set, just as serving does. The target resolver
	// separately proves that this card is the credential selected for the target.
	probeCtx, target, err := s.resolveValidationTarget(ctx, logger, endpoint, challengeState, client, tokens)
	switch {
	case errors.Is(err, errRemoteSessionUnroutable):
		return oops.E(oops.CodeBadRequest, err, "This connection is not used by any server behind this endpoint, so it cannot be verified.").LogWarn(ctx, logger)
	case err != nil:
		if _, ok := errors.AsType[*oops.ShareableError](err); ok {
			return err
		}
		return oops.E(oops.CodeUnexpected, err, "resolve validation target").LogError(ctx, logger)
	}

	ref := remotesessions.RemoteSessionRef{
		ID:             entry.RemoteSessionID,
		Subject:        subject,
		ClientID:       client.ID,
		UpdatedAt:      entry.RemoteSessionUpdatedAt,
		ProjectID:      endpoint.ProjectID,
		OrganizationID: endpoint.OrganizationID,
	}
	// The issuer's own interfaces run beside the probe, under their own budget;
	// only they can say whether the provider still honours the token at all.
	var upstream remotesessions.UpstreamVerification
	enriched := make(chan struct{})
	go func() {
		defer close(enriched)
		var err error
		if upstream, err = s.remoteChallengeMgr.EnrichRemoteSession(ctx, ref); err != nil {
			logger.WarnContext(ctx, "remote session enrichment failed", attr.SlogError(err))
		}
	}()
	probedAt := time.Now()
	verdict, reason := s.probeUpstream(probeCtx, logger, target.build, target.name)
	<-enriched
	issuerDisplay, _ := issuerCardBranding(client, s.serverURL)
	verdict, reason = combineUpstreamVerdict(verdict, reason, upstream, issuerDisplay)
	logger = logger.With(attr.SlogRemoteSessionID(entry.RemoteSessionID.String()), attr.SlogOutcome(string(verdict)))
	s.validationMetrics.Record(ctx, client.IssuerURL, string(verdict))

	written, err := s.remoteChallengeMgr.RecordRemoteSessionValidation(ctx, ref, remotesessions.RemoteSessionValidation{
		Status: verdict,
		Reason: reason,
		At:     probedAt,
	})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "record remote session validation").LogError(ctx, logger)
	}
	if !written {
		logger.InfoContext(ctx, "remote session validation dropped; grant changed or verdict superseded during the probe")
		return nil
	}
	logger.InfoContext(ctx, "remote session validated", attr.SlogReason(reason))
	return nil
}

// resolveValidationTarget finds the upstream this endpoint would hand the credential to, judged as the consent subject.
func (s *Service) resolveValidationTarget(
	ctx context.Context,
	logger *slog.Logger,
	endpoint *ResolvedMcpEndpoint,
	challengeState AuthnChallengeState,
	client remotesessions.Client,
	tokens map[uuid.UUID]remotesessions.UpstreamToken,
) (context.Context, validationTarget, error) {
	var none validationTarget
	subject := *challengeState.Subject
	sessionID := "consent:" + challengeState.ID
	ctx, err := s.contextForSessionSubject(ctx, endpoint, subject, sessionID, challengeState.ClientID)
	if err != nil {
		return ctx, none, fmt.Errorf("stamp consent subject context: %w", err)
	}

	switch {
	case endpoint.MetaMcpServerID.Valid:
		return s.metaValidationTarget(ctx, logger, endpoint, client, tokens, sessionID, subject)
	case endpoint.McpServerID.Valid:
		return s.standaloneValidationTarget(ctx, logger, endpoint, challengeState, client, tokens)
	default:
		return ctx, none, errRemoteSessionUnroutable
	}
}

// metaValidationTarget dials the member the grant is qualified to, through the gateway's own routing.
func (s *Service) metaValidationTarget(
	ctx context.Context,
	logger *slog.Logger,
	endpoint *ResolvedMcpEndpoint,
	client remotesessions.Client,
	tokens map[uuid.UUID]remotesessions.UpstreamToken,
	sessionID string,
	subject urn.SessionSubject,
) (context.Context, validationTarget, error) {
	var none validationTarget
	rows, claimed, err := s.claimingMetaMembers(ctx, endpoint, client.RemoteSessionIssuerID)
	if err != nil {
		return ctx, none, fmt.Errorf("resolve claiming meta MCP members: %w", err)
	}
	if !claimed {
		return ctx, none, errRemoteSessionUnroutable
	}
	entry := tokens[client.RemoteSessionIssuerID]
	targetID := uuid.Nil
	for _, row := range rows {
		// The dispatch selectors: a tunneled member takes only its own issuer's entry (tunneledIssuerToken).
		routes := grantRoutesToUpstream(entry.Resource, row.UpstreamUrl, false)
		if row.Tunneled {
			routes = tunneledIssuerToken(tokens, conv.ToNullUUID(client.RemoteSessionIssuerID), row.UpstreamUrl) != ""
		}
		if routes {
			targetID = row.McpServerID
			break
		}
	}
	if targetID == uuid.Nil {
		return ctx, none, errRemoteSessionUnroutable
	}

	ctx, members, err := s.resolveMetaMemberSnapshot(ctx, logger, endpoint.MetaMcpServerID.UUID, endpoint.ProjectID)
	if err != nil {
		return ctx, none, fmt.Errorf("resolve meta MCP member snapshot: %w", err)
	}
	var member metaMember
	found := false
	for _, candidate := range members {
		if candidate.serverID == targetID {
			member = candidate
			found = true
			break
		}
	}
	if !found {
		return ctx, none, errRemoteSessionUnroutable
	}
	name := conv.Default(member.name, member.slug)

	selectedTokens := map[uuid.UUID]remotesessions.UpstreamToken{
		client.RemoteSessionIssuerID: entry,
	}
	gate := metaGateContext{
		projectID:       endpoint.ProjectID,
		metaServerID:    endpoint.MetaMcpServerID.UUID,
		organizationID:  endpoint.OrganizationID,
		tokens:          selectedTokens,
		toolSelection:   nil,
		authenticated:   subject.Kind != urn.SessionSubjectKindAnonymous,
		sessionID:       sessionID,
		chatID:          "",
		userID:          "",
		externalUserID:  "",
		apiKeyID:        "",
		protocolVersion: mcpversions.Resolution{Declared: "", InEffect: metaMemberUpstreamProtocolVersion},
	}
	switch subject.Kind {
	case urn.SessionSubjectKindUser:
		gate.userID = subject.ID
	case urn.SessionSubjectKindAPIKey:
		gate.apiKeyID = subject.ID
	case urn.SessionSubjectKindAgent, urn.SessionSubjectKindAnonymous:
		// The agent actor rides on ctx; the gate has no field for it.
	}
	ctx = s.memberAttributionContext(ctx, logger, &gate)
	// routeMetaMember, not dialMetaMember: a probe is not a dispatch to count.
	dial, _, err := s.routeMetaMember(ctx, logger, gate, member, gate.callerIdentity())
	if err != nil {
		if errors.Is(err, errAmbiguousMemberCredential) {
			return ctx, none, fmt.Errorf("%w: %w", errRemoteSessionUnroutable, err)
		}
		if memberErr, ok := errors.AsType[*metaMemberError](err); ok {
			// An unservable member is the probe's failure to report, not a reason to refuse the check.
			return ctx, validationTarget{name: name, build: func(context.Context) (*proxy.Proxy, error) { return nil, memberErr }}, nil
		}
		return ctx, none, fmt.Errorf("dial meta MCP member: %w", err)
	}
	if dial.anonymous {
		return ctx, none, errRemoteSessionUnroutable
	}
	// The dial proves this card matches the actual backend. Check the complete
	// set for ambiguity without loading and authorizing that backend twice.
	if _, err := routeMetaMemberToken(tokens, member, entry.Resource); err != nil {
		return ctx, none, fmt.Errorf("%w: %w", errRemoteSessionUnroutable, err)
	}
	return ctx, validationTarget{name: name, build: probeProxyBuilder(dial.build)}, nil
}

// standaloneValidationTarget dials a proxied endpoint's own backend, routed as serveRemoteBackend and serveTunneledBackend route it.
func (s *Service) standaloneValidationTarget(
	ctx context.Context,
	logger *slog.Logger,
	endpoint *ResolvedMcpEndpoint,
	challengeState AuthnChallengeState,
	client remotesessions.Client,
	tokens map[uuid.UUID]remotesessions.UpstreamToken,
) (context.Context, validationTarget, error) {
	var none validationTarget
	server, err := mcpservers_repo.New(s.db).GetMCPServerByIDAndProjectID(ctx, mcpservers_repo.GetMCPServerByIDAndProjectIDParams{
		ID:        endpoint.McpServerID.UUID,
		ProjectID: endpoint.ProjectID,
	})
	if err != nil {
		return ctx, none, fmt.Errorf("load mcp server for validation: %w", err)
	}
	if !server.RemoteMcpServerID.Valid && !server.TunneledMcpServerID.Valid {
		return ctx, none, errRemoteSessionUnroutable
	}

	selected := tokens[client.RemoteSessionIssuerID]
	selectedTokens := map[uuid.UUID]remotesessions.UpstreamToken{client.RemoteSessionIssuerID: selected}
	selectedToken, err := routeUpstreamToken(ctx, logger, selectedTokens, endpoint.UpstreamResource, tunneledBackendIssuer(&server))
	var routeErr *upstreamRoutingError
	switch {
	case errors.As(err, &routeErr), selectedToken == "":
		return ctx, none, errRemoteSessionUnroutable
	case err != nil:
		return ctx, none, fmt.Errorf("route selected upstream token for validation: %w", err)
	}

	token, err := routeUpstreamToken(ctx, logger, tokens, endpoint.UpstreamResource, tunneledBackendIssuer(&server))
	switch {
	case errors.As(err, &routeErr):
		return ctx, none, errRemoteSessionUnroutable
	case err != nil:
		return ctx, none, fmt.Errorf("route upstream token for validation: %w", err)
	case token == "" || token != selectedToken:
		return ctx, none, errRemoteSessionUnroutable
	}

	authorized, err := s.authorizeProxyBackendAccess(ctx, logger, endpoint.ProjectID, &server)
	if err != nil {
		return ctx, none, fmt.Errorf("authorize validation access: %w", err)
	}
	ctx = authorized
	name := conv.Default(server.Name.String, endpoint.Slug)

	if server.RemoteMcpServerID.Valid {
		// The backend rows load here, outside the probe budget.
		build, berr := s.remoteBackendProxyBuilder(ctx, logger, endpoint.ProjectID, endpoint.OrganizationID, &server, token, "", nil, remotemcp.WithoutToolsCallIdentityCoverage())
		if berr != nil {
			return ctx, none, fmt.Errorf("build remote backend proxy for validation: %w", berr)
		}
		return ctx, validationTarget{name: name, build: probeProxyBuilder(build)}, nil
	}
	// One state-derived affinity key pins the handshake and its close to a single gateway.
	affinity := tunnelrouting.HashedClientAffinityKey("consent-validate", challengeState.ID)
	return ctx, validationTarget{name: name, build: probeProxyBuilder(func(ctx context.Context) (*proxy.Proxy, error) {
		p, berr := s.tunnelManager.buildProxy(ctx, affinity, logger, endpoint.ProjectID, endpoint.OrganizationID, &server, token, "", nil, remotemcp.WithoutToolsCallIdentityCoverage())
		if berr != nil {
			return nil, fmt.Errorf("build tunnel proxy: %w", berr)
		}
		return p, nil
	})}, nil
}

// probeProxyBuilder strips client-session and census interceptors and proxy
// metrics so a synthetic validation handshake is not counted as user traffic.
func probeProxyBuilder(build memberProxyBuilder) memberProxyBuilder {
	return func(ctx context.Context) (*proxy.Proxy, error) {
		p, err := build(ctx)
		if err != nil {
			return nil, err
		}
		p.InitializeRequestInterceptors = nil
		p.UserRequestObservationInterceptors = nil
		p.Metrics = nil
		return p, nil
	}
}

// combineUpstreamVerdict folds the issuer's introspection into the member's
// verdict. A member that accepted the token is trusted over the provider; any
// other verdict yields to an inactive answer, which is authoritative evidence
// that the token is dead (RFC 7662: expired, revoked, or not introspectable).
func combineUpstreamVerdict(verdict remotesessions.ValidationOutcome, reason string, upstream remotesessions.UpstreamVerification, issuer string) (remotesessions.ValidationOutcome, string) {
	if verdict == remotesessions.ValidationOutcomeValid || !upstream.Inactive {
		return verdict, reason
	}
	return remotesessions.ValidationOutcomeInactive, "Inactive at " + issuer
}
