---
"server": minor
---

Add `mcpRegistries.getSetupDocs`, which returns the published setup documentation for an upstream MCP server from the `github.com/speakeasy-api/mcp-setup-docs/go` catalog. A guide can be located by the upstream server's endpoint URL, by its registry identifier (a Pulse alias, a provenance name, or the canonical `slug/remote-id` ref), or by both at once — matches from the two lookup keys are deduplicated by guide slug, and each guide reports how it matched and which of its documented endpoints the lookup selected. Servers with no published guide return an empty list rather than an error.
