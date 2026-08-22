---
"server": minor
---

Add the `networkIngress` management API for Private Network Ingress: org admins can configure an embedded overlay-network node (Tailscale first) that serves the organization's MCP endpoints on its own private network. Includes the `network_ingress` product feature (Enterprise entitlement), encrypted credential storage with a write-only rotation endpoint, and audit logging for every mutation. The network gateway that runs the nodes ships separately.
