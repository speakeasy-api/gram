---
"server": patch
---

The Platform MCP `get_xaa_readiness` tool now also reports the organization's Okta sign-in setup in `okta_sign_in`: whether the AI agent's linked app is registered as the sign-in client and ready, the active public key (JWK, public members only) to paste in the agent's Credentials in Okta and Activate, the sign-in redirect URI, which organization sign-in issuers trust it or still trust a previous agent's client, the next sign-in step, and the first Okta setup checklist step not yet confirmed (skipping steps Speakeasy cannot observe once its side is done). Several clients claiming the agent ID report a distinct `resolve_duplicate_sign_in_clients` step instead of picking one. It stays read-only and organization-admin only; setting up sign-in still happens in the dashboard because it needs a signing key choice and a confirmed trust change.

Recording an AI agent ID that is the connection's own service app client ID is now rejected.
