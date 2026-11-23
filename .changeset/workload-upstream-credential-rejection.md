---
"server": patch
---

When the upstream behind a remote or tunneled MCP server rejects the credential a workload uses on its owner's behalf, the workload now gets an error that names the server and the upstream's reason, instead of a Speakeasy sign-in challenge. The workload's Speakeasy token is still valid, so it no longer exchanges a new one and retries into the same rejection. The error carries no credential, and the rejection is recorded against the server in its connection attempts. People signing in to the same server still get the sign-in challenge.
