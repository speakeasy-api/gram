---
"server": minor
"dashboard": minor
---

Custom domains now support apex/root domains, which cannot carry a CNAME record. The server advertises its static ingress IPs (new `GRAM_CUSTOM_DOMAIN_A_RECORDS` setting) through `domain.list`'s new `dns_config` field and a per-domain `suggested_record_type`, and the setup wizard offers A-record instructions with a CNAME/A toggle. DNS verification tolerates slow propagation: instead of failing fast on missing records, registration polls with capped backoff for up to 24 hours (under a row-scoped workflow identity) and "Check now" wakes the pending check immediately. Routing and health checks judge A-record setups against the configured ingress IPs (flagging stray A or AAAA records), and health emails name the record type that fits the domain.

A default MCP server can now be staged while configuring the domain: `domain.register` creates the pending domain synchronously and returns it, the new `domain.listRootMcpServers` endpoint lists every eligible server in the organization, and `domain.setRootMcpEndpoint` accepts an `mcp_server_id` — attaching the server to the domain (creating its endpoint from the server slug) and mapping it to the root in one call, before DNS cuts over. Migrations from another MCP host can therefore configure everything up front and let cutover converge asynchronously.

Reconciliation no longer marks a domain verified or activated without the TXT ownership proof, closing a path where settings updates could activate an unverified domain.
