package id

import "github.com/google/uuid"

// speakeasyNamespace is a UUIDv5 derived from the DNS namespace + "speakeasy.com".
// This MUST match the registry's namespace so both systems independently derive
// the same organization ID from a given WorkOS organization ID.
var speakeasyNamespace = uuid.NewSHA1(uuid.NameSpaceDNS, []byte("speakeasy.com"))

// FromWorkOSID returns a deterministic UUIDv5 organization ID derived from a
// WorkOS organization ID. Given the same input, both Gram and the Speakeasy
// Registry produce the same output — no cross-system communication needed.
func FromWorkOSID(workosOrgID string) string {
	return uuid.NewSHA1(speakeasyNamespace, []byte(workosOrgID)).String()
}
