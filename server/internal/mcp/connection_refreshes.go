package mcp

import (
	"context"
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/speakeasy-api/gram/server/internal/agents/runtimepolicy"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/identitychaining"
	"github.com/speakeasy-api/gram/server/internal/signins"
)

// agentAdmissionRejection tells a refused agent from an admission check that could not run.
func agentAdmissionRejection(err error) string {
	if errors.Is(err, runtimepolicy.ErrPrincipalNotAdmitted) {
		return "agent_not_admitted"
	}
	if errors.Is(err, errCredentialRejected) {
		return "agent_credential_invalid"
	}
	if shareable, ok := errors.AsType[*oops.ShareableError](err); ok {
		if shareable.Code == oops.CodeUnauthorized {
			return "agent_admission_misconfigured"
		}
		if shareable.Code == oops.CodeForbidden {
			return "agent_admission_denied"
		}
	}
	return "agent_admission_unavailable"
}

// signInRevokedByAdmin: an admin or the directory revoked the sign-in behind a refresh.
const signInRevokedByAdmin = "sign_in_revoked_by_admin"

// signInRevokedReason names a refused sign-in by who revoked it; a bare fence does not say who.
func signInRevokedReason(revokeReason pgtype.Text) string {
	switch signins.RevokeReason(revokeReason.String) {
	case signins.RevokeReasonAdmin, signins.RevokeReasonDeprovisioned:
		return signInRevokedByAdmin
	case signins.RevokeReasonSignOut, signins.RevokeReasonSignOutEverywhere:
		return "sign_in_revoked"
	default:
		return "sign_in_revoked"
	}
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

	// upstreamRefreshEMARefused: written by older servers for any refusal, so it names no party.
	upstreamRefreshEMARefused upstreamRefreshCheck = "upstream_ema_refused"

	// upstreamRefreshEMAServiceRefused: the service refused managed sign-in for good.
	upstreamRefreshEMAServiceRefused upstreamRefreshCheck = "upstream_ema_service_refused"

	// upstreamRefreshEMAIdentityProviderRefused: the identity provider refused managed sign-in for good.
	upstreamRefreshEMAIdentityProviderRefused upstreamRefreshCheck = "upstream_ema_idp_refused"

	// upstreamRefreshEMANotRenewed: managed sign-in failed for good with no one refusing, such as a setup problem.
	upstreamRefreshEMANotRenewed upstreamRefreshCheck = "upstream_ema_not_renewed"
)

// emaRefusedRefreshCheck names who refused a cached managed sign-in refusal, from chainingVerdicts, as the request before it did.
func emaRefusedRefreshCheck(verdict identitychaining.RefreshVerdict) upstreamRefreshCheck {
	refusal := identitychaining.Outcome{Stage: verdict.Stage, Reason: verdict.Reason, Confidence: "", Retryable: false, Cached: true, Provider: verdict.Provider, Unavailable: "", Submitted: false, Retained: false, Failures: nil}
	recorded, _ := chainingVerdictOf(refusal)
	switch recorded.code.RefusedBy() {
	case mcpconnectionattempts.RefusedByIdentityProvider:
		return upstreamRefreshEMAIdentityProviderRefused
	case mcpconnectionattempts.RefusedByService:
		return upstreamRefreshEMAServiceRefused
	}
	return upstreamRefreshEMANotRenewed
}

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
	return c == upstreamRefreshReconnect || c == upstreamRefreshEMARefused || c == upstreamRefreshEMAIdentityProviderRefused || c == upstreamRefreshEMAServiceRefused || c == upstreamRefreshEMANotRenewed
}

// refuseInboundRefresh records a refusal only after its response was delivered.
func refuseInboundRefresh(ctx context.Context, tracker *mcpconnectionattempts.Tracker, r *http.Request, writeErr error, reason string, opts ...mcpconnectionattempts.Option) error {
	if writeErr != nil {
		tracker.Continue()
		return writeErr
	}
	code, ok := inboundRefreshHappened(reason)
	if !ok {
		tracker.Continue()
		return nil
	}
	tracker.Refuse(ctx, code, append(opts, mcpconnectionattempts.Detail(reason), mcpconnectionattempts.FromRequest(r))...)
	return nil
}

// inboundRefreshHappened leaves out a request with no token to judge; an unknown reason is ours.
func inboundRefreshHappened(reason string) (mcpconnectionattempts.Code, bool) {
	switch reason {
	case "refresh_token_resource_mismatch", "resource_mismatch":
		return mcpconnectionattempts.CodeResourceMismatch, true
	case "refresh_token_unknown_or_already_used":
		return mcpconnectionattempts.CodeRefreshTokenUnknownOrAlreadyUsed, true
	case "refresh_token_client_mismatch":
		return mcpconnectionattempts.CodeRefreshTokenUsedByOtherApp, true
	case "refresh_token_expired":
		return mcpconnectionattempts.CodeRefreshTokenExpired, true
	case "sign_in_revoked":
		return mcpconnectionattempts.CodeSignInRevoked, true
	case signInRevokedByAdmin:
		return mcpconnectionattempts.CodeSignInRevokedByAdmin, true
	case "refresh_token_revoked", "refresh_token_replay_rejected", "tool_selection_resource_mismatch":
		return mcpconnectionattempts.CodeRefreshTokenRevoked, true
	case "agent_credential_invalid":
		return mcpconnectionattempts.CodeConnectionChanged, true
	case "tool_selection_malformed", "refresh_token_replay_session_mismatch":
		return mcpconnectionattempts.CodeStoredSessionInvalid, true
	case "agent_not_admitted":
		return mcpconnectionattempts.CodeSubjectNotAdmitted, true
	case "agent_admission_misconfigured":
		return mcpconnectionattempts.CodeCredentialMisconfigured, true
	case "agent_admission_denied":
		return mcpconnectionattempts.CodeNoServerAccess, true
	case string(upstreamRefreshReconnect):
		return mcpconnectionattempts.CodeUpstreamReconnectRequired, true
	case string(upstreamRefreshEMARefused):
		return mcpconnectionattempts.CodeUpstreamEMARefused, true
	case string(upstreamRefreshEMAIdentityProviderRefused):
		return mcpconnectionattempts.CodeUpstreamEMAIdentityProviderRefused, true
	case string(upstreamRefreshEMAServiceRefused):
		return mcpconnectionattempts.CodeUpstreamEMAServiceRefused, true
	case string(upstreamRefreshEMANotRenewed):
		return mcpconnectionattempts.CodeUpstreamEMANotRenewed, true
	case "invalid_request":
		var none mcpconnectionattempts.Code
		return none, false
	default:
		return mcpconnectionattempts.CodeInternalError, true
	}
}
