---
"server": minor
---

The Speakeasy server binary (server, mcp, worker, streams, admin and network-ingress-server) reads the Speakeasy staff organization from a `GRAM_STAFF_ORGANIZATION_ID` setting instead of a hardcoded organization. Outside local development each process refuses to start when it is missing or matches no organization in its database. Audit log staff masking, the organization whitelist endpoint, OpenRouter credit limit exemption, daily connection outcomes and the hooks rollout canary (in the plugin publisher and the admin dashboard) all use it. The Vanta evidence sink sends the deployment's HTTPS server origin as each resource's `externalUrl`.
