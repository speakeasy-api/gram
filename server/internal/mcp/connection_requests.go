package mcp

import (
	"context"
	"errors"
	"net/http"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcpaccess"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotemcp"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// Per-request rows record refusals and failures only: a served request writes nothing.

// errRiskPolicyDenied marks a tools/call a risk policy stopped.
var errRiskPolicyDenied = errors.New("risk policy denied the call")

// reportedClient names the client a request's MCP session reported, for a row. The writer does the
// lookup. Only a session the client sent counts: a generated id has no record.
func (s *Service) reportedClient(ctx context.Context, r *http.Request, projectID uuid.UUID, scope string) mcpconnectionattempts.Option {
	if info, ok := contextvalues.GetMCPClientInfo(ctx); ok && info.Name != "" {
		return mcpconnectionattempts.ReportedClient(func() (string, string) { return info.Name, info.Version })
	}
	if r == nil || scope == "" || s.tunnelClientFamilies == nil {
		return mcpconnectionattempts.ReportedClient(nil)
	}
	sessionID := r.Header.Get(proxy.McpSessionIDHeader)
	if sessionID == "" {
		return mcpconnectionattempts.ReportedClient(nil)
	}
	return mcpconnectionattempts.ReportedClient(func() (string, string) {
		return s.tunnelClientFamilies.forRow(s.logger, projectID, scope, sessionID)
	})
}

// gatewayReportedClient is reportedClient for a gateway's own session record.
func (s *Service) gatewayReportedClient(ctx context.Context, gate *metaGateContext) mcpconnectionattempts.Option {
	return s.reportedClient(ctx, gate.request, uuid.Nil, metaClientInfoScope(gate.metaServerID))
}

// recordGateRejection attributes invalid bearers only when their signature proves a subject.
func (s *Service) recordGateRejection(ctx context.Context, r *http.Request, endpoint *ResolvedMcpEndpoint, token, reason string, valErr error) {
	if valErr == nil || errors.Is(valErr, jwt.ErrTokenExpired) || errors.Is(ctx.Err(), context.Canceled) {
		return
	}
	code := gateHappened(valErr)
	opts := []mcpconnectionattempts.Option{mcpconnectionattempts.Detail(reason), mcpconnectionattempts.FromRequest(r), s.reportedClient(ctx, r, endpoint.clientInfoProjectID(), endpoint.clientInfoScope)}
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
	if errors.Is(valErr, jwt.ErrTokenExpired) || errors.Is(valErr, jwt.ErrTokenInvalidAudience) ||
		errors.Is(valErr, errSessionPolicyGone) || errors.Is(valErr, errCredentialRejected) || errors.Is(valErr, errToolSelectionResourceMismatch) ||
		errors.Is(valErr, errTokenHostMismatch) || errors.Is(valErr, errSharedTokenIssuerMismatch) || isCredentialDenial(valErr) {
		return mcpconnectionattempts.CodeInvalidBearerToken
	}
	return mcpconnectionattempts.CodeAuthCheckUnavailable
}

// directConnectionOptions records a caller who presented a credential; anonymous calls stay in metrics.
func directConnectionOptions(ctx context.Context, r *http.Request, organizationID string, projectID, serverID uuid.UUID, upstream remotesessions.UpstreamToken, client mcpconnectionattempts.Option) []remotemcp.BuildOption {
	if authCtx, ok := contextvalues.GetAuthContext(ctx); !ok || authCtx == nil {
		return nil
	}
	return []remotemcp.BuildOption{remotemcp.WithConnectionAttribution(remotemcp.ConnectionAttribution{
		Scope:            mcpconnectionattempts.ServerScope(organizationID, projectID, serverID, requestApp(ctx)),
		Details:          []mcpconnectionattempts.Option{callerSubject(ctx), client},
		UpstreamFailures: true,
		SelfCredential:   upstream.CredentialOwner == remotesessions.CredentialOwnerSelf,
		Request:          r,
	})}
}

// callerSubject preserves the authenticated actor, not the human authorizing an agent.
func callerSubject(ctx context.Context) mcpconnectionattempts.Option {
	if actor, ok := contextvalues.AuthenticatedActor(ctx); ok {
		subject, err := urn.ParseSessionSubject(actor.String())
		if err == nil {
			return mcpconnectionattempts.Subject(&subject)
		}
		return mcpconnectionattempts.Subject(nil)
	}
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.UserID == "" {
		return mcpconnectionattempts.Subject(nil)
	}
	subject := urn.NewUserSubject(authCtx.UserID)
	return mcpconnectionattempts.Subject(&subject)
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
func (s *Service) memberConnectionOption(ctx context.Context, gate *metaGateContext, member metaMember) remotemcp.BuildOption {
	return remotemcp.WithConnectionAttribution(remotemcp.ConnectionAttribution{
		Scope:            gate.connectionScope(ctx),
		Details:          []mcpconnectionattempts.Option{mcpconnectionattempts.Member(member.serverID), gate.connectionSubject(ctx), s.gatewayReportedClient(ctx, gate)},
		UpstreamFailures: false,
		SelfCredential:   false,
		Request:          gate.request,
	})
}

// recordMemberFailure records one gateway member's failed tool call.
func (s *Service) recordMemberFailure(ctx context.Context, gate *metaGateContext, member metaMember, what mcpconnectionattempts.Code) {
	if what == (mcpconnectionattempts.Code{}) || errors.Is(ctx.Err(), context.Canceled) {
		return
	}
	s.connections.Once(ctx, gate.connectionScope(ctx), mcpconnectionattempts.StepMCPRequest, what, mcpconnectionattempts.Member(member.serverID), gate.connectionSubject(ctx), mcpconnectionattempts.FromRequest(gate.request), s.gatewayReportedClient(ctx, gate))
}

// resolveFailureHappened says who fixes a member credential that could not be resolved.
func resolveFailureHappened(err error) mcpconnectionattempts.Code {
	switch {
	case errors.Is(err, remotesessions.ErrRemoteSessionUnavailable):
		if remotesessions.RefreshBusy(err) {
			return mcpconnectionattempts.Code{}
		}
		if remotesessions.TokenEndpointCausedOutage(err) {
			return mcpconnectionattempts.CodeRemoteSessionUnavailable
		}
		return remotesessions.ConnectionOutageHappened(err)
	case errors.Is(err, remotesessions.ErrRemoteSessionMisconfigured), errors.Is(err, remotesessions.ErrClientCredentialMisconfigured):
		return mcpconnectionattempts.CodeCredentialMisconfigured
	case errors.Is(err, remotesessions.ErrNoValidToken):
		return mcpconnectionattempts.CodeInvalidRemoteSession
	}
	return mcpconnectionattempts.CodeInternalError
}

// memberNoCredentialHappened says why a member had no credential to send.
func (g *metaGateContext) memberNoCredentialHappened(member metaMember) mcpconnectionattempts.Code {
	if member.authClient != nil {
		switch member.authClient.outcome {
		case authClientGap:
			return authClientGapHappened(member.authClient.reason)
		case authClientNoAccess:
			return mcpconnectionattempts.CodeNoServerAccess
		case authClientLegacy, authClientNoAuth, authClientResolved:
		}
	}
	if reason := g.skippedUpstreams.Reason(member.projectID, member.remoteSessionIssuerID.UUID, member.chosenClientID); reason != nil {
		return resolveFailureHappened(reason)
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
	s.connections.Once(ctx, scope, mcpconnectionattempts.StepMCPRequest, code, callerSubject(ctx), mcpconnectionattempts.FromRequest(payload.request), s.reportedClient(ctx, payload.request, sessionClientInfoProjectID(payload), sessionClientInfoScope(payload)))
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

func killswitchHappened(err error) mcpconnectionattempts.Code {
	if mcpErr, ok := errors.AsType[*oops.MCPError](err); ok && mcpErr.Data != nil && mcpErr.Data.Code == oops.MCPErrorDataCodeToolCallsPaused {
		return mcpconnectionattempts.CodeMCPToolCallsPaused
	}
	return mcpconnectionattempts.CodeKillswitchCheckUnavailable
}
