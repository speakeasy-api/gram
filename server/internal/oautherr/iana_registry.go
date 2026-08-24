package oautherr

// registeredCodes is the set of every Code constant in this package: the
// error codes registered in the IANA OAuth Extensions Error Registry by IETF
// RFCs. TestIANARegistryIsComplete fails when a Code constant is added
// without an entry here, so the set cannot silently fall behind the
// constants.
var registeredCodes = map[string]struct{}{
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

// IsIANARegisteredCode reports whether code is an OAuth 2.0 error code that
// an IETF RFC has registered in the IANA OAuth Extensions Error Registry, at
// any endpoint. Codes the registry lists from other bodies (OpenID Connect,
// OpenID Federation, UMA 2.0, OpenID4VP) and RFC 6749 §8.5 extension codes are
// not registered here. The comparison is exact: registry names are lowercase.
func IsIANARegisteredCode(code string) bool {
	_, ok := registeredCodes[code]
	return ok
}
