---
"dashboard": patch
---

Setting up an agent that runs on a platform now uses the MCP authentication provider its servers share. The token endpoint and audience come from that provider, where the wizard used to preselect the organization's first shared provider whatever the servers used, and the servers then refused the token. When a server uses its own per-server provider, or the agent's servers are split across providers, the Credential step names each one with a link to its authentication settings and won't continue until they share one, so no agent is created with a token its servers refuse. GitHub Actions jobs are unaffected: the device agent gets their tokens from each server.
