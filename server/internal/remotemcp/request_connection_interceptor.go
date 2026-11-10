package remotemcp

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/mcp/mcpversions"
	"github.com/speakeasy-api/gram/server/internal/mcp/tunnelrouting"
	"github.com/speakeasy-api/gram/server/internal/mcpaccess"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/shutdowndrain"
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

	// Request is the caller's request, read only if a row is written.
	Request *http.Request
}

// requestConnections records refused and failed tool calls, and rejected credentials on other requests; nil adds no interceptor.
type requestConnections struct {
	ConnectionAttribution

	recorder *mcpconnectionattempts.Recorder

	// tunneled marks an upstream reached through a tunnel.
	tunneled bool

	// upstreamAuth reports that Speakeasy attached a credential.
	upstreamAuth bool

	// credentialHeaders names the admin-set credential headers.
	credentialHeaders []string

	// provider is the upstream URL, empty for a tunnel's internal gateway address.
	provider string
}

// newRequestConnections is nil when the proxy has no attribution.
func newRequestConnections(attribution *ConnectionAttribution, recorder *mcpconnectionattempts.Recorder, identity proxy.ServerIdentity, upstreamURL, upstreamAuth string, headers []proxy.ConfiguredHeader) *requestConnections {
	if attribution == nil {
		return nil
	}
	connections := &requestConnections{ConnectionAttribution: *attribution, recorder: recorder, tunneled: identity.TunneledMCPServerID != "", upstreamAuth: upstreamAuth != "", credentialHeaders: configuredCredentialNames(headers), provider: ""}
	if !connections.tunneled {
		connections.provider = upstreamURL
	}
	return connections
}

func (c *requestConnections) record(ctx context.Context, code mcpconnectionattempts.Code, extra ...mcpconnectionattempts.Option) {
	// A caller that went away says nothing about the server.
	if errors.Is(ctx.Err(), context.Canceled) {
		return
	}
	opts := append([]mcpconnectionattempts.Option{mcpconnectionattempts.Provider(c.provider), mcpconnectionattempts.FromRequest(c.Request)}, c.Details...)
	opts = append(opts, extra...)
	c.recorder.Once(ctx, c.Scope, mcpconnectionattempts.StepMCPRequest, code, opts...)
}

func (c *requestConnections) refusals() []proxy.ToolsCallRefusalInterceptor {
	if c == nil {
		return nil
	}
	return []proxy.ToolsCallRefusalInterceptor{c}
}

func (c *requestConnections) withFailures(interceptors []proxy.ToolsCallFailureInterceptor) []proxy.ToolsCallFailureInterceptor {
	if c == nil || !c.UpstreamFailures {
		return interceptors
	}
	return append(interceptors, c)
}

// requestFailures records credential rejections on requests other than tools/call.
func (c *requestConnections) requestFailures() []proxy.RequestFailureInterceptor {
	if c == nil || !c.UpstreamFailures {
		return nil
	}
	return []proxy.RequestFailureInterceptor{c}
}

// Name implements the interceptor interfaces.
func (c *requestConnections) Name() string {
	return "request-connection-attempts"
}

// InterceptToolsCallRefusal implements [proxy.ToolsCallRefusalInterceptor].
func (c *requestConnections) InterceptToolsCallRefusal(ctx context.Context, err error) {
	if code, ok := refusalHappened(err); ok {
		c.record(ctx, code)
	}
}

// InterceptToolsCallFailure implements [proxy.ToolsCallFailureInterceptor].
func (c *requestConnections) InterceptToolsCallFailure(ctx context.Context, failure *proxy.ToolsCallFailure) {
	if errors.Is(failure.Err, shutdowndrain.ErrInterrupted) {
		return
	}
	if code, ok := c.upstreamFailureHappened(failure.StatusCode, failure.SentHeader, failure.Header, failure.Err); ok {
		c.record(ctx, code)
	}
}

// InterceptRequestFailure implements [proxy.RequestFailureInterceptor]. Only a
// 401 or 403 is recorded: a rejected credential, or through a tunnel, the
// tunnel gateway's own refusal. Other statuses on these requests say less about
// the connection than a failed tool call does. The row names the method.
func (c *requestConnections) InterceptRequestFailure(ctx context.Context, failure *proxy.RequestFailure) {
	if failure.StatusCode != http.StatusUnauthorized && failure.StatusCode != http.StatusForbidden {
		return
	}
	if code, ok := c.upstreamFailureHappened(failure.StatusCode, failure.SentHeader, failure.Header, failure.Err); ok {
		c.record(ctx, code, mcpconnectionattempts.RequestMethod(requestMethodDetail(failure.Method)))
	}
}

// upstreamFailureHappened classifies by what the final request carried: a redirect off
// the server's origin or a suppressed header sends no credential to reject. A
// credential renewal that failed for an outage is that outage, not a rejection.
func (c *requestConnections) upstreamFailureHappened(status int, sent, response http.Header, cause error) (mcpconnectionattempts.Code, bool) {
	if errors.Is(cause, remotesessions.ErrRemoteSessionUnavailable) {
		code := remotesessions.ConnectionOutageHappened(cause)
		return code, code != (mcpconnectionattempts.Code{})
	}
	credential := sentCredential(sent, c.upstreamAuth, c.SelfCredential, c.credentialHeaders)
	return UpstreamFailureHappened(status, CredentialRejected(status, response.Values("WWW-Authenticate")), credential, c.tunneled, response.Get(tunnelrouting.ErrorHeader), cause)
}

// SentCredential is what a request that reached the server still carried: the bearer Speakeasy
// attached, which self marks as the server's own, and any admin-set credential in headers.
func SentCredential(sent http.Header, bearerAttached, self bool, headers []proxy.ConfiguredHeader) UpstreamCredentialSent {
	return sentCredential(sent, bearerAttached, self, configuredCredentialNames(headers))
}

func sentCredential(sent http.Header, bearerAttached, self bool, credentialHeaders []string) UpstreamCredentialSent {
	attached := bearerAttached && sent.Get("Authorization") != ""
	return UpstreamCredentialSent{
		Attached:   attached,
		Self:       attached && self,
		Configured: slices.ContainsFunc(credentialHeaders, func(name string) bool { return sent.Get(name) != "" }),
	}
}

// CredentialRejected reports whether a provider's answer rejected the credential: a 401, or a 403
// whose Bearer or DPoP challenge says error="invalid_token" (RFC 6750 §3.1, RFC 9449 §7.1). A 403
// for insufficient_scope, or with no such challenge, is a permission answer from a working credential.
func CredentialRejected(status int, wwwAuthenticate []string) bool {
	switch status {
	case http.StatusUnauthorized:
		return true
	case http.StatusForbidden:
		return slices.ContainsFunc(authParams(wwwAuthenticate), func(p authParam) bool {
			return (strings.EqualFold(p.scheme, "Bearer") || strings.EqualFold(p.scheme, "DPoP")) &&
				strings.EqualFold(p.name, "error") && strings.EqualFold(p.value, "invalid_token")
		})
	}
	return false
}

// requestMethodDetail bounds a client-supplied method to a known label.
func requestMethodDetail(method string) string {
	if strings.HasPrefix(method, "notifications/") {
		return "notification"
	}
	if mcpversions.KnownMethod(method) {
		return strings.ReplaceAll(method, "/", "_")
	}
	return "other"
}

// UpstreamCredentialSent is what the failed call sent upstream.
type UpstreamCredentialSent struct {
	// Attached reports that Speakeasy sent a credential.
	Attached bool

	// Self marks a credential the server holds for itself.
	Self bool

	// Configured reports that an admin-set header was sent.
	Configured bool
}

// HasConfiguredCredential reports an admin-set static credential. A header forwarded from the caller is the caller's.
func HasConfiguredCredential(headers []proxy.ConfiguredHeader) bool {
	return slices.ContainsFunc(headers, isConfiguredCredential)
}

func isConfiguredCredential(header proxy.ConfiguredHeader) bool {
	return header.StaticValue != "" && header.ValueFromRequestHeader == "" &&
		proxy.IsCredentialHeader(header.Name, header.IsSecret, false)
}

// configuredCredentialNames lists the admin-set credential headers.
func configuredCredentialNames(headers []proxy.ConfiguredHeader) []string {
	var names []string
	for _, header := range headers {
		if isConfiguredCredential(header) {
			names = append(names, header.Name)
		}
	}
	return names
}

// UpstreamFailureHappened classifies a failed upstream call for direct servers and gateway members.
// credentialRejected says a 401 or 403 rejected the credential sent, as [CredentialRejected] decides.
// tunnelError is the tunnel gateway's own error status, when it answered instead of the server.
func UpstreamFailureHappened(status int, credentialRejected bool, credential UpstreamCredentialSent, tunneled bool, tunnelError string, causes ...error) (mcpconnectionattempts.Code, bool) {
	var none mcpconnectionattempts.Code
	if status == 0 {
		for _, cause := range causes {
			if setup, ok := errors.AsType[*proxy.ForwardSetupError](cause); ok {
				switch setup.Kind {
				case proxy.ForwardSetupCaller:
					return mcpconnectionattempts.CodeInvalidRequest, true
				case proxy.ForwardSetupConfiguration:
					return mcpconnectionattempts.CodeCredentialMisconfigured, true
				default:
					return mcpconnectionattempts.CodeInternalError, true
				}
			}
		}
	}
	// The gateway strips this header from what the server sends, so it is the gateway's own.
	if tunneled && (status == http.StatusBadGateway || status == 0 || tunnelError != "") {
		if code, ok := tunnelrouting.FailureHappened(tunnelError, causes...); ok {
			return code, true
		}
	}
	switch {
	case status == 0 && slices.ContainsFunc(causes, func(cause error) bool { return errors.Is(cause, proxy.ErrUpstreamTimedOut) }):
		// A slow server looks the same through a tunnel as it does directly.
		return mcpconnectionattempts.CodeProviderUnreachable, true
	case tunneled && status == 0:
		// Our own tunnel gateway did not answer, so the tunnel's owner can't fix it.
		return mcpconnectionattempts.CodeInternalError, true
	case status == 0:
		return mcpconnectionattempts.CodeProviderUnreachable, true
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		if !credentialRejected {
			return none, false
		}
		switch {
		case credential.Self:
			return mcpconnectionattempts.CodeSharedCredentialRejected, true
		case credential.Attached:
			return mcpconnectionattempts.CodeProviderRejected, true
		case credential.Configured:
			return mcpconnectionattempts.CodeStaticKeyRejected, true
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
	// A tool the catalog doesn't have is not a connection outcome.
	if _, held := errors.AsType[*proxy.ToolResultError](err); held || errors.Is(err, errToolNotOnUpstream) {
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
