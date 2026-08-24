package oautherr

// RFC 7009 (Token Revocation) §2.2.1 extends the RFC 6749 §5.2 token endpoint
// error codes for the revocation endpoint.
const (
	// CodeUnsupportedTokenType is RFC 7009 §2.2.1: the authorization server
	// does not support the revocation of the presented token type.
	CodeUnsupportedTokenType = "unsupported_token_type"
)
