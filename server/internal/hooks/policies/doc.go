// Package policies is the router-backed ingest decision layer: one file per
// policy, each exporting a constructor that takes the enforcement primitives
// it needs as plain func values and returns the agenthooks handler, plus the
// actor-resolution middleware.
//
// The stages are thin adapters over the enforcement primitives the hooks
// service implements (risk scans, shadow-MCP evaluation, block-page URL
// minting) — they translate the primitives' outcomes into agenthooks
// decisions and nothing else, so the enforcement behavior is exactly the
// inline evaluation Ingest ran before. The runner itself is assembled at the
// cmd callsite (cmd/gram/hook_policies.go), whose registration block is the
// single definition of the run order; the boundary that maps the winning
// decision back onto the wire response lives in the hooks service's
// evaluateCanonicalHook.
package policies
