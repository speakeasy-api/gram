---
"server": minor
---

Every hosted MCP server with an address now has its own MCP server record and endpoint from the moment it is created or cloned, instead of only after its network access is first edited. Every change to the hosted server (name, slug, enabled and public flags, custom domain, OAuth issuer, tool variations group, and enabling it by attaching it to an assistant) keeps that record and endpoint in step, and deleting the hosted server removes them along with their plugin, gateway, and assistant attachments. A hosted server whose custom domain was deleted no longer gets an endpoint recreated on that domain, and disabling a hosted server that was its custom domain's root route now clears that route.
