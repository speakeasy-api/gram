package remotemcp

import (
	"context"
	"errors"
	"net/http"

	"github.com/speakeasy-api/gram/server/internal/mcpaccess"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
)

// toolsCallConnections records tools/call requests that were refused or failed.
// A call that succeeds touches none of this beyond a nil-error check. Nil
// records nothing and adds no interceptor.
type toolsCallConnections struct {
	recorder *mcpconnectionattempts.Recorder
	scope    mcpconnectionattempts.Scope

	// details are added to every row.
	details []mcpconnectionattempts.Option

	// recordFailures adds the upstream failure interceptor.
	recordFailures bool

	// selfCredential marks an upstream credential the server holds for itself,
	// which the person connected did not supply and cannot replace.
	selfCredential bool

	// tunneled marks an upstream reached through a tunnel, so an unanswered call means the tunnel.
	tunneled bool

	// upstreamAuth reports that Speakeasy attached a credential. A 401 without one
	// is the upstream's ordinary sign-in challenge, not a rejection.
	upstreamAuth bool

	// provider is the upstream URL, left empty for a tunnel's internal gateway address.
	provider string
}

func (c *toolsCallConnections) record(ctx context.Context, code mcpconnectionattempts.Code) {
	opts := append([]mcpconnectionattempts.Option{mcpconnectionattempts.Provider(c.provider)}, c.details...)
	c.recorder.Once(ctx, c.scope, mcpconnectionattempts.StepMCPRequest, code, opts...)
}

// watch returns interceptor unchanged when nothing is recorded.
func (c *toolsCallConnections) watch(interceptor proxy.ToolsCallRequestInterceptor, refusal func(error) (mcpconnectionattempts.Code, bool)) proxy.ToolsCallRequestInterceptor {
	if c == nil {
		return interceptor
	}
	return &watchedToolsCallRequest{ToolsCallRequestInterceptor: interceptor, connections: c, refusal: refusal}
}

func (c *toolsCallConnections) watchResponse(interceptor proxy.ToolsCallResponseInterceptor) proxy.ToolsCallResponseInterceptor {
	if c == nil {
		return interceptor
	}
	return &watchedToolsCallResponse{ToolsCallResponseInterceptor: interceptor, connections: c}
}

func (c *toolsCallConnections) withFailures(interceptors []proxy.ToolsCallFailureInterceptor) []proxy.ToolsCallFailureInterceptor {
	if c == nil || !c.recordFailures {
		return interceptors
	}
	return append(interceptors, c)
}

// Name implements [proxy.ToolsCallFailureInterceptor].
func (c *toolsCallConnections) Name() string {
	return "tools-call-connection-attempts"
}

// InterceptToolsCallFailure implements [proxy.ToolsCallFailureInterceptor].
func (c *toolsCallConnections) InterceptToolsCallFailure(ctx context.Context, failure *proxy.ToolsCallFailure) {
	if code, ok := c.failureHappened(failure); ok {
		c.record(ctx, code)
	}
}

func (c *toolsCallConnections) failureHappened(failure *proxy.ToolsCallFailure) (mcpconnectionattempts.Code, bool) {
	var none mcpconnectionattempts.Code
	status := failure.StatusCode
	switch {
	case proxy.IsShutdownInterrupt(failure.Err):
		return none, false
	case c.tunneled && status == http.StatusBadGateway:
		return mcpconnectionattempts.CodeTunnelUnavailable, true
	case c.tunneled && status == 0:
		// Our own tunnel gateway did not answer, so this is not the tunnel owner's to fix.
		return mcpconnectionattempts.CodeInternalError, true
	case status == 0:
		return mcpconnectionattempts.CodeProviderUnreachable, true
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		switch {
		case c.selfCredential:
			return mcpconnectionattempts.CodeCredentialMisconfigured, true
		case c.upstreamAuth:
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

type watchedToolsCallRequest struct {
	proxy.ToolsCallRequestInterceptor

	connections *toolsCallConnections
	refusal     func(error) (mcpconnectionattempts.Code, bool)
}

func (w *watchedToolsCallRequest) InterceptToolsCallRequest(ctx context.Context, call *proxy.ToolsCallRequest) error {
	err := w.ToolsCallRequestInterceptor.InterceptToolsCallRequest(ctx, call)
	if err != nil {
		if code, ok := w.refusal(err); ok {
			w.connections.record(ctx, code)
		}
	}
	return err //nolint:wrapcheck // The chain's error contract passes through unchanged.
}

type watchedToolsCallResponse struct {
	proxy.ToolsCallResponseInterceptor

	connections *toolsCallConnections
}

func (w *watchedToolsCallResponse) InterceptToolsCallResponse(ctx context.Context, call *proxy.ToolsCallResponse) error {
	err := w.ToolsCallResponseInterceptor.InterceptToolsCallResponse(ctx, call)
	if err != nil {
		if code, ok := riskPolicyRefusal(err); ok {
			w.connections.record(ctx, code)
		}
	}
	return err //nolint:wrapcheck // The chain's error contract passes through unchanged.
}

func killswitchRefusal(err error) (mcpconnectionattempts.Code, bool) {
	if reject, ok := errors.AsType[*proxy.RejectError](err); ok {
		if _, paused := reject.Data.(proxy.KillswitchRejectionData); paused {
			return mcpconnectionattempts.CodeMCPToolCallsPaused, true
		}
	}
	return mcpconnectionattempts.CodeKillswitchCheckUnavailable, true
}

func usageLimitRefusal(error) (mcpconnectionattempts.Code, bool) {
	return mcpconnectionattempts.CodeUsageLimitReached, true
}

// toolPermissionRefusal leaves out a failed lookup, which is not a denial.
func toolPermissionRefusal(err error) (mcpconnectionattempts.Code, bool) {
	if shareable, ok := errors.AsType[*oops.ShareableError](err); ok && shareable.Error() == mcpaccess.ToolPermissionDeniedMessage {
		return mcpconnectionattempts.CodeToolPermissionDenied, true
	}
	var none mcpconnectionattempts.Code
	return none, false
}

func toolNotApprovedRefusal(err error) (mcpconnectionattempts.Code, bool) {
	if reject, ok := errors.AsType[*proxy.RejectError](err); ok && reject.Message == ToolNotApprovedMessage {
		return mcpconnectionattempts.CodeToolNotApproved, true
	}
	var none mcpconnectionattempts.Code
	return none, false
}

// riskPolicyRefusal leaves out a call held for approval: the agent reads it,
// asks for approval and retries, so it is not an end state.
func riskPolicyRefusal(err error) (mcpconnectionattempts.Code, bool) {
	if _, ok := errors.AsType[*proxy.RejectError](err); ok {
		return mcpconnectionattempts.CodePolicyBlocked, true
	}
	var none mcpconnectionattempts.Code
	return none, false
}
