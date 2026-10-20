package mcprequests

import "github.com/speakeasy-api/gram/server/internal/mcp/mcpversions"

// MethodOther and MethodNone are the two synthetic buckets [ClampMethod]
// emits, mirroring mcpversions.Other and mcpversions.None: every point on the
// census carries a method dimension, so a breakdown by method accounts for all
// traffic. Neither can collide with a real method, which always contains a
// letter sequence the spec assigns; a client literally sending "other" is
// unrecognized and buckets into MethodOther like any other unknown value.
const (
	// MethodOther collects every unrecognized method: extension methods and
	// methods introduced by spec revisions mcpversions does not know yet.
	// Sustained growth here means either mcpversions has gone stale or a
	// client is sending garbage; both are worth a look.
	MethodOther = "other"

	// MethodNone marks a request that carried no method at all, which is
	// malformed JSON-RPC. Kept distinct so that cohort is countable.
	MethodNone = "none"
)

// ClampMethod bounds a client-supplied JSON-RPC method name for use as a
// metric dimension: a method [mcpversions.KnownMethod] recognizes passes
// through, an absent method becomes [MethodNone], and anything else becomes
// [MethodOther]. The result is always drawn from a fixed set, so a hostile
// client cannot mint unbounded series no matter what it sends.
//
// Recognized methods include those Speakeasy does not implement: the census is an
// observation instrument and records what clients send, not what Speakeasy serves.
// A method a new revision or an extension adds buckets into [MethodOther]
// until mcpversions learns it, the same silent-staleness trade-off
// mcpversions.Clamp accepts for versions.
func ClampMethod(method string) string {
	if method == "" {
		return MethodNone
	}
	if mcpversions.KnownMethod(method) {
		return method
	}
	return MethodOther
}
