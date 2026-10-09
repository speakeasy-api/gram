package mcp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcpidentity"
	mcpservers_repo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/identitychaining"
	"github.com/speakeasy-api/gram/server/internal/urn"
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
	Serves(ctx context.Context, req identitychaining.Request) (uuid.UUID, bool)
	Configured(ctx context.Context, organizationID string, projectID, userSessionIssuerID uuid.UUID) bool
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
// backendIssuerID is the upstream's own derived issuer. Only a tunneled
// upstream is identified by it, never by the resource it claims, exactly as
// interactive routing treats it; one without a derived issuer calls
// anonymously and so never chains. A remote upstream is identified by its
// resource.
func (s *Service) identityChainingRequest(ctx context.Context, organizationID string, projectID, userSessionIssuerID uuid.UUID, upstreamResource string, tunneled bool, backendIssuerID uuid.NullUUID) (identitychaining.Request, bool) {
	var none identitychaining.Request
	if s.identityChainer == nil {
		return none, false
	}
	identity, ok := mcpidentity.FromContext(ctx)
	if !ok || identity.Kind() != mcpidentity.KindUserSession || identity.UserID() == "" {
		return none, false
	}
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.UserID != identity.UserID() || authCtx.ActiveOrganizationID != organizationID {
		return none, false
	}
	return identitychaining.NewRequest(organizationID, projectID, userSessionIssuerID, identity.UserID(), upstreamResource, tunneled, backendIssuerID)
}

// resolveDirectUpstreamToken resolves the credential a direct remote or
// tunneled endpoint forwards; the zero UpstreamToken calls anonymously. It is
// the issuer gate's all-or-nothing resolution unless an identity chaining
// binding governs the endpoint's upstream. Then only that upstream is
// required: an interactive token for it wins, and identity chaining supplies
// one otherwise. An upstream chaining does not govern gets exactly the strict
// gate's answer, from the tokens already resolved.
func (s *Service) resolveDirectUpstreamToken(ctx context.Context, w http.ResponseWriter, logger *slog.Logger, authentication *issuerGateAuthentication, upstreamResource string, tunneled bool, backendIssuerID uuid.NullUUID) (remotesessions.UpstreamToken, error) {
	var none remotesessions.UpstreamToken
	endpoint := authentication.endpoint
	req, chainable := s.identityChainingRequest(ctx, endpoint.OrganizationID, endpoint.ProjectID, endpoint.UserSessionIssuerID, upstreamResource, tunneled, backendIssuerID)
	if !chainable {
		return s.resolveStrictUpstreamToken(ctx, w, logger, authentication, upstreamResource, tunneled, backendIssuerID)
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
			return none, s.rejectRemoteSession(ctx, w, authentication, strictErr)
		}
		tokens, err = s.remoteChallengeMgr.ResolveAvailableAccessTokens(ctx, endpoint.ProjectID, endpoint.OrganizationID, endpoint.UserSessionIssuerID, authentication.subject)
		if err != nil {
			return none, oops.E(oops.CodeUnexpected, err, "resolve remote session").LogError(ctx, logger)
		}
		if upstreamTokenPresent(tokens, upstreamResource, tunneled, backendIssuerID) {
			return routeDirectUpstreamToken(ctx, w, logger, tokens, upstreamResource, tunneled, backendIssuerID)
		}
	case strictErr != nil:
		return none, oops.E(oops.CodeUnexpected, strictErr, "resolve remote session").LogError(ctx, logger)
	case upstreamTokenPresent(tokens, upstreamResource, tunneled, backendIssuerID):
		return routeDirectUpstreamToken(ctx, w, logger, tokens, upstreamResource, tunneled, backendIssuerID)
	}

	chained, outcome := s.identityChainer.Acquire(ctx, req)
	switch {
	case outcome.Succeeded():
		// The token is delegated to the authenticated human, and comes from
		// no stored grant.
		return remotesessions.UpstreamToken{
			Token:                              chained.Value(),
			Resource:                           "",
			CredentialOwner:                    remotesessions.CredentialOwnerSubject,
			RemoteSessionClientID:              uuid.Nil,
			RemoteSessionID:                    uuid.Nil,
			RemoteSessionUpdatedAt:             time.Time{},
			RemoteSessionResolvedFromUpdatedAt: time.Time{},
			ClientCredentialErr:                nil,
		}, nil
	case !outcome.Applicable() && unusable:
		return none, s.rejectRemoteSession(ctx, w, authentication, strictErr)
	case !outcome.Applicable():
		return routeDirectUpstreamToken(ctx, w, logger, tokens, upstreamResource, tunneled, backendIssuerID)
	case outcome.Reason == identitychaining.ReasonReauthenticationRequired:
		endpoint.LogWith(logger).WarnContext(ctx, "mcp issuer gate rejected: identity chaining requires reauthentication",
			attr.SlogUserSessionIssuerID(endpoint.UserSessionIssuerID.String()),
			attr.SlogMcpURL(authentication.mcpURL),
			attr.SlogOAuthFailureReason(issuerGateReasonIdentityChainingReauthentication),
		)
		s.metrics.RecordMCPRequestRejected(ctx, issuerGateReasonIdentityChainingReauthentication, authentication.mcpURL, authentication.surface)
		// Reauthorizing signs the human in again, which retains a fresh
		// delegation, so the challenge matches a missing upstream session.
		return none, writeRemoteSessionReconnectChallenge(w, authentication)
	}
	return none, identityChainingError(outcome)
}

// resolveStrictUpstreamToken is the issuer gate's all-or-nothing resolution
// followed by routing, the behavior without identity chaining.
func (s *Service) resolveStrictUpstreamToken(ctx context.Context, w http.ResponseWriter, logger *slog.Logger, authentication *issuerGateAuthentication, upstreamResource string, tunneled bool, backendIssuerID uuid.NullUUID) (remotesessions.UpstreamToken, error) {
	var none remotesessions.UpstreamToken
	tokens, err := s.resolveIssuerGateAccessTokens(ctx, w, authentication)
	if err != nil {
		return none, fmt.Errorf("resolve issuer-gated upstream tokens: %w", err)
	}
	return routeDirectUpstreamToken(ctx, w, logger, tokens, upstreamResource, tunneled, backendIssuerID)
}

// routeDirectUpstreamToken routes tokens to the backend, mapping a fail-closed
// routing outcome to a precondition failure and a self client whose
// credential could not be obtained to its remedy.
func routeDirectUpstreamToken(ctx context.Context, w http.ResponseWriter, logger *slog.Logger, tokens map[uuid.UUID]remotesessions.UpstreamToken, upstreamResource string, tunneled bool, backendIssuerID uuid.NullUUID) (remotesessions.UpstreamToken, error) {
	upstreamToken, err := routeUpstreamToken(ctx, logger, tokens, upstreamResource, tunneled, backendIssuerID)
	switch {
	case errors.As(err, new(*upstreamRoutingError)):
		// routeUpstreamToken already logged the structured detail.
		return upstreamToken, oops.E(oops.CodeFailedPrecondition, err, "this MCP server's upstream credentials are not configured unambiguously")
	case errors.Is(err, remotesessions.ErrNoValidToken):
		// remotesessions already logged why the client credential failed.
		return upstreamToken, clientCredentialRoutingError(w, err)
	case err != nil:
		return upstreamToken, oops.E(oops.CodeUnexpected, err, "resolve upstream token for proxied MCP backend").LogError(ctx, logger)
	}
	return upstreamToken, nil
}

// upstreamTokenPresent reports, without routing or logging, whether any
// resolved token claims the backend's upstream. Duplicates and self clients
// whose credential could not be obtained count as present, so routing still
// fails closed on them rather than chaining around them.
func upstreamTokenPresent(tokens map[uuid.UUID]remotesessions.UpstreamToken, upstreamResource string, tunneled bool, backendIssuerID uuid.NullUUID) bool {
	want := strings.TrimRight(upstreamResource, "/")
	if tunneled {
		if !backendIssuerID.Valid {
			return false
		}
		entry, ok := tokens[backendIssuerID.UUID]
		return ok && grantRoutesToUpstream(entry.Resource, want, true)
	}
	for issuerID, entry := range tokens {
		if upstreamTokenRoutes(issuerID, entry, want, backendIssuerID) {
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

// consentChainedClients returns the bound clients whose upstream identity
// chaining is configured to serve for the consent subject, judged with the
// request the runtime builds. It reads configuration only, never acquires a
// token, and degrades to not chained on any lookup fault.
func (s *Service) consentChainedClients(ctx context.Context, endpoint *ResolvedMcpEndpoint, challengeState AuthnChallengeState, clients []remotesessions.Client, routing consentRouting) map[uuid.UUID]bool {
	subject := challengeState.Subject
	if s.identityChainer == nil || len(clients) == 0 || endpoint.UserSessionIssuerID == uuid.Nil || subject == nil || subject.Kind != urn.SessionSubjectKindUser || subject.ID == "" {
		return nil
	}
	if !s.identityChainer.Configured(ctx, endpoint.OrganizationID, endpoint.ProjectID, endpoint.UserSessionIssuerID) {
		return nil
	}
	if endpoint.MetaMcpServerID.Valid {
		return s.consentChainedMetaClients(ctx, endpoint, challengeState, clients, routing)
	}
	tunneled, tunnelIssuer, ok := s.consentDirectBackend(ctx, endpoint, routing)
	if !ok {
		return nil
	}
	req, ok := identitychaining.NewRequest(endpoint.OrganizationID, endpoint.ProjectID, endpoint.UserSessionIssuerID, subject.ID, endpoint.UpstreamResource, tunneled, tunnelIssuer)
	if !ok {
		return nil
	}
	issuer, served := s.identityChainer.Serves(ctx, req)
	if !served {
		return nil
	}
	chained := make(map[uuid.UUID]bool, len(clients))
	for _, c := range clients {
		chained[c.ID] = c.RemoteSessionIssuerID == issuer
	}
	return chained
}

// consentDirectBackend reports whether a direct endpoint's backend is
// tunneled and its derived issuer, reusing resolved routing when present.
func (s *Service) consentDirectBackend(ctx context.Context, endpoint *ResolvedMcpEndpoint, routing consentRouting) (bool, uuid.NullUUID, bool) {
	none := uuid.NullUUID{UUID: uuid.Nil, Valid: false}
	if !endpoint.McpServerID.Valid || strings.TrimRight(endpoint.UpstreamResource, "/") == "" {
		return false, none, false
	}
	switch routing.backend {
	case consentBackendRemote:
		return false, none, true
	case consentBackendTunneled:
		return true, routing.issuer, true
	case consentBackendNone, consentBackendMeta:
	}
	server, err := mcpservers_repo.New(s.db).GetMCPServerByIDAndProjectID(ctx, mcpservers_repo.GetMCPServerByIDAndProjectIDParams{
		ID:        endpoint.McpServerID.UUID,
		ProjectID: endpoint.ProjectID,
	})
	if err != nil {
		s.logger.WarnContext(ctx, "load mcp server for consent identity chaining", attr.SlogError(err))
		return false, none, false
	}
	switch {
	case server.RemoteMcpServerID.Valid:
		return false, none, true
	case server.TunneledMcpServerID.Valid:
		return true, tunneledBackendIssuer(&server), true
	}
	return false, none, false
}

// consentChainedMetaClients marks a gateway card chained only when chaining
// serves every member behind its authorization server through that card's
// own issuer, each judged with the request routeMetaMember's chainer builds.
func (s *Service) consentChainedMetaClients(ctx context.Context, endpoint *ResolvedMcpEndpoint, challengeState AuthnChallengeState, clients []remotesessions.Client, routing consentRouting) map[uuid.UUID]bool {
	var memberCtx context.Context
	type servedBy struct {
		issuer uuid.UUID
		ok     bool
	}
	served := map[identitychaining.Request]servedBy{}
	chained := make(map[uuid.UUID]bool, len(clients))
	for _, c := range clients {
		members, resolved := routing.members[c.RemoteSessionIssuerID]
		if !resolved {
			if memberCtx == nil {
				stamped, err := s.contextForSessionSubject(ctx, endpoint, *challengeState.Subject, "consent:"+challengeState.ID, challengeState.ClientID)
				if err != nil {
					s.logger.WarnContext(ctx, "stamp consent subject for identity chaining", attr.SlogError(err))
					return nil
				}
				memberCtx = stamped
			}
			var err error
			members, _, err = s.claimingMetaMembers(memberCtx, endpoint, c.RemoteSessionIssuerID)
			if err != nil {
				s.logger.WarnContext(ctx, "resolve identity chaining members for consent", attr.SlogError(err))
				continue
			}
		}
		chained[c.ID] = len(members) > 0
		for _, m := range members {
			req, ok := identitychaining.NewRequest(endpoint.OrganizationID, endpoint.ProjectID, endpoint.UserSessionIssuerID, challengeState.Subject.ID, m.UpstreamUrl, m.Tunneled, uuid.NullUUID{UUID: c.RemoteSessionIssuerID, Valid: true})
			if !ok {
				chained[c.ID] = false
				break
			}
			answer, seen := served[req]
			if !seen {
				answer.issuer, answer.ok = s.identityChainer.Serves(ctx, req)
				served[req] = answer
			}
			if !answer.ok || answer.issuer != c.RemoteSessionIssuerID {
				chained[c.ID] = false
				break
			}
		}
	}
	return chained
}
