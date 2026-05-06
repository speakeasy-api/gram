---
"server": minor
---

Implement `userSessions.{list,revoke}` per spike §6.1: list returns the issued-session registry filtered by principal_urn / user_session_issuer_id, revoke soft-deletes the row and pushes its jti into the unified `chat_session_revoked:{jti}` cache. `refresh_token_hash` is excluded at the repo projection so it cannot reach the management API surface.
