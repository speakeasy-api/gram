---
"server": minor
---

Add `remoteSessions.getServerIdentityImpact`, which previews the MCP servers and gateways a client binding change on a user session issuer would affect, across every project in the organization for an organization-level issuer. Each server is classified as repoint, clear or resignin from the clients its own project can see, and each gateway as client_removed when it loses a provider client. The preview applies the same refusals as the commit, including the refusal to bind or unbind an organization-level client on an organization-level issuer, and treats clients of a deleted provider the same way. It requires the access the change itself needs. Servers the caller cannot read are only counted, and when the change touches an organization-level client every unreadable server on the issuer is counted.
