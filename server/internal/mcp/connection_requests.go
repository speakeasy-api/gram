package mcp

import (
	"context"
	"errors"
	"strings"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/auth/principalcredential"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcpaccess"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
	"github.com/speakeasy-api/gram/server/internal/oauthwire"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotemcp"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersessions_repo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// Per-request rows record refusals and failures only. A request that is served
// writes nothing: at tool-call rates, success rows would fill the recorder's
// write budget and leave the failures to drop.

// errToolUsageLimitReached marks a tools/call refused by the free-tier cap.
var errToolUsageLimitReached = errors.New("tool usage limit reached")

// errRiskPolicyDenied marks a tools/call a risk policy stopped.
var errRiskPolicyDenied = errors.New("risk policy denied the call")

// errTunnelNoLiveRoute marks a tunneled server with no connected tunnel.
var errTunnelNoLiveRoute = errors.New("tunnel has no live route")

// requestApp is the app behind an authenticated request, once its token named one.
func requestApp(ctx context.Context) mcpconnectionattempts.App {
	if clientID, ok := contextvalues.GetOAuthClientID(ctx); ok && clientID != "" {
		return mcpconnectionattempts.AppOf(clientID)
	}
	return mcpconnectionattempts.AppUnverified
}

// recordGateRejection records a bearer the gate refused. Three cases write nothing:
// no bearer and an expired bearer are how a healthy client learns to sign in or
// refresh, and a failed refresh has its own row. A bearer that is none of ours
// names no one, so it would only let a scanner write rows into an organization.
func (s *Service) recordGateRejection(ctx context.Context, endpoint *ResolvedMcpEndpoint, token, reason string, valErr error) {
	if valErr == nil || errors.Is(valErr, jwt.ErrTokenExpired) {
		return
	}
	code := gateHappened(reason)
	subject, verifyErr := s.userSessionSigner.VerifiedSubject(token)
	if verifyErr != nil {
		if code == mcpconnectionattempts.CodeInvalidBearerToken && !strings.HasPrefix(token, "gram_") && !principalcredential.IsToken(token) {
			return
		}
		s.connections.Once(ctx, connectionScope(endpoint, mcpconnectionattempts.AppUnverified), mcpconnectionattempts.StepMCPRequest, code, mcpconnectionattempts.Detail(reason))
		return
	}
	s.connections.Once(ctx, connectionScope(endpoint, mcpconnectionattempts.AppUnverified), mcpconnectionattempts.StepMCPRequest, code, mcpconnectionattempts.Detail(reason), mcpconnectionattempts.Subject(&subject))
}

// gateHappened keeps an outage on our side from reading as a person's bad token.
func gateHappened(reason string) mcpconnectionattempts.Code {
	switch reason {
	case issuerGateReasonRevocationUnavailable,
		"org_lookup_failed",
		"issuer_lookup_failed",
		"caller_profile_unavailable",
		"tool_selection_load_failed",
		"agent_session_load_failed",
		"workload_session_load_failed",
		"workload_admission_load_failed":
		return mcpconnectionattempts.CodeAuthCheckUnavailable
	case issuerGateReasonWorkloadSessionOutOfReach:
		return mcpconnectionattempts.CodeNoServerAccess
	default:
		return mcpconnectionattempts.CodeInvalidBearerToken
	}
}

// remoteSessionHappened maps a rejection whose bearer was accepted.
func remoteSessionHappened(reason string) mcpconnectionattempts.Code {
	switch reason {
	case issuerGateReasonRemoteSessionUnavailable:
		return mcpconnectionattempts.CodeRemoteSessionUnavailable
	case issuerGateReasonRemoteSessionMisconfigured, issuerGateReasonClientCredentialMisconfigured:
		return mcpconnectionattempts.CodeCredentialMisconfigured
	default:
		return mcpconnectionattempts.CodeInvalidRemoteSession
	}
}

// directConnectionOptions files a directly served proxy's refused and failed tool calls under its server.
func directConnectionOptions(ctx context.Context, organizationID string, projectID, serverID uuid.UUID, upstream remotesessions.UpstreamToken) []remotemcp.BuildOption {
	return []remotemcp.BuildOption{
		remotemcp.WithConnectionScope(mcpconnectionattempts.ServerScope(organizationID, projectID, serverID, requestApp(ctx)), callerSubject(ctx)),
		remotemcp.WithConnectionUpstreamFailures(upstream.CredentialOwner == remotesessions.CredentialOwnerSelf),
	}
}

// callerSubject is the signed-in user of an authenticated request, when there is one.
func callerSubject(ctx context.Context) mcpconnectionattempts.Option {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.UserID == "" {
		return mcpconnectionattempts.Subject(nil)
	}
	subject := urn.NewUserSubject(authCtx.UserID)
	return mcpconnectionattempts.Subject(&subject)
}

// connectionScope files a gateway's rows under the gateway, with the member as a detail.
func (g *metaGateContext) connectionScope(ctx context.Context) mcpconnectionattempts.Scope {
	if g.organizationGateway {
		return mcpconnectionattempts.OrganizationGatewayScope(g.organizationID, g.metaServerID, requestApp(ctx))
	}
	return mcpconnectionattempts.GatewayScope(g.organizationID, g.projectID, g.metaServerID, requestApp(ctx))
}

func (g *metaGateContext) connectionSubject(ctx context.Context) mcpconnectionattempts.Option {
	if g.authentication != nil {
		return mcpconnectionattempts.Subject(&g.authentication.subject)
	}
	return callerSubject(ctx)
}

// memberConnectionOption files a member proxy's refused tool calls under its gateway.
func memberConnectionOption(ctx context.Context, gate *metaGateContext, member metaMember) remotemcp.BuildOption {
	return remotemcp.WithConnectionScope(gate.connectionScope(ctx), mcpconnectionattempts.Member(member.serverID), gate.connectionSubject(ctx))
}

// recordMemberFailure records a gateway member's failed tool call. A failure with no code writes nothing.
func (s *Service) recordMemberFailure(ctx context.Context, gate *metaGateContext, member metaMember, what mcpconnectionattempts.Code) {
	s.connections.Once(ctx, gate.connectionScope(ctx), mcpconnectionattempts.StepMCPRequest, what, mcpconnectionattempts.Member(member.serverID), gate.connectionSubject(ctx))
}

// memberConnectionError carries what a member's failure means, without changing its text or type.
type memberConnectionError struct {
	error

	code mcpconnectionattempts.Code
}

func (f memberConnectionError) Unwrap() error { return f.error }

// recordHostedToolsCallRefusal records a refused hosted tools/call. Any other error writes nothing.
func (s *Service) recordHostedToolsCallRefusal(ctx context.Context, payload *mcpInputs, err error) {
	code, ok := toolsCallRefusal(err)
	if !ok || payload.mcpServerID == nil {
		return
	}
	scope := mcpconnectionattempts.ServerScope(payload.organizationID, payload.projectID, *payload.mcpServerID, requestApp(ctx))
	s.connections.Once(ctx, scope, mcpconnectionattempts.StepMCPRequest, code, callerSubject(ctx))
}

func toolsCallRefusal(err error) (mcpconnectionattempts.Code, bool) {
	var none mcpconnectionattempts.Code
	if err == nil {
		return none, false
	}
	switch {
	case errors.Is(err, errToolUsageLimitReached):
		return mcpconnectionattempts.CodeUsageLimitReached, true
	case errors.Is(err, errRiskPolicyDenied):
		return mcpconnectionattempts.CodePolicyBlocked, true
	}
	if shareable, ok := errors.AsType[*oops.ShareableError](err); ok && shareable.Error() == mcpaccess.ToolPermissionDeniedMessage {
		return mcpconnectionattempts.CodeToolPermissionDenied, true
	}
	return none, false
}

func killswitchHappened(err error) mcpconnectionattempts.Code {
	if mcpErr, ok := errors.AsType[*oops.MCPError](err); ok && mcpErr.Data != nil && mcpErr.Data.Code == oops.MCPErrorDataCodeToolCallsPaused {
		return mcpconnectionattempts.CodeMCPToolCallsPaused
	}
	return mcpconnectionattempts.CodeKillswitchCheckUnavailable
}

// serverAccessDenied reports a forbidden mcp:connect, as opposed to a failed check.
func serverAccessDenied(err error) bool {
	shareable, ok := errors.AsType[*oops.ShareableError](err)
	return ok && shareable.Code == oops.CodeForbidden && strings.HasPrefix(shareable.Error(), mcpaccess.ServerPermissionDeniedMessage)
}

// upstreamRefreshCheck is what refresh learned about the upstream behind a
// session. Its values name the reason the refresh row records.
type upstreamRefreshCheck string

const (
	// upstreamRefreshServed: the upstream is usable, or the session needs none.
	upstreamRefreshServed upstreamRefreshCheck = ""

	// upstreamRefreshUnverified: the check could not answer, so the refresh proceeds.
	upstreamRefreshUnverified upstreamRefreshCheck = "refreshed_unverified"

	// upstreamRefreshReconnect: the person has to reconnect the upstream.
	upstreamRefreshReconnect upstreamRefreshCheck = "upstream_reconnect_required"

	// upstreamRefreshEMARefused: the provider refused managed sign-in for good.
	upstreamRefreshEMARefused upstreamRefreshCheck = "upstream_ema_refused"
)

func upstreamRefreshOf(reconnect bool) upstreamRefreshCheck {
	if reconnect {
		return upstreamRefreshReconnect
	}
	return upstreamRefreshServed
}

func (c upstreamRefreshCheck) reconnect() bool {
	return c == upstreamRefreshReconnect || c == upstreamRefreshEMARefused
}

func (s *Service) recordInboundRefreshed(ctx context.Context, endpoint *ResolvedMcpEndpoint, client *usersessions_repo.UserSessionClient, check upstreamRefreshCheck, subject *urn.SessionSubject) {
	var what mcpconnectionattempts.Happened = mcpconnectionattempts.ResultRefreshed
	if check == upstreamRefreshUnverified {
		what = mcpconnectionattempts.CodeRefreshedUnverified
	}
	s.connections.Once(ctx, connectionScope(endpoint, mcpconnectionattempts.AppOf(client.ClientID)), mcpconnectionattempts.StepInboundToken, what,
		mcpconnectionattempts.Grant(oauthwire.GrantTypeRefreshToken), mcpconnectionattempts.UserSessionClient(client.ID), mcpconnectionattempts.UserSessionIssuer(endpoint.UserSessionIssuerID), mcpconnectionattempts.Subject(subject))
}

// recordInboundRefreshRejection records a refused refresh under the reason its log line carries.
func (s *Service) recordInboundRefreshRejection(ctx context.Context, endpoint *ResolvedMcpEndpoint, client *usersessions_repo.UserSessionClient, reason string, opts ...mcpconnectionattempts.Option) {
	code, ok := inboundRefreshHappened(reason)
	if !ok {
		return
	}
	opts = append(opts, mcpconnectionattempts.Grant(oauthwire.GrantTypeRefreshToken), mcpconnectionattempts.UserSessionClient(client.ID), mcpconnectionattempts.UserSessionIssuer(endpoint.UserSessionIssuerID), mcpconnectionattempts.Detail(reason))
	s.connections.Once(ctx, connectionScope(endpoint, mcpconnectionattempts.AppOf(client.ClientID)), mcpconnectionattempts.StepInboundToken, code, opts...)
}

func inboundRefreshHappened(reason string) (mcpconnectionattempts.Code, bool) {
	switch reason {
	case "invalid_request":
		return mcpconnectionattempts.CodeInvalidRequest, true
	case "resource_mismatch", "refresh_token_resource_mismatch":
		return mcpconnectionattempts.CodeResourceMismatch, true
	case "refresh_token_unknown_or_already_used", "refresh_token_client_mismatch":
		return mcpconnectionattempts.CodeRefreshTokenUnknownOrAlreadyUsed, true
	case "refresh_token_expired":
		return mcpconnectionattempts.CodeRefreshTokenExpired, true
	case "sign_in_revoked":
		return mcpconnectionattempts.CodeSignInRevoked, true
	case "refresh_token_revoked", "refresh_token_replay_session_mismatch", "refresh_token_replay_rejected", "tool_selection_malformed", "tool_selection_resource_mismatch":
		return mcpconnectionattempts.CodeRefreshTokenRevoked, true
	case "agent_admission_denied":
		return mcpconnectionattempts.CodeNoServerAccess, true
	case string(upstreamRefreshReconnect):
		return mcpconnectionattempts.CodeUpstreamReconnectRequired, true
	case string(upstreamRefreshEMARefused):
		return mcpconnectionattempts.CodeUpstreamEMARefused, true
	case "sign_in_lookup_failed":
		return mcpconnectionattempts.CodeInternalError, true
	}
	var none mcpconnectionattempts.Code
	return none, false
}
