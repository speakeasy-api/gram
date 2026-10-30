---
"server": minor
"dashboard": patch
---

Remove the deprecated `toolsets.setUserSessionIssuer` endpoint, which reached its sunset date. Link a sign-in provider with `mcpServers.update` and `user_session_issuer_id`, and detach it with `clear_user_session_issuer`.
