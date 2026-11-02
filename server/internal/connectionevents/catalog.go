package connectionevents

// Outcome is the overall result of an attempt, stored on its final step only.
type Outcome string

const (
	OutcomeConnected Outcome = "connected"
	OutcomeFailed    Outcome = "failed"
	OutcomeBlocked   Outcome = "blocked"
	OutcomeDeclined  Outcome = "declined"

	// OutcomeAbandoned is an attempt with no final row. Readers derive it, so
	// the recorder has no way to write it.
	OutcomeAbandoned Outcome = "abandoned"
)

// WhoFixes is the party that has to act, not necessarily the one that caused it.
type WhoFixes string

const (
	WhoNobody    WhoFixes = ""
	WhoPerson    WhoFixes = "person"
	WhoAdmin     WhoFixes = "admin"
	WhoProvider  WhoFixes = "provider"
	WhoApp       WhoFixes = "app"
	WhoSpeakeasy WhoFixes = "speakeasy"
)

// WhoToAsk is the plain-language pointer shown for each party.
func (w WhoFixes) WhoToAsk() string {
	switch w {
	case WhoPerson:
		return "You can do this yourself."
	case WhoAdmin:
		return "Ask your Speakeasy admin."
	case WhoProvider:
		return "Ask your admin to check with the service provider."
	case WhoApp:
		return "Ask the team that makes your AI app."
	case WhoSpeakeasy:
		return "Contact Speakeasy support."
	case WhoNobody:
		return ""
	default:
		return ""
	}
}

// Step names a stage of an attempt. Counts are comparable only within a step.
type Step struct{ name string }

// String is the stored value.
func (s Step) String() string { return s.name }

var (
	StepInboundAuthorize = Step{"inbound_authorize"}
	StepInboundSignIn    = Step{"inbound_sign_in"}
	StepInboundConsent   = Step{"inbound_consent"}
	StepInboundToken     = Step{"inbound_token"}
	StepProviderSignIn   = Step{"provider_sign_in"}
	StepProviderToken    = Step{"provider_token"}
	StepProviderRefresh  = Step{"provider_refresh"}
	StepSignInRenewal    = Step{"sign_in_renewal"}
	StepUpstreamEMA      = Step{"upstream_ema"}
	StepMCPRequest       = Step{"mcp_request"}
	StepDiscoveryListing = Step{"discovery_listing"}
	StepDiscoveryMember  = Step{"discovery_member"}
)

// Happened is what a call site says about a step: a success Result, or a Code
// for anything else. Only this package implements it.
type Happened interface {
	happened()
}

// Result is how a step succeeded. Failure results are not exported: the catalog
// sets them from the code, so a failed step cannot be recorded as a success.
type Result struct{ name string }

func (Result) happened() {}

// String is the stored value.
func (r Result) String() string { return r.name }

var (
	ResultAccepted                = Result{"accepted"}
	ResultSignedIn                = Result{"signed_in"}
	ResultCodeIssued              = Result{"code_issued"}
	ResultRefreshed               = Result{"refreshed"}
	ResultRefreshReplayed         = Result{"refresh_replayed"}
	ResultAdoptedConcurrentWinner = Result{"adopted_concurrent_winner"}
	ResultRenewed                 = Result{"renewed"}
	ResultAssertionIssued         = Result{"assertion_issued"}
	ResultAllowed                 = Result{"allowed"}
	ResultListed                  = Result{"listed"}
	ResultComplete                = Result{"complete"}

	resultPartial       = Result{"partial"}
	resultFailed        = Result{"failed"}
	resultPolicyBlocked = Result{"policy_blocked"}
	resultDeclined      = Result{"declined"}
	resultNeedsSignIn   = Result{"needs_sign_in"}
	resultNeedsConsent  = Result{"needs_consent"}
)

// Code is a stable reason code. Codes are append-only: rows and dashboards
// outlive a release, so never rename or remove one. Declare a new one below with
// define and add it to the end of frozenCodes in the test.
type Code struct{ name string }

func (Code) happened() {}

// String is the stored value.
func (c Code) String() string { return c.name }

// Meaning is what a code stands for and what a person reads about it. It is
// the only source of who fixes it and of the message.
type Meaning struct {
	// Outcome is the attempt's result when this code ends it.
	Outcome Outcome

	// WhoFixes is the party to act.
	WhoFixes WhoFixes

	// WhatHappened says what went wrong in one short sentence.
	WhatHappened string

	// WhatToDo says what to do next in one short sentence.
	WhatToDo string

	result Result
}

var catalog = map[string]Meaning{}

// MeaningOf returns the meaning of a stored reason.
func MeaningOf(reason string) (Meaning, bool) {
	meaning, ok := catalog[reason]
	return meaning, ok
}

func define(name string, outcome Outcome, who WhoFixes, what, todo string) Code {
	return defineResult(name, resultFor(outcome), outcome, who, what, todo)
}

func defineResult(name string, result Result, outcome Outcome, who WhoFixes, what, todo string) Code {
	catalog[name] = Meaning{Outcome: outcome, WhoFixes: who, WhatHappened: what, WhatToDo: todo, result: result}
	return Code{name}
}

func resultFor(outcome Outcome) Result {
	switch outcome {
	case OutcomeBlocked:
		return resultPolicyBlocked
	case OutcomeDeclined:
		return resultDeclined
	case OutcomeConnected:
		return ResultComplete
	case OutcomeFailed, OutcomeAbandoned:
		return resultFailed
	default:
		return resultFailed
	}
}

// Each line is the code, its outcome, who fixes it, what happened, what to do.
var (

	// App signs in to Speakeasy.
	CodeCIMDAdmissionDenied    = define("cimd_admission_denied", OutcomeBlocked, WhoAdmin, "Your organization does not allow this app.", "Ask for the app to be added to the allowed list.")
	CodeAIToolBlocked          = define("ai_tool_blocked", OutcomeBlocked, WhoAdmin, "Your organization has blocked this AI app.", "Use an approved app, or ask for this one to be approved.")
	CodeNotOrgMember           = define("not_org_member", OutcomeBlocked, WhoAdmin, "This account is not a member of the organization.", "Ask to be added to the organization.")
	CodeNoServerAccess         = define("no_server_access", OutcomeBlocked, WhoAdmin, "This account does not have access to this server.", "Ask for access to the server.")
	CodeNetworkDenied          = define("network_denied", OutcomeBlocked, WhoAdmin, "Your organization only allows this server from certain networks.", "Connect from an allowed network, or ask for yours to be added.")
	CodeCustomDomainRequired   = define("custom_domain_required", OutcomeBlocked, WhoPerson, "This server only works through its custom domain.", "Connect using the server's custom domain address.")
	CodeCustomDomainNotReady   = define("custom_domain_not_ready", OutcomeFailed, WhoAdmin, "This server's custom domain is not ready yet.", "Finish verifying the custom domain, then try again.")
	CodeIDPUnavailable         = define("idp_unavailable", OutcomeFailed, WhoSpeakeasy, "The sign-in service could not be reached.", "Try again in a few minutes.")
	CodeAIToolCheckUnavailable = define("ai_tool_check_unavailable", OutcomeFailed, WhoSpeakeasy, "We could not check whether this app is allowed.", "Try again in a few minutes.")
	CodeCIMDFetchFailed        = define("cimd_fetch_failed", OutcomeFailed, WhoApp, "We could not read this app's public details.", "Try again later. If it keeps failing, the app needs fixing.")
	CodeCIMDMetadataInvalid    = define("cimd_metadata_invalid", OutcomeFailed, WhoApp, "This app's public details are not valid.", "Update the app, or report the problem to its makers.")
	CodeRegistrationInvalid    = define("registration_invalid", OutcomeFailed, WhoApp, "This app could not register with the server.", "Update the app, or report the problem to its makers.")
	CodeUnknownClientID        = define("unknown_client_id", OutcomeFailed, WhoPerson, "The server does not recognize this app.", "Remove the server from your app and add it again.")
	CodeRedirectURIMismatch    = define("redirect_uri_mismatch", OutcomeFailed, WhoApp, "The app asked to return to an address it did not register.", "Update the app, or report the problem to its makers.")
	CodeResourceMismatch       = define("resource_mismatch", OutcomeFailed, WhoApp, "The app asked to connect to a different server than this one.", "Check the server address set in your app.")
	CodePKCEMismatch           = define("pkce_mismatch", OutcomeFailed, WhoApp, "The app could not complete a sign-in safety check.", "Try again. If it keeps failing, the app needs fixing.")
	CodeCodeNotFoundOrExpired  = define("code_not_found_or_expired", OutcomeFailed, WhoApp, "The app took too long to finish signing in, or tried twice.", "Try connecting again.")
	CodeServerNotFound         = define("server_not_found", OutcomeFailed, WhoPerson, "We could not find a server at this address.", "Check the address. If it looks right, ask your admin.")
	CodeExpired                = define("expired", OutcomeFailed, WhoPerson, "The sign-in took longer than 10 minutes and expired.", "Start connecting again.")
	CodeSessionChanged         = define("session_changed", OutcomeFailed, WhoPerson, "Sign-in finished in a different browser than it started in.", "Start again, and finish in the same browser.")
	CodeCanceled               = define("canceled", OutcomeDeclined, WhoPerson, "The sign-in was canceled.", "Connect again if you still want access.")

	// Enterprise-managed sign-in to Speakeasy.
	CodeEndpointNotAgentAddressable    = define("endpoint_not_agent_addressable", OutcomeBlocked, WhoAdmin, "This gateway doesn't accept this kind of connection.", "Ask for it to be allowed.")
	CodeSubjectNotAdmitted             = define("subject_not_admitted", OutcomeBlocked, WhoAdmin, "This identity is not allowed to connect here.", "Ask for this identity to be allowed.")
	CodeClientNotCIMD                  = define("client_not_cimd", OutcomeBlocked, WhoApp, "This app cannot use this kind of sign-in.", "Use an app that publishes its details at a public address.")
	CodeSubjectNotProvisioned          = define("subject_not_provisioned", OutcomeBlocked, WhoAdmin, "This account is not set up in the organization's directory.", "Ask to be added to the directory and the organization.")
	CodeInvalidRequest                 = define("invalid_request", OutcomeFailed, WhoApp, "The app sent a request we could not understand.", "Update the app, or report the problem to its makers.")
	CodeResourceNotAvailable           = define("resource_not_available", OutcomeFailed, WhoAdmin, "This account has no server it can use here.", "Ask for access to a server.")
	CodeAssertionInvalid               = define("assertion_invalid", OutcomeFailed, WhoApp, "The sign-in proof was not accepted.", "Try again. If it keeps failing, check the sign-in setup.")
	CodeAssertionOther                 = define("assertion_other", OutcomeFailed, WhoSpeakeasy, "The sign-in proof was refused for a reason we have not named yet.", "Report this so we can give it a clear reason.")
	CodeUpstreamCredentialChoice       = define("upstream_credential_choice", OutcomeFailed, WhoApp, "The app asked for a server sign-in this organization does not accept.", "Update the app. If that does not help, ask your admin.")
	CodeUpstreamSignInNotSetUp         = define("upstream_sign_in_not_set_up", OutcomeFailed, WhoAdmin, "This server's sign-in with the service is not set up.", "Set up the service's sign-in for this server.")
	CodeIssuerNotTrusted               = define("issuer_not_trusted", OutcomeFailed, WhoAdmin, "Speakeasy is not set up to trust this sign-in service.", "Add the trust link in the organization's settings.")
	CodeAssertionVerifierMisconfigured = define("assertion_verifier_misconfigured", OutcomeFailed, WhoSpeakeasy, "An internal sign-in check is not set up correctly.", "Report this problem.")
	CodeAssertionCheckUnavailable      = define("assertion_check_unavailable", OutcomeFailed, WhoSpeakeasy, "We could not check this sign-in right now.", "Try again in a few minutes.")

	// Provider sign-in.
	CodeProviderOAuthInvalidClient = define("provider_oauth_invalid_client", OutcomeFailed, WhoAdmin, "The service rejected Speakeasy's sign-in details for it.", "Check the client ID and secret set for this service.")
	CodeProviderOAuthInvalidScope  = define("provider_oauth_invalid_scope", OutcomeFailed, WhoAdmin, "The service rejected the permissions we asked for.", "Check the permissions set for this service.")
	CodeProviderOAuthInvalidGrant  = define("provider_oauth_invalid_grant", OutcomeFailed, WhoPerson, "The service did not accept this sign-in.", "Try signing in again.")
	CodeProviderDenied             = define("provider_denied", OutcomeDeclined, WhoPerson, "The service refused the sign-in, or you canceled it.", "Try again. If it keeps happening, ask your admin.")
	CodeIssuerMismatch             = define("issuer_mismatch", OutcomeFailed, WhoAdmin, "The sign-in answer came from a different service than expected.", "Check the sign-in address set for this service.")
	CodeConnectionChanged          = define("connection_changed", OutcomeFailed, WhoAdmin, "This service's connection was changed during sign-in.", "Check the connection still exists, then try again.")
	CodeUnrenewableToken           = define("unrenewable_token", OutcomeFailed, WhoAdmin, "The service gave a sign-in that expires and cannot be renewed.", "Set the service up to allow renewing sign-ins.")
	CodeRejectedByMember           = define("rejected_by_member", OutcomeFailed, WhoAdmin, "Sign-in worked, but the server rejected the new access.", "Check the server accepts sign-ins from this service.")
	CodeProviderRateLimited        = define("provider_rate_limited", OutcomeFailed, WhoProvider, "The service is limiting requests right now.", "Wait a few minutes and try again.")
	CodeProviderUnavailable        = define("provider_unavailable", OutcomeFailed, WhoProvider, "The service is temporarily unavailable.", "Wait a few minutes and try again.")
	CodeProviderServerError        = define("provider_server_error", OutcomeFailed, WhoProvider, "The service had an error.", "Wait a few minutes and try again.")
	CodeProviderUnreachable        = define("provider_unreachable", OutcomeFailed, WhoProvider, "We could not reach the service.", "Wait a few minutes and try again.")
	CodeLoginExpired               = define("login_expired", OutcomeFailed, WhoPerson, "A newer sign-in replaced this one.", "Finish the most recent sign-in, or start again.")
	CodeSharedCredentialRejected   = define("shared_credential_rejected", OutcomeFailed, WhoAdmin, "The server rejected your organization's shared sign-in.", "Update the shared credential for this server.")
	CodeStaticKeyRejected          = define("static_key_rejected", OutcomeFailed, WhoAdmin, "The server rejected the saved key.", "Update the key for this server.")

	// Upstream enterprise-managed sign-in.
	CodeConfigurationRequired    = define("configuration_required", OutcomeFailed, WhoAdmin, "Company-managed sign-in is not fully set up for this server.", "Finish the setup for this server.")
	CodeAccessDenied             = define("access_denied", OutcomeBlocked, WhoAdmin, "Your company sign-in service refused access to this server.", "Check this person is assigned to the app there.")
	CodeScopePolicyDenied        = define("scope_policy_denied", OutcomeBlocked, WhoAdmin, "Your company sign-in service refused these permissions.", "Check the permissions allowed for this server.")
	CodeInvalidTarget            = define("invalid_target", OutcomeFailed, WhoAdmin, "Your company sign-in service does not know this server.", "Check the server's connection in the sign-in service.")
	CodeMalformedAssertion       = define("malformed_assertion", OutcomeFailed, WhoAdmin, "The sign-in proof did not match this server's setup.", "Check the sign-in setup for this server.")
	CodeInsufficientScope        = define("insufficient_scope", OutcomeFailed, WhoAdmin, "The sign-in did not include the permissions needed.", "Check the permissions allowed for this server.")
	CodeMalformedResponse        = define("malformed_response", OutcomeFailed, WhoAdmin, "The sign-in service sent an answer we could not read.", "Check the sign-in setup for this server.")
	CodeTransientFailure         = define("transient_failure", OutcomeFailed, WhoSpeakeasy, "A temporary problem stopped the sign-in.", "Try again in a few minutes.")
	CodeIDPTransientFailure      = define("idp_transient_failure", OutcomeFailed, WhoProvider, "Your sign-in service had a temporary problem.", "Try again in a few minutes.")
	CodeProviderTransientFailure = define("provider_transient_failure", OutcomeFailed, WhoProvider, "The service had a temporary problem.", "Try again in a few minutes.")
	CodeReauthenticationRequired = define("reauthentication_required", OutcomeFailed, WhoPerson, "Your company sign-in is missing or has expired.", "Sign in again.")
	CodeIneligibleIdentity       = define("ineligible_identity", OutcomeBlocked, WhoAdmin, "This account cannot be used for company-managed sign-in here.", "Use an allowed account, or ask for this sign-in method to be allowed.")

	// Every MCP request.
	CodeToolPermissionDenied       = define("tool_permission_denied", OutcomeBlocked, WhoAdmin, "This person is not allowed to use this tool.", "Ask for permission to use the tool.")
	CodeMCPToolCallsPaused         = define("mcp_tool_calls_paused", OutcomeBlocked, WhoAdmin, "Tool use has been paused for this server.", "Ask when tool use will be turned back on.")
	CodePolicyBlocked              = define("policy_blocked", OutcomeBlocked, WhoAdmin, "An organization policy stopped this request.", "Ask about the policy if you think this is wrong.")
	CodeUsageLimitReached          = define("usage_limit_reached", OutcomeBlocked, WhoAdmin, "The organization reached its usage limit.", "Ask for the limit to be raised.")
	CodeToolNotApproved            = define("tool_not_approved", OutcomeBlocked, WhoPerson, "This tool was not picked when you connected.", "Connect again and pick this tool.")
	CodeInvalidBearerToken         = define("invalid_bearer_token", OutcomeFailed, WhoPerson, "Your sign-in for this server is no longer valid.", "Connect to the server again.")
	CodeAuthCheckUnavailable       = define("auth_check_unavailable", OutcomeFailed, WhoSpeakeasy, "We could not check your sign-in right now.", "Try again in a few minutes. You do not need to sign in again.")
	CodeInvalidRemoteSession       = define("invalid_remote_session", OutcomeFailed, WhoPerson, "Your connection to this service is missing or has expired.", "Reconnect to the service.")
	CodeCredentialMisconfigured    = define("credential_misconfigured", OutcomeFailed, WhoAdmin, "The connection to this service is not set up correctly.", "Check the connection settings for this server.")
	CodeRemoteSessionUnavailable   = define("remote_session_unavailable", OutcomeFailed, WhoProvider, "The service's sign-in system is temporarily unavailable.", "Wait a few minutes and try again.")
	CodeProviderRejected           = define("provider_rejected", OutcomeFailed, WhoPerson, "The service rejected your access.", "Reconnect. If it keeps happening, ask your admin.")
	CodeEMAProviderRejected        = define("ema_provider_rejected", OutcomeFailed, WhoAdmin, "The service refused the managed sign-in.", "Check the service's setup for managed sign-in.")
	CodeTunnelUnavailable          = define("tunnel_unavailable", OutcomeFailed, WhoAdmin, "The private connection is down.", "Check that its tunnel is running.")
	CodeKillswitchCheckUnavailable = define("killswitch_check_unavailable", OutcomeFailed, WhoSpeakeasy, "We could not check whether tool use is allowed.", "Try again in a few minutes.")
	CodeMemberToolsMissing         = defineResult("member_tools_missing", resultPartial, OutcomeFailed, WhoAdmin, "Some of this gateway's servers did not list their tools.", "Check those servers' status on the gateway.")

	// Refresh.
	CodeRefreshedUnverified              = defineResult("refreshed_unverified", ResultRefreshed, OutcomeConnected, WhoAdmin, "The sign-in was renewed, but the service could not be checked.", "If tool calls fail, check the service's connection.")
	CodeRefreshTokenExpired              = define("refresh_token_expired", OutcomeBlocked, WhoPerson, "Your sign-in reached its time limit.", "Connect to the server again.")
	CodeSignInRevoked                    = define("sign_in_revoked", OutcomeBlocked, WhoPerson, "Your sign-in was ended.", "Connect again. If it keeps happening, ask your admin.")
	CodeRefreshTokenRevoked              = define("refresh_token_revoked", OutcomeFailed, WhoPerson, "This connection was ended.", "Connect to the server again.")
	CodeRefreshTokenUnknownOrAlreadyUsed = define("refresh_token_unknown_or_already_used", OutcomeFailed, WhoPerson, "The app used an old or unknown sign-in.", "Connect to the server again.")
	CodeUpstreamReconnectRequired        = define("upstream_reconnect_required", OutcomeFailed, WhoPerson, "Your connection to the service needs renewing.", "Reconnect to the service.")
	CodeUpstreamEMARefused               = define("upstream_ema_refused", OutcomeFailed, WhoAdmin, "The service refused to renew the managed sign-in.", "Check the service's setup for managed sign-in.")
	CodeInvalidGrant                     = define("invalid_grant", OutcomeFailed, WhoPerson, "The service no longer accepts this sign-in.", "Reconnect to the service.")
	CodeRefreshInvalidClient             = define("refresh_invalid_client", OutcomeFailed, WhoPerson, "The service no longer recognizes this connection.", "Reconnect to the service.")
	CodeNoGrant                          = define("no_grant", OutcomeFailed, WhoPerson, "There is no saved sign-in to renew.", "Reconnect to the service.")
	CodeRefreshRejected                  = define("refresh_rejected", OutcomeFailed, WhoAdmin, "The service refused to renew the connection.", "Check the connection settings for this service.")
	CodeInternalError                    = define("internal_error", OutcomeFailed, WhoSpeakeasy, "Something went wrong on our side.", "Try again in a few minutes.")
	CodeOfflineNotRequested              = define("offline_not_requested", OutcomeFailed, WhoAdmin, "Company sign-in is not set to stay signed in.", "Turn on offline access in the sign-in setup.")
	CodeOfflineUnsupported               = define("offline_unsupported", OutcomeFailed, WhoAdmin, "The company sign-in service cannot keep people signed in.", "Check whether the sign-in service can allow offline access.")
	CodeRenewalRefused                   = define("renewal_refused", OutcomeFailed, WhoPerson, "The company sign-in service did not allow staying signed in.", "Sign in again when asked.")
	CodeRenewalMisconfigured             = define("renewal_misconfigured", OutcomeFailed, WhoAdmin, "Company sign-in renewal is not set up correctly.", "Check the sign-in setup for this organization.")
	CodeInvalidIdentity                  = define("invalid_identity", OutcomeFailed, WhoAdmin, "The company sign-in did not return a usable identity.", "Check the sign-in setup for this organization.")
	CodeRenewalRetryable                 = define("renewal_retryable", OutcomeFailed, WhoProvider, "The company sign-in service had a brief problem.", "Try again in a minute.")
	CodeRenewalAmbiguous                 = define("renewal_ambiguous", OutcomeFailed, WhoPerson, "We could not tell if your company sign-in was renewed.", "Sign in again.")

	// Gateway members.
	CodeNeedsSignIn        = defineResult("needs_sign_in", resultNeedsSignIn, OutcomeBlocked, WhoPerson, "You have not signed in to this service yet.", "Connect to the service.")
	CodeConsentNotApproved = defineResult("consent_not_approved", resultNeedsConsent, OutcomeBlocked, WhoPerson, "You have not approved this server for this connection.", "Connect again and approve the server.")
)
