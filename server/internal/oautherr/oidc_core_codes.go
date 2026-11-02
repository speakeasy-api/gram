package oautherr

// OpenID Connect Core 1.0 section 3.1.2.6 error codes. Some providers also send
// them from the token endpoint when a refresh needs the person to sign in.
const (
	// OIDCCodeInteractionRequired means the provider needs the person to interact.
	OIDCCodeInteractionRequired = "interaction_required"

	// OIDCCodeLoginRequired means the provider needs the person to sign in.
	OIDCCodeLoginRequired = "login_required"

	// OIDCCodeConsentRequired means the provider needs the person's consent.
	OIDCCodeConsentRequired = "consent_required"

	// OIDCCodeAccountSelectionRequired means the person must choose an account.
	OIDCCodeAccountSelectionRequired = "account_selection_required"
)
