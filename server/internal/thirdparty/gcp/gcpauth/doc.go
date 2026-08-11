// Package gcpauth turns a GCP IAM external credential into the two things
// callers need from it.
//
// ResolvePrincipal answers "who am I": it reports the principal the credential's
// identity resolves to and, in doing so, proves that identity is reachable and
// usable by minting a token. TokenSource returns the oauth2.TokenSource that
// authenticates Google API calls made as that identity, for callers that need to
// actually use the credential rather than describe it.
//
// The ambient attached identity and service-account impersonation are supported
// today. Workload Identity Federation is recognized and reported as
// not-yet-supported (ErrUnsupportedMode) rather than silently ignored, leaving
// the seam ready to extend.
package gcpauth
