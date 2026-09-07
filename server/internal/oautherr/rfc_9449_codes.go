package oautherr

// RFC 9449 (Demonstrating Proof of Possession, DPoP) defines error codes used
// by both the authorization server and protected resources.
const (
	// CodeInvalidDPoPProof is RFC 9449 §5 (token endpoint) and §7.1 (protected
	// resource): the DPoP proof JWT is missing, malformed, or otherwise fails
	// the validation checks of §4.3.
	CodeInvalidDPoPProof = "invalid_dpop_proof"

	// CodeUseDPoPNonce is RFC 9449 §8 (authorization server) and §9 (protected
	// resource): the server requires a server-provided nonce in the DPoP proof;
	// the client must retry with the value from the DPoP-Nonce response header.
	CodeUseDPoPNonce = "use_dpop_nonce"
)
