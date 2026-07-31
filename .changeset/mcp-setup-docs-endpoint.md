---
"server": minor
---

Add `mcpRegistries.getSetupDocs`, which returns the published setup documentation for an upstream MCP server from the `github.com/speakeasy-api/mcp-setup-docs/go` catalog. A guide can be located by the upstream server's endpoint URL, by its registry specifier as returned by `listCatalog`, or by both at once. Matches from the two lookup keys are deduplicated by guide slug and returned in descending specificity, and each guide reports how it matched and which documented endpoint the lookup selected. Servers with no published guide return an empty list rather than an error.
