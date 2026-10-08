package mcp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcpidentity"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/identitychaining"
)

// issuerGateReasonIdentityChainingReauthentication: the human's retained IdP
// delegation cannot be renewed, so the runtime challenged the client to sign
// in again, which retains a fresh delegation.
const issuerGateReasonIdentityChainingReauthentication = "identity_chaining_reauthentication"

// identityChainer acquires downstream tokens by identity chaining when a human
// has no usable interactive token for an upstream.
type identityChainer interface {
	Acquire(ctx context.Context, req identitychaining.Request) (identitychaining.Token, identitychaining.Outcome)
	Governs(ctx context.Context, req identitychaining.Request) bool
}

// SetIdentityChainer enables identity chaining on proxied upstreams. Without
// one, upstream token resolution keeps its interactive-only behavior.
func (s *Service) SetIdentityChainer(chainer identityChainer) {
	s.identityChainer = chainer
}

// identityChainingRequest builds the chaining request for the authenticated
// human in ctx. It reports false when chaining must not run: no executor, or
// a caller that is not a validated user session (assistants, agents,
// workloads and API keys never select a human's delegation). Whether chaining
// applies to the upstream is then decided by its binding configuration.
//
// A tunneled upstream is identified by its own derived issuer, never by the
// resource it claims, exactly as interactive routing treats it; one without a
// derived issuer calls anonymously and so never chains.
func (s *Service) identityChainingRequest(ctx context.Context, organizationID string, projectID, userSessionIssuerID uuid.UUID, upstreamResource string, tunneled bool, tunneledIssuerID uuid.NullUUID) (identitychaining.Request, bool) {
	var none identitychaining.Request
	if s.identityChainer == nil || userSessionIssuerID == uuid.Nil || strings.TrimRight(upstreamResource, "/") == "" || (tunneled && !tunneledIssuerID.Valid) {
		return none, false
	}
	remoteIssuer := uuid.NullUUID{UUID: uuid.Nil, Valid: false}
	if tunneled {
		remoteIssuer = tunneledIssuerID
	}
	identity, ok := mcpidentity.FromContext(ctx)
	if !ok || identity.Kind() != mcpidentity.KindUserSession || identity.UserID() == "" {
		return none, false
	}
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.UserID != identity.UserID() || authCtx.ActiveOrganizationID != organizationID {
		return none, false
	}
	return identitychaining.Request{
		OrganizationID:        organizationID,
		ProjectID:             projectID,
		UserSessionIssuerID:   userSessionIssuerID,
		UserID:                identity.UserID(),
		UpstreamResource:      upstreamResource,
		RemoteSessionIssuerID: remoteIssuer,
	}, true
}

// resolveDirectUpstreamToken resolves the bearer a direct remote or tunneled
// endpoint forwards. It is the issuer gate's all-or-nothing resolution unless
// an identity chaining binding governs the endpoint's upstream. Then only that
// upstream is required: an interactive token for it wins, and identity
// chaining supplies one otherwise. An upstream chaining does not govern gets
// exactly the strict gate's answer, from the tokens already resolved.
func (s *Service) resolveDirectUpstreamToken(ctx context.Context, w http.ResponseWriter, logger *slog.Logger, authentication *issuerGateAuthentication, upstreamResource string, tunneled bool, tunneledIssuerID uuid.NullUUID) (string, error) {
	endpoint := authentication.endpoint
	req, chainable := s.identityChainingRequest(ctx, endpoint.OrganizationID, endpoint.ProjectID, endpoint.UserSessionIssuerID, upstreamResource, tunneled, tunneledIssuerID)
	if !chainable {
		return s.resolveStrictUpstreamToken(ctx, w, logger, authentication, upstreamResource, tunneled, tunneledIssuerID)
	}

	tokens, strictErr := s.remoteChallengeMgr.ResolveAccessTokens(ctx, endpoint.ProjectID, endpoint.OrganizationID, endpoint.UserSessionIssuerID, authentication.subject)
	unusable := errors.Is(strictErr, remotesessions.ErrNoValidToken)
	var err error
	switch {
	case unusable:
		// A linked upstream is unusable. Only an upstream chaining governs is
		// served without it; otherwise the gate answers exactly as without
		// chaining.
		if !s.identityChainer.Governs(ctx, req) {
			return "", s.rejectRemoteSession(ctx, w, authentication, strictErr)
		}
		tokens, err = s.remoteChallengeMgr.ResolveAvailableAccessTokens(ctx, endpoint.ProjectID, endpoint.OrganizationID, endpoint.UserSessionIssuerID, authentication.subject)
		if err != nil {
			return "", oops.E(oops.CodeUnexpected, err, "resolve remote session").LogError(ctx, logger)
		}
		if upstreamTokenPresent(tokens, upstreamResource, tunneled, tunneledIssuerID) {
			return routeDirectUpstreamToken(ctx, logger, tokens, upstreamResource, tunneled, tunneledIssuerID)
		}
	case strictErr != nil:
		return "", oops.E(oops.CodeUnexpected, strictErr, "resolve remote session").LogError(ctx, logger)
	case upstreamTokenPresent(tokens, upstreamResource, tunneled, tunneledIssuerID):
		return routeDirectUpstreamToken(ctx, logger, tokens, upstreamResource, tunneled, tunneledIssuerID)
	}

	chained, outcome := s.identityChainer.Acquire(ctx, req)
	switch {
	case outcome.Succeeded():
		return chained.Value(), nil
	case !outcome.Applicable() && unusable:
		return "", s.rejectRemoteSession(ctx, w, authentication, strictErr)
	case !outcome.Applicable():
		return routeDirectUpstreamToken(ctx, logger, tokens, upstreamResource, tunneled, tunneledIssuerID)
	case outcome.Reason == identitychaining.ReasonReauthenticationRequired:
		endpoint.LogWith(logger).WarnContext(ctx, "mcp issuer gate rejected: identity chaining requires reauthentication",
			attr.SlogUserSessionIssuerID(endpoint.UserSessionIssuerID.String()),
			attr.SlogMcpURL(authentication.mcpURL),
			attr.SlogOAuthFailureReason(issuerGateReasonIdentityChainingReauthentication),
		)
		s.metrics.RecordMCPRequestRejected(ctx, issuerGateReasonIdentityChainingReauthentication, authentication.mcpURL, authentication.surface)
		// Reauthorizing signs the human in again, which retains a fresh
		// delegation, so the challenge matches a missing upstream session.
		return "", writeRemoteSessionReconnectChallenge(w, authentication)
	}
	return "", identityChainingError(outcome)
}

// resolveStrictUpstreamToken is the issuer gate's all-or-nothing resolution
// followed by routing, the behavior without identity chaining.
func (s *Service) resolveStrictUpstreamToken(ctx context.Context, w http.ResponseWriter, logger *slog.Logger, authentication *issuerGateAuthentication, upstreamResource string, tunneled bool, tunneledIssuerID uuid.NullUUID) (string, error) {
	tokens, err := s.resolveIssuerGateAccessTokens(ctx, w, authentication)
	if err != nil {
		return "", fmt.Errorf("resolve issuer-gated upstream tokens: %w", err)
	}
	return routeDirectUpstreamToken(ctx, logger, tokens, upstreamResource, tunneled, tunneledIssuerID)
}

// routeDirectUpstreamToken routes tokens to the backend, mapping a fail-closed
// routing outcome to a precondition failure.
func routeDirectUpstreamToken(ctx context.Context, logger *slog.Logger, tokens map[uuid.UUID]remotesessions.UpstreamToken, upstreamResource string, tunneled bool, tunneledIssuerID uuid.NullUUID) (string, error) {
	upstreamToken, err := routeUpstreamToken(ctx, logger, tokens, upstreamResource, tunneled, tunneledIssuerID)
	switch {
	case errors.As(err, new(*upstreamRoutingError)):
		// routeUpstreamToken already logged the structured detail.
		return "", oops.E(oops.CodeFailedPrecondition, err, "this MCP server's upstream credentials are not configured unambiguously")
	case err != nil:
		return "", oops.E(oops.CodeUnexpected, err, "resolve upstream token for proxied MCP backend").LogError(ctx, logger)
	}
	return upstreamToken, nil
}

// upstreamTokenPresent reports, without routing or logging, whether any
// resolved token claims the backend's upstream. Duplicates count as present
// so routing still fails closed on them rather than chaining around them.
func upstreamTokenPresent(tokens map[uuid.UUID]remotesessions.UpstreamToken, upstreamResource string, tunneled bool, tunneledIssuerID uuid.NullUUID) bool {
	want := strings.TrimRight(upstreamResource, "/")
	if tunneled {
		return tunneledIssuerToken(tokens, tunneledIssuerID, want) != ""
	}
	for _, entry := range tokens {
		if grantRoutesToUpstream(entry.Resource, want, false) {
			return true
		}
	}
	return false
}

// identityChainingError is the client-facing failure for an applicable
// attempt. It names the normalized reason only, never provider detail.
func identityChainingError(outcome identitychaining.Outcome) error {
	if outcome.Retryable {
		return oops.E(oops.CodeUnavailable, nil, "enterprise-managed authorization for this MCP server is temporarily unavailable (%s); retry shortly", outcome.Reason)
	}
	return oops.E(oops.CodeForbidden, nil, "enterprise-managed authorization for this MCP server failed (%s); contact your administrator", outcome.Reason)
}

// metaMemberChainer returns the tools/call gate's chaining hook for member,
// or nil when chaining cannot apply: no gated issuer, or a member outside the
// gateway's project, whose bindings belong to another project.
func (s *Service) metaMemberChainer(gate *metaGateContext, member metaMember) func(ctx context.Context, upstreamResource string) (string, error) {
	if s.identityChainer == nil || gate.userSessionIssuerID == uuid.Nil || member.projectID != gate.projectID {
		return nil
	}
	return func(ctx context.Context, upstreamResource string) (string, error) {
		req, chainable := s.identityChainingRequest(ctx, gate.organizationID, gate.projectID, gate.userSessionIssuerID, upstreamResource, member.tunneledServerID.Valid, member.remoteSessionIssuerID)
		if !chainable {
			return "", nil
		}
		chained, outcome := s.identityChainer.Acquire(ctx, req)
		switch {
		case outcome.Succeeded():
			return chained.Value(), nil
		case !outcome.Applicable():
			return "", nil
		case outcome.Reason == identitychaining.ReasonReauthenticationRequired:
			return "", &metaMemberError{message: fmt.Sprintf("server %q needs a fresh sign-in to renew enterprise-managed access; sign in to this gateway again", member.slug)}
		case outcome.Retryable:
			return "", &metaMemberError{message: fmt.Sprintf("server %q enterprise-managed authorization is temporarily unavailable (%s); retry shortly", member.slug, outcome.Reason)}
		}
		return "", &metaMemberError{message: fmt.Sprintf("server %q enterprise-managed authorization failed (%s); contact your administrator", member.slug, outcome.Reason)}
	}
}
