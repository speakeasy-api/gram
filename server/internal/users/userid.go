package users

import "github.com/google/uuid"

// speakeasyNamespace is a UUIDv5 derived from the DNS namespace + "speakeasy.com".
// This MUST match the registry's namespace so both systems independently derive
// the same user ID from a given WorkOS user ID.
var speakeasyNamespace = uuid.NewSHA1(uuid.NameSpaceDNS, []byte("speakeasy.com"))

// UserIDFromWorkOSID returns a deterministic UUIDv5 user ID derived from a
// WorkOS user ID. Given the same input, both Gram and the Speakeasy Registry
// produce the same output — no cross-system communication needed.
func UserIDFromWorkOSID(workosID string) string {
	return uuid.NewSHA1(speakeasyNamespace, []byte(workosID)).String()
}
