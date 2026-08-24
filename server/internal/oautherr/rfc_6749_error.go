package oautherr

import (
	"encoding/json"
	"strings"
)

// RFC6749Error is the OAuth 2.0 error response RFC 6749 defines: the "error"
// code with the optional "error_description" and "error_uri" members. RFC 6749
// §5.2 returns it as the JSON body of a token endpoint error and §4.1.2.1 as
// query parameters on the authorization endpoint redirect; RFC 7009 §2.2.1 and
// RFC 7591 §3.2.2 reuse the same shape for the revocation and registration
// endpoints. Non-RFC bodies that ParseTokenError recognizes are normalized onto
// the same members so consumers reason about one shape.
type RFC6749Error struct {
	// Code is the REQUIRED "error" member: a single ASCII error code, normally
	// one of the Code constants in this package or an RFC 6749 §8.5 extension
	// code.
	Code string `json:"error"`

	// Description is the OPTIONAL "error_description" member: human-readable
	// ASCII text intended for the client developer rather than the end user.
	Description string `json:"error_description,omitempty"`

	// URI is the OPTIONAL "error_uri" member: a URI identifying a
	// human-readable web page with information about the error.
	URI string `json:"error_uri,omitempty"`
}

// Error renders the code with its description ("invalid_grant: token
// revoked"), falling back to the URI when there is no description and to the
// bare code when there is neither.
func (e RFC6749Error) Error() string {
	switch {
	case e.Description != "":
		return e.Code + ": " + e.Description
	case e.URI != "":
		return e.Code + ": " + e.URI
	default:
		return e.Code
	}
}

// ParseTokenError decodes the body of an OAuth 2.0 token endpoint error
// response. The RFC 6749 §5.2 JSON object is tried first; when the body is not
// RFC-shaped, the known vendor envelopes are tried and normalized onto
// RFC6749Error.
//
// When both readings produce a result (an RFC "error" member next to a vendor
// "errors" array), a CodeInvalidGrant reading wins regardless of which parser
// produced it: it is the one code that tells a client its grant can never
// succeed again, so it must not be masked by a less specific sibling.
// Otherwise the RFC reading wins.
//
// ok is false when the body carries no recognizable error, in which case the
// returned RFC6749Error is the zero value.
func ParseTokenError(body []byte) (RFC6749Error, bool) {
	rfc, rfcOK := parseRFC6749TokenError(body)
	vendor, vendorOK := parseVendorTokenError(body)
	switch {
	case rfcOK && rfc.Code == CodeInvalidGrant:
		return rfc, true
	case vendorOK && vendor.Code == CodeInvalidGrant:
		return vendor, true
	case rfcOK:
		return rfc, true
	case vendorOK:
		return vendor, true
	default:
		return RFC6749Error{Code: "", Description: "", URI: ""}, false
	}
}

// parseRFC6749TokenError decodes the literal RFC 6749 §5.2 object. The decode
// error is ignored on purpose: encoding/json fills every member it can and
// reports only the first type mismatch, so a malformed member (or a vendor
// extension of the wrong shape beside the RFC members) does not discard a
// valid "error" code. A bare unregistered code is kept verbatim, since RFC 6749
// §8.5 permits extension codes.
func parseRFC6749TokenError(body []byte) (RFC6749Error, bool) {
	var e RFC6749Error
	_ = json.Unmarshal(body, &e)
	e.Code = strings.TrimSpace(e.Code)
	return e, e.Code != ""
}
