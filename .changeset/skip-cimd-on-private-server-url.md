---
"server": patch
---

Connecting an upstream identity provider no longer registers through a Client ID Metadata Document that the provider cannot fetch. When Gram's own URL is loopback or private, as in local development, a public provider such as a hosted MCP server's authorization server cannot reach the document, and every sign-in failed with a 403 from the provider. Registration now falls back to dynamic client registration in that case, while a provider on the same private network, such as the dev identity provider, still uses the metadata document.
