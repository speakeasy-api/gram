package mcp

import (
	"context"
	"errors"
	"net/http"
	"sync/atomic"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/aivendors"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpversions"
	"github.com/speakeasy-api/gram/server/internal/mcpaccess"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotemcp"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	toolsets_repo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// Per-request rows record refusals and failures. A served request is kept apart, once per connection, by Recorder.Served.

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

// recordRequestOutcome writes one request's refusal or failure, unless there is none or the caller left.
func (s *Service) recordRequestOutcome(ctx context.Context, scope mcpconnectionattempts.Scope, code mcpconnectionattempts.Code, r *http.Request, opts ...mcpconnectionattempts.Option) {
	if code == (mcpconnectionattempts.Code{}) || errors.Is(ctx.Err(), context.Canceled) {
		return
	}
	s.connections.Once(ctx, scope, mcpconnectionattempts.StepMCPRequest, code, append(opts, mcpconnectionattempts.FromRequest(r))...)
}

// recordGateRejection attributes invalid bearers only when their signature proves a subject.
func (s *Service) recordGateRejection(ctx context.Context, r *http.Request, endpoint *ResolvedMcpEndpoint, token, reason string, valErr error) {
	if valErr == nil || errors.Is(valErr, jwt.ErrTokenExpired) {
		return
	}
	code := gateHappened(valErr)
	opts := []mcpconnectionattempts.Option{mcpconnectionattempts.Detail(reason), s.reportedClient(ctx, r, endpoint.clientInfoProjectID(), endpoint.clientInfoScope)}
	subject, verifyErr := s.userSessionSigner.VerifiedSubject(token)
	switch {
	case verifyErr == nil:
		opts = append(opts, mcpconnectionattempts.Subject(&subject))
	case code == mcpconnectionattempts.CodeInvalidBearerToken:
		return
	}
	s.recordRequestOutcome(ctx, connectionScope(endpoint, mcpconnectionattempts.AppUnverified), code, r, opts...)
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
func directConnectionOptions(ctx context.Context, catalog *aivendors.Snapshot, r *http.Request, organizationID string, projectID, serverID uuid.UUID, upstream remotesessions.UpstreamToken, client mcpconnectionattempts.Option) []remotemcp.BuildOption {
	if authCtx, ok := contextvalues.GetAuthContext(ctx); !ok || authCtx == nil {
		return nil
	}
	return []remotemcp.BuildOption{remotemcp.WithConnectionAttribution(remotemcp.ConnectionAttribution{
		Scope:            mcpconnectionattempts.ServerScope(organizationID, projectID, serverID, requestApp(ctx, catalog)),
		Details:          []mcpconnectionattempts.Option{callerSubject(ctx), client, mcpconnectionattempts.RemoteSessionClient(ownSignInClient(upstream))},
		UpstreamOutcomes: true,
		SelfCredential:   upstream.CredentialOwner == remotesessions.CredentialOwnerSelf,
		Request:          r,
	})}
}

// ownSignInClient is the service client of the person's own stored sign-in the call sends, else uuid.Nil.
func ownSignInClient(upstream remotesessions.UpstreamToken) uuid.UUID {
	if upstream.Token == "" || upstream.CredentialOwner == remotesessions.CredentialOwnerSelf {
		return uuid.Nil
	}
	return upstream.RemoteSessionClientID
}

// callerSubject preserves the authenticated actor, not the human authorizing an agent.
func callerSubject(ctx context.Context) mcpconnectionattempts.Option {
	return mcpconnectionattempts.Subject(resolvedCaller(ctx))
}

// resolvedCaller is the authenticated actor, else the user, else the API key of a session whose
// subject is one, as the gate's rows name it. Nil when none resolves.
func resolvedCaller(ctx context.Context) *urn.SessionSubject {
	if actor, ok := contextvalues.AuthenticatedActor(ctx); ok {
		subject, err := urn.ParseSessionSubject(actor.String())
		if err == nil {
			return &subject
		}
		return nil
	}
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil
	}
	if authCtx.UserID != "" {
		subject := urn.NewUserSubject(authCtx.UserID)
		return &subject
	}
	if keyID, err := uuid.Parse(authCtx.APIKeyID); err == nil {
		subject := urn.NewAPIKeySubject(keyID)
		return &subject
	}
	return nil
}

func (g *metaGateContext) connectionSubject(ctx context.Context) mcpconnectionattempts.Option {
	return mcpconnectionattempts.Subject(g.resolvedSubject(ctx))
}

// resolvedSubject is the gateway's authenticated subject, or the caller's when it has none.
func (g *metaGateContext) resolvedSubject(ctx context.Context) *urn.SessionSubject {
	if g.authentication != nil {
		return &g.authentication.subject
	}
	return resolvedCaller(ctx)
}

// authClientGapHappened separates a setup an admin has to finish from a choice the person can redo.
func authClientGapHappened(reason string) mcpconnectionattempts.Code {
	if reason == authClientReasonSetup {
		return mcpconnectionattempts.CodeCredentialMisconfigured
	}
	return mcpconnectionattempts.CodeInvalidRemoteSession
}

// memberConnection names a member's connection on every row, with the service client of the person's own sign-in when one is sent.
func (s *Service) memberConnection(ctx context.Context, gate *metaGateContext, member metaMember, serviceClient uuid.UUID) []mcpconnectionattempts.Option {
	return []mcpconnectionattempts.Option{mcpconnectionattempts.Member(member.serverID), gate.connectionSubject(ctx), s.gatewayReportedClient(ctx, gate), mcpconnectionattempts.RemoteSessionClient(serviceClient)}
}

// memberConnectionOption records a member proxy's refused tool calls under its gateway.
func (s *Service) memberConnectionOption(ctx context.Context, gate *metaGateContext, member metaMember, serviceClient uuid.UUID) remotemcp.BuildOption {
	return remotemcp.WithConnectionAttribution(remotemcp.ConnectionAttribution{
		Scope:            gate.connectionScope(ctx, s.aiCatalog.SnapshotFor(ctx)),
		Details:          s.memberConnection(ctx, gate, member, serviceClient),
		UpstreamOutcomes: false,
		SelfCredential:   false,
		Request:          gate.request,
	})
}

// recordMemberFailure records a member's failed tool call, refining a missing connection to its cause. serviceClient
// is the service client of the person's own sign-in the call sent, uuid.Nil for none.
func (s *Service) recordMemberFailure(ctx context.Context, gate *metaGateContext, member metaMember, serviceClient uuid.UUID, what mcpconnectionattempts.Code, opts ...mcpconnectionattempts.Option) {
	if what == mcpconnectionattempts.CodeInvalidRemoteSession {
		what = gate.memberNoCredentialHappened(member)
	}
	// Kept beside a provider_refresh row: only this row carries member_id.
	s.recordRequestOutcome(ctx, gate.connectionScope(ctx, s.aiCatalog.SnapshotFor(ctx)), what, gate.request, append(s.memberConnection(ctx, gate, member, serviceClient), opts...)...)
}

// recordMemberServed records a member call its own server answered without an error, on the connection its failures name.
func (s *Service) recordMemberServed(ctx context.Context, gate *metaGateContext, member metaMember, serviceClient uuid.UUID) {
	s.connections.Served(ctx, gate.connectionScope(ctx, s.aiCatalog.SnapshotFor(ctx)), true, append(s.memberConnection(ctx, gate, member, serviceClient), mcpconnectionattempts.FromRequest(gate.request))...)
}

// gatewayMemberCallsKey holds a gateway request's member tool calls.
type gatewayMemberCallsKey struct{}

// gatewayMemberCalls counts a gateway request's member tool calls and those whose result was not an error.
type gatewayMemberCalls struct{ called, succeeded atomic.Int32 }

// withGatewayMemberCalls starts counting the member tool calls of the gateway request on ctx.
func withGatewayMemberCalls(ctx context.Context) context.Context {
	return context.WithValue(ctx, gatewayMemberCallsKey{}, &gatewayMemberCalls{called: atomic.Int32{}, succeeded: atomic.Int32{}})
}

// noteGatewayMemberCall counts a member tool call of the request. A Code Mode program's calls are not the request's own.
func noteGatewayMemberCall(ctx context.Context, succeeded bool) {
	calls, ok := ctx.Value(gatewayMemberCallsKey{}).(*gatewayMemberCalls)
	if !ok || ctx.Value(metaCodeInvocationKey{}) != nil {
		return
	}
	calls.called.Add(1)
	if succeeded {
		calls.succeeded.Add(1)
	}
}

// recordGatewayServed records a gateway request served without an error. Speakeasy answered it,
// so it names no member: a member call also writes its own row. A member tool call that failed or
// returned an error result writes nothing.
func (s *Service) recordGatewayServed(ctx context.Context, gate *metaGateContext) {
	if calls, ok := ctx.Value(gatewayMemberCallsKey{}).(*gatewayMemberCalls); ok && calls.called.Load() > 0 && calls.succeeded.Load() == 0 {
		return
	}
	s.connections.Served(ctx, gate.connectionScope(ctx, s.aiCatalog.SnapshotFor(ctx)), false, gate.connectionSubject(ctx), s.gatewayReportedClient(ctx, gate), mcpconnectionattempts.FromRequest(gate.request))
}

// consentRefusalHappened is the code for whoever a consent refusal's message tells to act first.
// An unchecked remedy is our failed lookup.
func consentRefusalHappened(remedy staleChoiceRemedy) mcpconnectionattempts.Code {
	switch remedy {
	case staleChoiceAskAdmin:
		return mcpconnectionattempts.CodeApprovalNeedsAdmin
	case staleChoiceUnchecked:
		return mcpconnectionattempts.CodeInternalError
	case staleChoiceReconnect, staleChoiceReconnectThenAdmin:
	}
	return mcpconnectionattempts.CodeConsentNotApproved
}

// resolveFailureHappened says who fixes a member credential that could not be resolved.
func resolveFailureHappened(err error) mcpconnectionattempts.Code {
	refused, chainingRefused := errors.AsType[*chainingRefusedError](err)
	switch {
	// An outage clears by itself, so it outranks a refusal it wraps.
	case errors.Is(err, remotesessions.ErrRemoteSessionUnavailable):
		return remotesessions.ConnectionOutageHappened(err)
	case chainingRefused:
		return refused.code
	case errors.Is(err, remotesessions.ErrRemoteSessionMisconfigured), errors.Is(err, remotesessions.ErrClientCredentialMisconfigured):
		return mcpconnectionattempts.CodeCredentialMisconfigured
	case errors.Is(err, remotesessions.ErrStoredSignInUnreadable):
		return mcpconnectionattempts.CodeStoredSignInUnreadable
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

// recordHostedToolsCallOutcome records a refused hosted tools/call or one whose provider rejected
// its credential. Any other outcome writes nothing.
func (s *Service) recordHostedToolsCallOutcome(ctx context.Context, payload *mcpInputs, err error) {
	code, refused := toolsCallRefusal(err)
	if !refused {
		code = payload.upstreamRejection
	}
	if code == (mcpconnectionattempts.Code{}) || payload.mcpServerID == nil {
		return
	}
	// An anonymous caller's rejected bearer says nothing about a person's access.
	if code == mcpconnectionattempts.CodeProviderRejected && resolvedCaller(ctx) == nil {
		return
	}
	scope := mcpconnectionattempts.ServerScope(payload.organizationID, payload.projectID, *payload.mcpServerID, requestApp(ctx, s.aiCatalog.SnapshotFor(ctx)))
	s.recordRequestOutcome(ctx, scope, code, payload.request, callerSubject(ctx), s.reportedClient(ctx, payload.request, sessionClientInfoProjectID(payload), sessionClientInfoScope(payload)))
}

// recordHostedServed records a hosted request served without an error, under the scope its
// failures use: its fronting server, else its toolset. A tool call or resource read counts only
// when its result is not an error.
func (s *Service) recordHostedServed(ctx context.Context, toolset *toolsets_repo.Toolset, cfg *hostedServing, payload *mcpInputs, method string) {
	if (method == mcpversions.MethodToolsCall || method == mcpversions.MethodResourcesRead) && !payload.resultSucceeded {
		return
	}
	s.connections.Served(ctx, hostedConnectionScope(ctx, s.aiCatalog.SnapshotFor(ctx), toolset, cfg), payload.backendAnswered,
		callerSubject(ctx), s.reportedClient(ctx, payload.request, toolset.ProjectID, toolset.Slug), mcpconnectionattempts.FromRequest(payload.request))
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
