// Package jwks resolves third-party JSON Web Key Sets into verification
// keys. It is the single shared implementation behind the user-session
// authorization server's third-party assertion verification: client
// assertions (private_key_jwt, where the key set is published by an MCP
// client) and trusted issuer assertions (where it is published by a
// customer's identity provider). Everything security-relevant is identical
// between the two — the
// guardian-policied outbound fetch, the ban on private and symmetric key
// material, the algorithm-agnostic parse, selection by kid, and the
// rate-limited refresh when a kid is unknown — so it lives here exactly once.
// Only storage differs, and storage is a pluggable Cache policy.
//
// # Layering
//
// Resolver is the stateless core, mirroring the cimd package's shape: it
// owns fetching, parsing, screening, and telemetry, but no storage — the
// caller passes in the CacheState it has stored and applies the returned
// Result to its own storage. KeyResolver is the orchestrator consumers are
// meant to use: it binds a Resolver to a Cache and a refresh rate limiter
// and exposes the one call that matters, VerificationKey. The unknown-kid
// refresh limiter is the single most important security property here — an
// unauthenticated caller sending assertions with random kids must not be
// able to turn the token endpoint into an outbound fetch amplifier — and it
// is only enforced inside KeyResolver. Callers that use Resolver directly
// (a durable serve-stale policy wrapping its own refresh loop) take on that
// responsibility themselves and must not expose an unthrottled refetch to
// unauthenticated input.
//
// # No origin rule on key set URLs
//
// A jwks_uri is deliberately not required to relate to the origin of the
// party that published it. No specification imposes such a rule — RFC 7591
// (client metadata) and RFC 8414 (issuer metadata) constrain jwks_uri only
// to the https scheme, and the CIMD draft's §8.1 explicitly preserves
// unrestricted relationships between a document's URLs — and real
// deployments depend on crossing hosts, from Google's issuer/key-host split
// to platform-hosted client documents naming key sets on the client's own
// domain. The trust binding is declarative instead: the authenticated
// document that names the jwks_uri vouches for it wherever it is hosted,
// and a signature only ever verifies for the party holding the private
// keys. The fetch policy (guardian SSRF screening, size cap, refresh rate
// limit) applies to every remote source identically.
package jwks
