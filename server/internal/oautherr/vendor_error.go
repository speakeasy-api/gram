package oautherr

import (
	"encoding/json"
	"strings"
)

// vendorTokenErrorParsers lists the non-RFC token endpoint error shapes this
// package recognizes, one parser per shape. Each parser decodes the body into
// its shape's struct and reports ok only when the body actually carried that
// shape; the decode error itself is ignored because encoding/json fills every
// member it can, so an RFC-shaped or foreign body simply leaves the shape's
// required members empty. Adding a shape means adding a struct, a parser, and
// an entry here.
var vendorTokenErrorParsers = []func(body []byte) (RFC6749Error, bool){
	parseVendorErrorsStringArray,
	parseVendorErrorCodeMessageObject,
}

// parseVendorTokenError tries each known vendor shape in order and returns the
// first reading.
func parseVendorTokenError(body []byte) (RFC6749Error, bool) {
	for _, parse := range vendorTokenErrorParsers {
		if e, ok := parse(body); ok {
			return e, true
		}
	}
	return RFC6749Error{Code: "", Description: "", URI: ""}, false
}

// vendorErrorsStringArray is the {"errors": ["<text>", ...]} envelope: an
// array of free-text strings under a plural "errors" key. It is an API-wide
// vendor convention rather than RFC 9457 Problem Details or JSON:API. The
// known emitter is Datadog, whose token endpoint
// (https://app.datadoghq.com/oauth2/v1/token) wraps its RFC 6749 §5.2 code and
// description in this envelope as one string ("invalid_grant - Invalid or
// expired refresh token or code verifier.").
type vendorErrorsStringArray struct {
	// Errors holds one free-text entry per error. Entries that are not
	// strings decode as empty and are ignored.
	Errors []string `json:"errors"`
}

// parseVendorErrorsStringArray reads flattened OAuth 2.0 errors out of a
// vendorErrorsStringArray body. Entries that do not start with a registered
// code are arbitrary vendor text and are dropped. A CodeInvalidGrant entry wins over
// any other regardless of position; otherwise the first recognized entry does.
func parseVendorErrorsStringArray(body []byte) (RFC6749Error, bool) {
	var v vendorErrorsStringArray
	_ = json.Unmarshal(body, &v)

	var first RFC6749Error
	var found bool
	for _, entry := range v.Errors {
		code, description, ok := splitFlattenedError(entry)
		if !ok {
			continue
		}
		e := RFC6749Error{Code: code, Description: description, URI: ""}
		if code == CodeInvalidGrant {
			return e, true
		}
		if !found {
			first, found = e, true
		}
	}
	return first, found
}

// vendorErrorCodeMessageObject is the {"error": {"code": "<vendor code>",
// "message": "<text>"}} envelope: the RFC 6749 "error" key holding an object
// instead of a code string, with a vendor code and human-readable message
// inside it. The known emitter is Dub, whose token endpoint
// (https://api.dub.co/oauth/token) responds 401 with this API-wide envelope
// (plus a "doc_url") and code "unauthorized" for every refresh failure,
// never emitting invalid_grant.
type vendorErrorCodeMessageObject struct {
	// Error is the nested vendor error.
	Error vendorCodeMessage `json:"error"`
}

// vendorCodeMessage is a vendor error code paired with its human-readable
// message. It is both the nested object inside vendorErrorCodeMessageObject
// and, once normalized by (vendorCodeMessage).normalized, the key of
// vendorDeadGrantErrors.
type vendorCodeMessage struct {
	// Code is the vendor's error code, which is not an RFC 6749 §5.2 code.
	Code string `json:"code"`

	// Message is the vendor's human-readable message.
	Message string `json:"message"`
}

// normalized returns the pair with surrounding whitespace trimmed from both
// members and any trailing period trimmed from Message, so a provider changing
// that punctuation does not silently break table recognition.
func (v vendorCodeMessage) normalized() vendorCodeMessage {
	message := strings.TrimSpace(v.Message)
	return vendorCodeMessage{
		Code:    strings.TrimSpace(v.Code),
		Message: strings.TrimSpace(strings.TrimRight(message, ".")),
	}
}

// vendorDeadGrantErrors is the last-chance fallback for token endpoints that
// do not follow RFC 6749 §5.2 at all: they signal a dead refresh grant with a
// vendor error code and a free-text message. Each entry is a message the
// provider's token endpoint is known to send only when the refresh grant can
// never succeed again (the token is unknown or expired, the installation was
// removed, or the grant belongs to another client, which RFC 6749 §5.2 also
// defines as invalid_grant). Client authentication failures from the same
// providers (missing or wrong client credentials, unknown client_id) are
// deliberately absent: those are fixed by correcting the client configuration
// and must not be mistaken for a dead grant.
//
// Matching is exact on purpose. A looser keyword match would eventually
// misclassify a client-authentication message as a dead grant, and a consumer
// acting on that (dropping a still-usable refresh token) forces every affected
// user to reconnect.
var vendorDeadGrantErrors = map[vendorCodeMessage]struct{}{
	// Dub.
	{Code: "unauthorized", Message: "Refresh token not found"}:            {},
	{Code: "unauthorized", Message: "Refresh token expired"}:              {},
	{Code: "unauthorized", Message: "Integration installation not found"}: {},
	{Code: "unauthorized", Message: "Client ID mismatch"}:                 {},
}

// parseVendorErrorCodeMessageObject normalizes a vendorErrorCodeMessageObject
// body. Only an exact vendorDeadGrantErrors match promotes it to
// CodeInvalidGrant; any other object with a code is surfaced with that vendor
// code and message so the failure stays legible, but nothing about it is an
// RFC 6749 code. ok is false when the body does not carry this shape or the
// nested code is empty.
func parseVendorErrorCodeMessageObject(body []byte) (RFC6749Error, bool) {
	var v vendorErrorCodeMessageObject
	_ = json.Unmarshal(body, &v)

	message := strings.TrimSpace(v.Error.Message)
	key := v.Error.normalized()
	if _, dead := vendorDeadGrantErrors[key]; dead {
		return RFC6749Error{Code: CodeInvalidGrant, Description: message, URI: ""}, true
	}
	if key.Code == "" {
		return RFC6749Error{Code: "", Description: "", URI: ""}, false
	}
	return RFC6749Error{Code: key.Code, Description: message, URI: ""}, true
}

// splitFlattenedError recognizes vendor free text that begins with an OAuth
// 2.0 error code registered by an IETF RFC, optionally followed by a separator
// and description ("invalid_grant - Invalid or expired refresh token."). The
// code must end at a byte outside the IANA registry alphabet so that
// "invalid_grant_extra" is not read as invalid_grant. The description is the
// remainder with leading whitespace and "-", ":" or "." separators removed.
// Non-IETF registry entries (OpenID Connect, UMA 2.0 and similar) and RFC 6749
// §8.5 extension codes are not split out of free text, since an unrecognized
// prefix cannot be told apart from an arbitrary vendor message.
func splitFlattenedError(s string) (code, description string, ok bool) {
	s = strings.TrimSpace(s)
	end := len(s)
	for i := range len(s) {
		if !isIANARegisteredCodeByte(s[i]) {
			end = i
			break
		}
	}
	code = s[:end]
	if !IsIETFRegisteredCode(code) {
		return "", "", false
	}
	description = strings.TrimSpace(strings.TrimLeft(s[end:], " \t-:."))
	return code, description, true
}

// isIANARegisteredCodeByte reports whether b belongs to the alphabet of the
// error codes in the IANA OAuth Extensions Error Registry, which are all
// lowercase ASCII letters, digits and underscore. That alphabet, not the RFC
// grammar, is what delimits a code inside vendor free text: RFC 6749 §5.2 and
// Appendix A.7 allow the "error" value any NQSCHAR (%x20-21 / %x23-5B /
// %x5D-7E), a set that includes space and hyphen and so cannot mark where
// "invalid_grant" ends in "invalid_grant - text".
func isIANARegisteredCodeByte(b byte) bool {
	return b == '_' || (b >= 'a' && b <= 'z') || (b >= '0' && b <= '9')
}
