// Package mcpversions is the single registry of MCP protocol revision
// identifiers Gram recognizes.
//
// Protocol versions reach Gram as client-supplied input — the
// `MCP-Protocol-Version` HTTP header on Streamable HTTP, or the
// `protocolVersion` field of an `initialize` request under the handshake-based
// revisions. That makes the value unbounded in principle: a broken or hostile
// client can send anything. This package draws the line between the revisions
// we know about and everything else, so callers can keep raw values on spans
// (high cardinality, sampled, diagnostic) while metric dimensions stay bounded
// by [Clamp] (low cardinality, unsampled, aggregatable).
//
// The list is deliberately inclusive: recognizing a revision Gram does not
// otherwise implement costs nothing, whereas omitting one that real clients
// send silently buckets live traffic into [Other] and hides it.
package mcpversions
