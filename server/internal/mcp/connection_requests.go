package mcp

import (
	"context"
	"errors"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcpaccess"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotemcp"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// Per-request rows record refusals and failures only. A request that is served
// writes nothing: at tool-call rates, success rows would fill the recorder's
// write budget and leave the failures to drop.

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

// recordGateRejection records a bearer the gate refused, once it proves a real
// flow on this server. No bearer and an expired one are how a healthy client
// learns to sign in or refresh. A bearer that is not ours, or was minted for
// another server, could come from anyone. A caller going away says nothing.
func (s *Service) recordGateRejection(ctx context.Context, endpoint *ResolvedMcpEndpoint, token, reason string, valErr error) {
	if valErr == nil || errors.Is(valErr, jwt.ErrTokenExpired) || errors.Is(valErr, jwt.ErrTokenInvalidAudience) || errors.Is(ctx.Err(), context.Canceled) {
		return
	}
	code := gateHappened(valErr)
	opts := []mcpconnectionattempts.Option{mcpconnectionattempts.Detail(reason)}
	subject, verifyErr := s.userSessionSigner.VerifiedSubject(token)
	switch {
	case verifyErr == nil:
		opts = append(opts, mcpconnectionattempts.Subject(&subject))
	case code == mcpconnectionattempts.CodeInvalidBearerToken:
		return
	}
	s.connections.Once(ctx, connectionScope(endpoint, mcpconnectionattempts.AppUnverified), mcpconnectionattempts.StepMCPRequest, code, opts...)
}

// gateHappened keeps an outage on our side from reading as a person's bad token.
func gateHappened(valErr error) mcpconnectionattempts.Code {
	if errors.Is(valErr, errWorkloadSessionOutOfReach) {
		return mcpconnectionattempts.CodeNoServerAccess
	}
	if shareable, ok := errors.AsType[*oops.ShareableError](valErr); ok && shareable.Code == oops.CodeForbidden {
		return mcpconnectionattempts.CodeNoServerAccess
	}
	if errors.Is(valErr, errSessionPolicyGone) || errors.Is(valErr, errCredentialRejected) || errors.Is(valErr, errToolSelectionResourceMismatch) ||
		errors.Is(valErr, errTokenHostMismatch) || errors.Is(valErr, errSharedTokenIssuerMismatch) || isCredentialDenial(valErr) {
		return mcpconnectionattempts.CodeInvalidBearerToken
	}
	return mcpconnectionattempts.CodeAuthCheckUnavailable
}

// directConnectionOptions records a directly served proxy's tool calls under its
// server, for a caller who presented a credential. An anonymous caller on a public
// server proves no flow, so those calls stay in the proxy's metrics.
func directConnectionOptions(ctx context.Context, organizationID string, projectID, serverID uuid.UUID, upstream remotesessions.UpstreamToken) []remotemcp.BuildOption {
	if authCtx, ok := contextvalues.GetAuthContext(ctx); !ok || authCtx == nil {
		return nil
	}
	return []remotemcp.BuildOption{remotemcp.WithConnectionAttribution(remotemcp.ConnectionAttribution{
		Scope:            mcpconnectionattempts.ServerScope(organizationID, projectID, serverID, requestApp(ctx)),
		Details:          []mcpconnectionattempts.Option{callerSubject(ctx)},
		UpstreamFailures: true,
		SelfCredential:   upstream.CredentialOwner == remotesessions.CredentialOwnerSelf,
	})}
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

// refreshAttribution files a request's refreshes under the server it reached.
func (a *issuerGateAuthentication) refreshAttribution(ctx context.Context) remotesessions.RefreshAttribution {
	return remotesessions.RefreshIn(connectionScope(a.endpoint, requestApp(ctx))).NotingSkips(a.skippedUpstreams)
}

// consentRefreshAttribution makes a refresh during sign-in a step of that sign-in.
func consentRefreshAttribution(endpoint *ResolvedMcpEndpoint, state AuthnChallengeState) remotesessions.RefreshAttribution {
	return remotesessions.RefreshIn(connectionScope(endpoint, mcpconnectionattempts.AppOf(state.ClientID))).InAttempt(state.connectionAttempt(endpoint))
}

// connectionScope files a gateway's rows under the gateway, with the member as a detail.
func (g *metaGateContext) connectionScope(ctx context.Context) mcpconnectionattempts.Scope {
	if g.projectID == uuid.Nil {
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

// authClientGapHappened separates a setup an admin has to finish from a choice the person can redo.
func authClientGapHappened(reason string) mcpconnectionattempts.Code {
	if reason == authClientReasonSetup {
		return mcpconnectionattempts.CodeCredentialMisconfigured
	}
	return mcpconnectionattempts.CodeInvalidRemoteSession
}

// memberConnectionOption records a member proxy's refused tool calls under its gateway.
func memberConnectionOption(ctx context.Context, gate *metaGateContext, member metaMember) remotemcp.BuildOption {
	return remotemcp.WithConnectionAttribution(remotemcp.ConnectionAttribution{
		Scope:            gate.connectionScope(ctx),
		Details:          []mcpconnectionattempts.Option{mcpconnectionattempts.Member(member.serverID), gate.connectionSubject(ctx)},
		UpstreamFailures: false,
		SelfCredential:   false,
	})
}

// recordMemberFailure records one gateway member's failed tool call.
func (s *Service) recordMemberFailure(ctx context.Context, gate *metaGateContext, member metaMember, what mcpconnectionattempts.Code) {
	if errors.Is(ctx.Err(), context.Canceled) {
		return
	}
	s.connections.Once(ctx, gate.connectionScope(ctx), mcpconnectionattempts.StepMCPRequest, what, mcpconnectionattempts.Member(member.serverID), gate.connectionSubject(ctx))
}

// memberNoCredentialHappened says why a member had no credential to send: its
// setup, or why partial resolution left its client out. Otherwise the person has to connect it.
func (g *metaGateContext) memberNoCredentialHappened(member metaMember) mcpconnectionattempts.Code {
	if member.authClient != nil && member.authClient.outcome == authClientGap {
		return authClientGapHappened(member.authClient.reason)
	}
	var skipped *remotesessions.SkippedClients
	if g.authentication != nil {
		skipped = g.authentication.skippedUpstreams
	}
	reason := skipped.Reason(member.remoteSessionIssuerID.UUID)
	switch {
	case errors.Is(reason, remotesessions.ErrRemoteSessionUnavailable):
		return mcpconnectionattempts.CodeRemoteSessionUnavailable
	case errors.Is(reason, remotesessions.ErrRemoteSessionMisconfigured):
		return mcpconnectionattempts.CodeCredentialMisconfigured
	}
	return mcpconnectionattempts.CodeInvalidRemoteSession
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
	if !ok || payload.mcpServerID == nil || errors.Is(ctx.Err(), context.Canceled) {
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
	case errors.Is(err, remotemcp.ErrToolUsageLimitReached):
		return mcpconnectionattempts.CodeUsageLimitReached, true
	case errors.Is(err, errRiskPolicyDenied):
		return mcpconnectionattempts.CodePolicyBlocked, true
	}
	if mcpaccess.IsToolPermissionDenied(err) {
		return mcpconnectionattempts.CodeToolPermissionDenied, true
	}
	return none, false
}

// agentAdmissionRejection tells a refused agent from an admission check that could not run.
func agentAdmissionRejection(err error) string {
	if shareable, ok := errors.AsType[*oops.ShareableError](err); ok && (shareable.Code == oops.CodeUnauthorized || shareable.Code == oops.CodeForbidden) {
		return "agent_admission_denied"
	}
	return "agent_admission_unavailable"
}

func killswitchHappened(err error) mcpconnectionattempts.Code {
	if mcpErr, ok := errors.AsType[*oops.MCPError](err); ok && mcpErr.Data != nil && mcpErr.Data.Code == oops.MCPErrorDataCodeToolCallsPaused {
		return mcpconnectionattempts.CodeMCPToolCallsPaused
	}
	return mcpconnectionattempts.CodeKillswitchCheckUnavailable
}

// upstreamRefreshCheck is what refresh learned about the upstream behind a
// session. Its values name the reason the refresh row records.
type upstreamRefreshCheck string

const (
	// upstreamRefreshServed: the upstream is usable, or the session needs none.
	upstreamRefreshServed upstreamRefreshCheck = ""

	// upstreamRefreshUnverified: identity chaining could not confirm the upstream.
	upstreamRefreshUnverified upstreamRefreshCheck = "refreshed_unverified"

	// upstreamRefreshCheckFailed: our own read failed, so the refresh proceeds unjudged.
	upstreamRefreshCheckFailed upstreamRefreshCheck = "upstream_check_failed"

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

// refuseInboundRefresh gives a refused refresh its verdict from the reason its log line carries.
func refuseInboundRefresh(ctx context.Context, tracker *mcpconnectionattempts.Tracker, reason string, opts ...mcpconnectionattempts.Option) {
	code, ok := inboundRefreshHappened(reason)
	if !ok {
		tracker.Continue()
		return
	}
	tracker.Refuse(ctx, code, append(opts, mcpconnectionattempts.Detail(reason))...)
}

// inboundRefreshHappened leaves out what proves no real session: a malformed
// request, one naming another server, and a token no live session holds. An unknown reason is ours.
func inboundRefreshHappened(reason string) (mcpconnectionattempts.Code, bool) {
	switch reason {
	case "refresh_token_resource_mismatch":
		return mcpconnectionattempts.CodeResourceMismatch, true
	case "refresh_token_client_mismatch":
		return mcpconnectionattempts.CodeRefreshTokenUnknownOrAlreadyUsed, true
	case "refresh_token_expired":
		return mcpconnectionattempts.CodeRefreshTokenExpired, true
	case "sign_in_revoked":
		return mcpconnectionattempts.CodeSignInRevoked, true
	case "refresh_token_revoked", "refresh_token_replay_session_mismatch", "refresh_token_replay_rejected", "tool_selection_malformed", "tool_selection_resource_mismatch":
		return mcpconnectionattempts.CodeRefreshTokenRevoked, true
	case "agent_admission_denied":
		return mcpconnectionattempts.CodeNoServerAccess, true
	case "agent_admission_unavailable":
		return mcpconnectionattempts.CodeInternalError, true
	case string(upstreamRefreshReconnect):
		return mcpconnectionattempts.CodeUpstreamReconnectRequired, true
	case string(upstreamRefreshEMARefused):
		return mcpconnectionattempts.CodeUpstreamEMARefused, true
	case "invalid_request", "resource_mismatch", "refresh_token_unknown_or_already_used":
		var none mcpconnectionattempts.Code
		return none, false
	default:
		return mcpconnectionattempts.CodeInternalError, true
	}
}
