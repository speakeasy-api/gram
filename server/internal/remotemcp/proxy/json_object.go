package proxy

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// object is a JSON object held as its members, used by the wire types in wire.go
// to read and write a payload without committing to a fixed set of member names.
type object map[string]json.RawMessage

// decodeObject decodes payload as a JSON object. An empty payload decodes to an
// empty object, so a setter can populate a member on a payload the peer omitted
// entirely.
//
// A JSON `null` is rejected rather than silently accepted: unmarshaling it into a
// map sets the map to nil instead of failing, which would turn every subsequent
// member write into a panic.
func decodeObject(payload json.RawMessage) (object, error) {
	members := object{}
	if len(payload) == 0 {
		return members, nil
	}

	if err := json.Unmarshal(payload, &members); err != nil {
		return nil, fmt.Errorf("decode payload as a JSON object: %w", err)
	}
	if members == nil {
		return nil, errors.New("payload is null, not a JSON object")
	}

	return members, nil
}

// encode re-encodes the object. Member order is not preserved, and a payload that
// repeated a member name has already collapsed to the single value Go's decoder
// kept — see the package comment in wire.go for why that is deliberate.
func (o object) encode() (json.RawMessage, error) {
	encoded, err := json.Marshal(o)
	if err != nil {
		return nil, fmt.Errorf("encode JSON object: %w", err)
	}

	return encoded, nil
}

// Caching hints on a list result, added in MCP 2026-07-28. See
// https://modelcontextprotocol.io/specification/2026-07-28/server/utilities/caching.
const (
	cacheScopeMember = "cacheScope"
	ttlMsMember      = "ttlMs"

	// cacheScopePrivate confines a cached result to the authorization context
	// that fetched it. The spec's counterpart, "public", declares the result
	// free of user-specific data and lets any client, shared gateway, or
	// caching proxy serve it to any user — explicitly across access tokens, and
	// explicitly for results from an authenticated endpoint.
	cacheScopePrivate = `"private"`

	// ttlMsStale tells the client to treat the result as immediately stale. The
	// spec defines 0 as exactly that, and defines an absent ttlMs as defaulting
	// to it.
	ttlMsStale = `0`
)

// confineToCaller marks a list result the proxy has filtered for one caller as
// cacheable only within that caller's authorization context.
//
// A filtered list is user-specific by construction even when the upstream's own
// inventory was not, so relaying the upstream's `cacheScope: "public"` would
// authorize any cache between Gram and the model to serve one caller's permitted
// tools to a different caller — the sharing that scope permits and that the spec
// names as a security consideration. "private" is the scope the spec calls
// correct for "filtered list results that vary per user".
//
// The TTL goes to zero for a related reason: the upstream's TTL describes how
// long its own inventory stays fresh, which says nothing about how long Gram's
// filtering of it does. A revoked grant changes the correct answer at once and
// Gram cannot invalidate a cache the client already holds. Call-time
// authorization stops a withdrawn tool from being executed but not from being
// listed, and not listing it is what the filter is for.
//
// Both are set only when the upstream sent a caching hint at all. An upstream on
// an older revision sends neither member, and a client on that revision has no
// caching to authorize — introducing members the revision in effect does not
// define would be Gram inventing wire fields rather than carrying them.
func (o object) confineToCaller() {
	_, hasScope := o[cacheScopeMember]
	_, hasTTL := o[ttlMsMember]

	// A case-variant alias would be read in place of the value written below by
	// any parser that folds member names, leaving the result still marked
	// publicly cacheable. An alias also counts as the upstream having sent a
	// hint, so its presence still triggers the confinement.
	for name := range o {
		switch {
		case name == cacheScopeMember || name == ttlMsMember:
		case strings.EqualFold(name, cacheScopeMember):
			hasScope = true
			delete(o, name)
		case strings.EqualFold(name, ttlMsMember):
			hasTTL = true
			delete(o, name)
		}
	}

	if !hasScope && !hasTTL {
		return
	}

	o[cacheScopeMember] = json.RawMessage(cacheScopePrivate)
	o[ttlMsMember] = json.RawMessage(ttlMsStale)
}
