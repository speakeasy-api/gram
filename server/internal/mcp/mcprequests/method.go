package mcprequests

// MethodOther and MethodNone are the two synthetic buckets [ClampMethod]
// emits, mirroring mcpversions.Other and mcpversions.None: every point on the
// census carries a method dimension, so a breakdown by method accounts for all
// traffic. Neither can collide with a real method, which always contains a
// letter sequence the spec assigns; a client literally sending "other" is
// unrecognized and buckets into MethodOther like any other unknown value.
const (
	// MethodOther collects every unrecognized method: extension methods
	// outside the known list and methods introduced by spec revisions this
	// package does not know yet. Sustained growth here means either the known
	// list has gone stale or a client is sending garbage; both are worth a
	// look.
	MethodOther = "other"

	// MethodNone marks a request that carried no method at all, which is
	// malformed JSON-RPC. Kept distinct so that cohort is countable.
	MethodNone = "none"
)

// knownMethods is every client-to-server method named by a published MCP
// revision, 2024-11-05 through 2026-07-28 — including the tasks family, which
// 2025-11-25 added to the core spec as experimental (SEP-1686) before
// 2026-07-28 moved it to an extension. Gram does not implement all of them;
// the census is an observation instrument and records what clients send, not
// what Gram serves. A method missing from this list — a new spec revision's
// addition or an extension's — buckets into [MethodOther] until the list is
// updated, the same silent-staleness trade-off mcpversions.Clamp accepts for
// versions.
var knownMethods = map[string]struct{}{
	"completion/complete":              {},
	"initialize":                       {},
	"logging/setLevel":                 {},
	"notifications/cancelled":          {},
	"notifications/initialized":        {},
	"notifications/progress":           {},
	"notifications/roots/list_changed": {},
	"notifications/tasks/status":       {},
	"ping":                             {},
	"prompts/get":                      {},
	"prompts/list":                     {},
	"resources/list":                   {},
	"resources/read":                   {},
	"resources/subscribe":              {},
	"resources/templates/list":         {},
	"resources/unsubscribe":            {},
	"server/discover":                  {},
	"subscriptions/listen":             {},
	"tasks/cancel":                     {},
	"tasks/get":                        {},
	"tasks/list":                       {},
	"tasks/result":                     {},
	"tools/call":                       {},
	"tools/list":                       {},
}

// ClampMethod bounds a client-supplied JSON-RPC method name for use as a
// metric dimension: a known method passes through, an absent method becomes
// [MethodNone], and anything else becomes [MethodOther]. The result is always
// drawn from a fixed set, so a hostile client cannot mint unbounded series no
// matter what it sends.
func ClampMethod(method string) string {
	if method == "" {
		return MethodNone
	}
	if _, ok := knownMethods[method]; ok {
		return method
	}
	return MethodOther
}
