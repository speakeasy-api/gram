package oautherr

// ietfRegisteredCodes is the set of every Code constant in this package: the
// error codes registered in the IANA OAuth Extensions Error Registry by IETF
// RFCs. TestIETFRegistryIsComplete fails when a Code constant is added
// without an entry here, so the set cannot silently fall behind the
// constants.
var ietfRegisteredCodes = map[string]struct{}{
	CodeAccessDenied:                   {},
	CodeAuthorizationPending:           {},
	CodeExpiredToken:                   {},
	CodeIncompatibleACEProfiles:        {},
	CodeInsufficientScope:              {},
	CodeInsufficientUserAuthentication: {},
	CodeInvalidAuthorizationDetails:    {},
	CodeInvalidClient:                  {},
	CodeInvalidClientMetadata:          {},
	CodeInvalidDPoPProof:               {},
	CodeInvalidGrant:                   {},
	CodeInvalidRedirectURI:             {},
	CodeInvalidRequest:                 {},
	CodeInvalidScope:                   {},
	CodeInvalidSoftwareStatement:       {},
	CodeInvalidTarget:                  {},
	CodeInvalidToken:                   {},
	CodeServerError:                    {},
	CodeSlowDown:                       {},
	CodeTemporarilyUnavailable:         {},
	CodeUnapprovedSoftwareStatement:    {},
	CodeUnauthorizedClient:             {},
	CodeUnsupportedGrantType:           {},
	CodeUnsupportedPoPKey:              {},
	CodeUnsupportedResponseType:        {},
	CodeUnsupportedTokenType:           {},
	CodeUseDPoPNonce:                   {},
}

// IsIETFRegisteredCode reports whether code is an OAuth 2.0 error code that
// an IETF RFC has registered in the IANA OAuth Extensions Error Registry, at
// any endpoint. Codes the registry lists from other bodies (OpenID Connect,
// OpenID Federation, UMA 2.0, OpenID4VP) are IANA-registered but not IETF
// codes and are excluded, as are RFC 6749 §8.5 extension codes. The
// comparison is exact: registry names are lowercase.
func IsIETFRegisteredCode(code string) bool {
	_, ok := ietfRegisteredCodes[code]
	return ok
}

// RegisteredCodeLabel preserves only explicitly allowlisted IETF and OpenID
// Connect Core error codes. Arbitrary caller text never becomes a label.
func RegisteredCodeLabel(code string) string {
	if IsIETFRegisteredCode(code) {
		return code
	}
	// OpenID Connect Core §3.1.2.6. Keep this separate from the IETF registry:
	// these standardized codes are safe labels, but are not IETF registrations.
	switch code {
	case OIDCCodeInteractionRequired, OIDCCodeLoginRequired, OIDCCodeAccountSelectionRequired,
		OIDCCodeConsentRequired, OIDCCodeInvalidRequestURI, OIDCCodeInvalidRequestObject,
		OIDCCodeRequestNotSupported, OIDCCodeRequestURINotSupported, OIDCCodeRegistrationNotSupported:
		return code
	default:
		return "unregistered"
	}
}
