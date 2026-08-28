package replay

import (
	"crypto/sha256"
	"encoding/base64"
	"strconv"
)

// Key identifies one single-use assertion identifier. The three parts are
// hashed together into the storage key, so no part can be confused with
// another however the values are punctuated.
type Key struct {
	// Issuer is the authorization server the assertion was presented to,
	// as a stable server-side identity such as a row identifier.
	//
	// It MUST NOT be derived from the request, and a URL is the trap to
	// avoid: one endpoint reachable on both its custom domain and the
	// default host would produce two different Issuer values for the same
	// server, and a single intercepted assertion could then be spent once
	// per reachable hostname.
	Issuer string

	// Client is the party the assertion authenticates, normally the
	// client_id. It keeps one tenant's identifiers from colliding with,
	// or evicting, another's.
	Client string

	// ID is the assertion's jti claim, verbatim.
	ID string
}

// storageKey is the hashed, length-prefixed encoding of a Key.
//
// Length prefixes make the encoding injective: without them a Client of
// "a:b" with an ID of "c" and a Client of "a" with an ID of "b:c" would
// collide, letting one client burn another's identifiers. Hashing then bounds
// the result, which matters because both Client and ID arrive from outside and
// a CIMD client_id is a URL that may run to kilobytes.
func (k Key) storageKey() string {
	sum := sha256.New()
	for _, part := range []string{k.Issuer, k.Client, k.ID} {
		sum.Write([]byte(strconv.Itoa(len(part))))
		sum.Write([]byte(":"))
		sum.Write([]byte(part))
	}
	return base64.RawURLEncoding.EncodeToString(sum.Sum(nil))
}
