---
"server": minor
---

Session portability can now mint short-lived capability URLs for handoff documents. The device agent uploads the handoff it rendered locally via the new `agent.createSessionHandoff` endpoint (per-user key only) and receives a burn-after-read URL served at `/shared/handoffs/{token}` — letting a cloud agent or another machine continue the session. Links expire after a clamped TTL (default 15 minutes), die on first read, and every mint lands as a content-free `chat_session:handoff_export` audit event.
