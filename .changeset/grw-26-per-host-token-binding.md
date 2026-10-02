---
"server": patch
---

Per-endpoint MCP access tokens are now bound to the host that minted them. A token whose `iss` names another host (server URL, an extra platform host such as `ai.speakeasy.com`, or a custom domain) is refused with the usual 401 challenge, so the client refreshes or reauthorizes on the host it is using, and the rejection is counted with the `issuer_mismatch` reason. The authentication host counts as the server URL. Dashboard-minted tokens, tokens without `iss`, private network ingress, and internal callers keep their current behaviour.
