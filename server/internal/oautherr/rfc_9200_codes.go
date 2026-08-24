package oautherr

// RFC 9200 (Authentication and Authorization for Constrained Environments,
// ACE-OAuth) §5.8.3 defines token endpoint error codes for proof-of-possession
// key negotiation.
const (
	// CodeIncompatibleACEProfiles is RFC 9200 §5.8.3: the client and the
	// resource server it requested an access token for do not share a common
	// ACE profile.
	CodeIncompatibleACEProfiles = "incompatible_ace_profiles"

	// CodeUnsupportedPopKey is RFC 9200 §5.8.3: the client submitted an
	// asymmetric proof-of-possession key in the token request that the
	// resource server cannot process.
	CodeUnsupportedPopKey = "unsupported_pop_key"
)
