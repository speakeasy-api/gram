package remotemcp

import (
	"context"
	"errors"
	"net/http"

	"github.com/speakeasy-api/gram/server/internal/mcpaccess"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
)

// ConnectionAttribution files a proxy's refused and failed tool calls.
type ConnectionAttribution struct {
	// Scope is what the rows are filed under.
	Scope mcpconnectionattempts.Scope

	// Details are added to every row.
	Details []mcpconnectionattempts.Option

	// UpstreamFailures also records calls the upstream failed. A gateway leaves
	// this off and records from its own classification, which covers the handshake.
	UpstreamFailures bool

	// SelfCredential marks a credential the server holds for itself, which the
	// person connected did not supply and cannot replace.
	SelfCredential bool
}

// toolsCallConnections records refused and failed tool calls; nil adds no interceptor.
type toolsCallConnections struct {
	ConnectionAttribution

	recorder *mcpconnectionattempts.Recorder

	// tunneled marks an upstream reached through a tunnel.
	tunneled bool

	// upstreamAuth reports that Speakeasy attached a credential.
	upstreamAuth bool

	// provider is the upstream URL, empty for a tunnel's internal gateway address.
	provider string
}

// newToolsCallConnections is nil when the proxy has no attribution.
func newToolsCallConnections(attribution *ConnectionAttribution, recorder *mcpconnectionattempts.Recorder, identity proxy.ServerIdentity, upstreamURL, upstreamAuth string) *toolsCallConnections {
	if attribution == nil {
		return nil
	}
	connections := &toolsCallConnections{ConnectionAttribution: *attribution, recorder: recorder, tunneled: identity.TunneledMCPServerID != "", upstreamAuth: upstreamAuth != "", provider: ""}
	if !connections.tunneled {
		connections.provider = upstreamURL
	}
	return connections
}

func (c *toolsCallConnections) record(ctx context.Context, code mcpconnectionattempts.Code) {
	// A caller that went away says nothing about the server.
	if errors.Is(ctx.Err(), context.Canceled) {
		return
	}
	opts := append([]mcpconnectionattempts.Option{mcpconnectionattempts.Provider(c.provider)}, c.Details...)
	c.recorder.Once(ctx, c.Scope, mcpconnectionattempts.StepMCPRequest, code, opts...)
}

func (c *toolsCallConnections) refusals() []proxy.ToolsCallRefusalInterceptor {
	if c == nil {
		return nil
	}
	return []proxy.ToolsCallRefusalInterceptor{c}
}

func (c *toolsCallConnections) withFailures(interceptors []proxy.ToolsCallFailureInterceptor) []proxy.ToolsCallFailureInterceptor {
	if c == nil || !c.UpstreamFailures {
		return interceptors
	}
	return append(interceptors, c)
}

// Name implements both interceptor interfaces.
func (c *toolsCallConnections) Name() string {
	return "tools-call-connection-attempts"
}

// InterceptToolsCallRefusal implements [proxy.ToolsCallRefusalInterceptor].
func (c *toolsCallConnections) InterceptToolsCallRefusal(ctx context.Context, err error) {
	if code, ok := refusalHappened(err); ok {
		c.record(ctx, code)
	}
}

// InterceptToolsCallFailure implements [proxy.ToolsCallFailureInterceptor].
func (c *toolsCallConnections) InterceptToolsCallFailure(ctx context.Context, failure *proxy.ToolsCallFailure) {
	if proxy.IsShutdownInterrupt(failure.Err) {
		return
	}
	if code, ok := UpstreamFailureHappened(failure.StatusCode, UpstreamCredential{Attached: c.upstreamAuth, Self: c.SelfCredential}, c.tunneled); ok {
		c.record(ctx, code)
	}
}

// UpstreamCredential is what the failed call sent upstream.
type UpstreamCredential struct {
	// Attached reports that Speakeasy sent a credential.
	Attached bool

	// Self marks a credential the server holds for itself.
	Self bool
}

// UpstreamFailureHappened classifies a failed upstream call for direct servers and gateway members.
func UpstreamFailureHappened(status int, credential UpstreamCredential, tunneled bool) (mcpconnectionattempts.Code, bool) {
	var none mcpconnectionattempts.Code
	switch {
	case tunneled && status == http.StatusBadGateway:
		return mcpconnectionattempts.CodeTunnelUnavailable, true
	case tunneled && status == 0:
		// Our own tunnel gateway did not answer, so the tunnel's owner can't fix it.
		return mcpconnectionattempts.CodeInternalError, true
	case status == 0:
		return mcpconnectionattempts.CodeProviderUnreachable, true
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		switch {
		case credential.Self:
			return mcpconnectionattempts.CodeCredentialMisconfigured, true
		case credential.Attached:
			return mcpconnectionattempts.CodeProviderRejected, true
		}
		return none, false
	case status == http.StatusTooManyRequests:
		return mcpconnectionattempts.CodeProviderRateLimited, true
	case status >= http.StatusInternalServerError:
		return mcpconnectionattempts.CodeProviderServerError, true
	}
	return none, false
}

// refusalHappened classifies a refusal; an unknown error is ours and a held call is not an end state.
func refusalHappened(err error) (mcpconnectionattempts.Code, bool) {
	if _, held := errors.AsType[*proxy.ToolResultError](err); held {
		var none mcpconnectionattempts.Code
		return none, false
	}
	if _, unavailable := errors.AsType[killswitchCheckError](err); unavailable {
		return mcpconnectionattempts.CodeKillswitchCheckUnavailable, true
	}
	if errors.Is(err, ErrToolUsageLimitReached) {
		return mcpconnectionattempts.CodeUsageLimitReached, true
	}
	if mcpaccess.IsToolPermissionDenied(err) {
		return mcpconnectionattempts.CodeToolPermissionDenied, true
	}
	if reject, ok := errors.AsType[*proxy.RejectError](err); ok {
		if _, paused := reject.Data.(proxy.KillswitchRejectionData); paused {
			return mcpconnectionattempts.CodeMCPToolCallsPaused, true
		}
		if reject.Message == ToolNotApprovedMessage {
			return mcpconnectionattempts.CodeToolNotApproved, true
		}
		if reject.Code == proxy.RejectCodeForbidden {
			return mcpconnectionattempts.CodePolicyBlocked, true
		}
	}
	return mcpconnectionattempts.CodeInternalError, true
}
