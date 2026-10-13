package identitychaining

// Reason is the normalized, credential-free cause of an outcome.
type Reason string

const (
	ReasonSuccess Reason = "success"

	// ReasonConsentRequired means local eligibility passed, but no MCP grant
	// exists yet and no downstream token has been issued.
	ReasonConsentRequired Reason = "consent_required"

	// ReasonNotApplicable: no ready binding names the upstream, so the
	// interactive path applies unchanged.
	ReasonNotApplicable Reason = "not_applicable"

	// ReasonBindingNotReady: the binding no longer passes readiness, so the
	// interactive path applies unchanged.
	ReasonBindingNotReady Reason = "binding_not_ready"

	ReasonConfigurationRequired    Reason = "configuration_required"
	ReasonReauthenticationRequired Reason = "reauthentication_required"
	ReasonInvalidTarget            Reason = "invalid_target"
	ReasonScopePolicyDenied        Reason = "scope_policy_denied"
	ReasonAccessDenied             Reason = "access_denied"
	ReasonInvalidClient            Reason = "invalid_client"
	ReasonInvalidGrant             Reason = "invalid_grant"
	ReasonMalformedAssertion       Reason = "malformed_assertion"
	ReasonMalformedResponse        Reason = "malformed_response"
	ReasonInsufficientScope        Reason = "insufficient_scope"
	ReasonTransientFailure         Reason = "transient_failure"
	ReasonUnknownRejection         Reason = "unknown_rejection"

	// ReasonProviderUnavailable: no identity provider allowed for the vendor
	// can serve the request, so the interactive path applies unchanged.
	ReasonProviderUnavailable Reason = "provider_unavailable"

	// ReasonStaleConfiguration: the binding, delegation or tenant changed
	// during the attempt, so its result was discarded.
	ReasonStaleConfiguration Reason = "stale_configuration"
)
