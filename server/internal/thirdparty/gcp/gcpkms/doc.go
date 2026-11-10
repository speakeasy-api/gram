// Package gcpkms signs and encrypts with GCP Cloud KMS keys whose key material
// never leaves the provider.
//
// The package is deliberately GCP-concrete. For signing, the cross-provider
// seam is jose.OpaqueSigner, which NewSigner returns and which every go-jose JWS
// and JWT path already accepts, so an AWS KMS equivalent can sit beside this
// package without either of them growing a shared abstraction.
//
// Only RS256 and ES256 are supported for signing. That is an interoperability
// choice rather than a technical limit: they are the two algorithms every OIDC
// and MCP verifier implements. A key signing with anything else is rejected by
// name, so a key version configured for RSA-PSS reports that it is PS256
// instead of silently producing signatures no verifier accepts.
//
// Signing and encryption are separate surfaces because a key's purpose is
// fixed at creation and the two address keys differently:
//
//   - SigningClient works with ASYMMETRIC_SIGN keys at the cryptoKeyVersions
//     level, since a published public key belongs to exactly one version.
//     ValidateKeyVersionName enforces the version suffix.
//   - EncryptionClient works with ENCRYPT_DECRYPT keys at the cryptoKeys level:
//     KMS encrypts with the primary version and the ciphertext records which
//     version to decrypt with. ValidateKeyName enforces the absence of one.
//
// The two share the token source, the authenticated connection setup, the
// per-key IAM grant helpers and version disabling.
package gcpkms
