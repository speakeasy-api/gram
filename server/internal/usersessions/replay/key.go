package replay

import (
	"crypto/sha256"
	"encoding/base64"
	"strconv"
)

// Key identifies one single-use assertion identifier. The parts are hashed
// together into the storage key, so no part can be confused with another
// however the values are punctuated.
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

	// Party is who minted the assertion: the client_id for a client
	// assertion, or a stable server-side reference to the trusted issuer
	// row for a workload assertion. It keeps one tenant's identifiers from
	// colliding with, or evicting, another's.
	//
	// A workload's Party must be the issuer reference rather than the
	// issuer URL, for the same reason Issuer must not be one.
	Party string

	// Subject narrows the identifier within Party, and is empty when Party
	// already names the whole minting party.
	//
	// A client assertion leaves it empty: RFC 7523 §3 makes iss, sub, and
	// client_id one value, so Party is the whole identity. A workload
	// assertion sets it to the external subject, because one platform
	// issuer vouches for many workloads. A jti is only unique per issuer,
	// never per workload, so without this two workloads under one issuer
	// would share a keyspace and could burn each other's identifiers.
	Subject string

	// ID is the assertion's jti claim, verbatim.
	ID string
}

// storageKey is the hashed, length-prefixed encoding of a Key.
//
// Length prefixes make the encoding injective: without them a Party of
// "a:b" with an ID of "c" and a Party of "a" with an ID of "b:c" would
// collide, letting one party burn another's identifiers. The same holds
// across the Party/Subject boundary, which is why Subject is a part of its
// own rather than folded into Party by the caller. Hashing then bounds the
// result, which matters because every part but Issuer arrives from outside
// and a CIMD client_id is a URL that may run to kilobytes.
//
// An empty Subject contributes no part at all, rather than a zero-length
// one. Two replicas running different builds share this keyspace during a
// rolling deploy, so a key whose parts have not changed must hash the same
// under both: emitting a "0:" segment for the absent Subject would move
// every client-assertion key at once, orphaning the holds already taken and
// letting an assertion spent against one replica be spent again against
// another for as long as the original hold had left to run.
//
// Omitting it stays injective because the encoding is self-delimiting — a
// length, a colon, then exactly that many bytes — so a three-part sequence
// cannot decode as a four-part one however the values are punctuated.
func (k Key) storageKey() string {
	parts := make([]string, 0, 4)
	parts = append(parts, k.Issuer, k.Party)
	if k.Subject != "" {
		parts = append(parts, k.Subject)
	}
	parts = append(parts, k.ID)

	sum := sha256.New()
	for _, part := range parts {
		sum.Write([]byte(strconv.Itoa(len(part))))
		sum.Write([]byte(":"))
		sum.Write([]byte(part))
	}
	return base64.RawURLEncoding.EncodeToString(sum.Sum(nil))
}
