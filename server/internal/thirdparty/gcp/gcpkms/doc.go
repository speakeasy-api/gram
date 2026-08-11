// Package gcpkms signs with GCP Cloud KMS asymmetric keys whose private half
// never leaves the provider.
//
// The package is deliberately GCP-concrete. The cross-provider seam is
// jose.OpaqueSigner, which NewSigner returns and which every go-jose JWS and JWT
// path already accepts, so an AWS KMS equivalent can sit beside this package
// without either of them growing a shared abstraction.
//
// Only RS256 and ES256 are supported. That is an interoperability choice rather
// than a technical limit: they are the two algorithms every OIDC and MCP
// verifier implements. A key signing with anything else is rejected by name, so
// a key version configured for RSA-PSS reports that it is PS256 instead of
// silently producing signatures no verifier accepts.
//
// Everything here is scoped to keys whose KMS purpose is ASYMMETRIC_SIGN, which
// is why the identifiers say "signing" and why ValidateKeyVersionName requires a
// cryptoKeyVersions suffix. A key's purpose is fixed at creation, so symmetric
// ENCRYPT_DECRYPT support would not extend these types: it addresses keys at the
// cryptoKeys level (KMS picks the version, and the ciphertext records which one),
// has no public half, and so needs its own interface and validator. The parts
// worth sharing at that point are the token source and the authenticated
// connection setup in NewSigningClient, not this API.
package gcpkms
