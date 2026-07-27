// Package gcpauth resolves the effective identity ("who am I") behind a GCP IAM
// external credential: given how the credential authenticates, it reports the
// principal that identity resolves to and, in doing so, proves the identity is
// reachable and usable.
//
// The ambient attached identity and service-account impersonation are resolved
// today. Workload Identity Federation is recognized and reported as
// not-yet-supported (ErrUnsupportedMode) rather than silently ignored, leaving
// the seam ready to extend.
package gcpauth
