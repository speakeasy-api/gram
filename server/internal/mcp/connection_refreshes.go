package mcp

import (
	"context"
	"errors"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/identitychaining"
	"net/http"
)

// agentAdmissionRejection tells a refused agent from an admission check that could not run.
func agentAdmissionRejection(err error) string {
	if shareable, ok := errors.AsType[*oops.ShareableError](err); ok && (shareable.Code == oops.CodeUnauthorized || shareable.Code == oops.CodeForbidden) {
		return "agent_admission_denied"
	}
	return "agent_admission_unavailable"
}

// upstreamRefreshCheck is what refresh learned about a session's upstream; values name the row's reason.
type upstreamRefreshCheck string

const (
	// upstreamRefreshServed: the upstream is usable, or the session needs none.
	upstreamRefreshServed upstreamRefreshCheck = ""

	// upstreamRefreshUnverified: identity chaining's setup kept it from confirming the upstream.
	upstreamRefreshUnverified upstreamRefreshCheck = "refreshed_unverified"

	// upstreamRefreshCheckFailed: our own read failed, so the refresh proceeds unjudged.
	upstreamRefreshCheckFailed upstreamRefreshCheck = "upstream_check_failed"

	// upstreamRefreshReconnect: the person has to reconnect the upstream.
	upstreamRefreshReconnect upstreamRefreshCheck = "upstream_reconnect_required"

	// upstreamRefreshEMARefused: the provider refused managed sign-in for good.
	upstreamRefreshEMARefused upstreamRefreshCheck = "upstream_ema_refused"
)

// unconfirmedRefreshCheck blames the setup only for a reason the setup causes; an outage is no one's to fix.
func unconfirmedRefreshCheck(reason identitychaining.Reason) upstreamRefreshCheck {
	switch reason {
	case identitychaining.ReasonConfigurationRequired, identitychaining.ReasonStaleConfiguration, identitychaining.ReasonBindingNotReady,
		identitychaining.ReasonInvalidTarget, identitychaining.ReasonScopePolicyDenied, identitychaining.ReasonAccessDenied, identitychaining.ReasonInvalidClient,
		identitychaining.ReasonMalformedAssertion, identitychaining.ReasonInsufficientScope, identitychaining.ReasonIneligibleIdentity:
		return upstreamRefreshUnverified
	default:
		return upstreamRefreshCheckFailed
	}
}

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
func refuseInboundRefresh(ctx context.Context, tracker *mcpconnectionattempts.Tracker, r *http.Request, reason string, opts ...mcpconnectionattempts.Option) {
	code, ok := inboundRefreshHappened(reason)
	if !ok {
		tracker.Continue()
		return
	}
	tracker.Refuse(ctx, code, append(opts, mcpconnectionattempts.Detail(reason), mcpconnectionattempts.FromRequest(r))...)
}

// inboundRefreshHappened leaves out a request with no token to judge; an unknown reason is ours.
func inboundRefreshHappened(reason string) (mcpconnectionattempts.Code, bool) {
	switch reason {
	case "refresh_token_resource_mismatch":
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
	case "agent_admission_unavailable":
		return mcpconnectionattempts.CodeInternalError, true
	case string(upstreamRefreshReconnect):
		return mcpconnectionattempts.CodeUpstreamReconnectRequired, true
	case string(upstreamRefreshEMARefused):
		return mcpconnectionattempts.CodeUpstreamEMARefused, true
	case "invalid_request", "resource_mismatch":
		var none mcpconnectionattempts.Code
		return none, false
	default:
		return mcpconnectionattempts.CodeInternalError, true
	}
}
