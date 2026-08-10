---
"server": minor
---

Add session-portability endpoints to the agent service: `agent.getSessionMeta` resolves picker metadata (title, chat id, last activity) for captured sessions the calling user owns — per-user keys only, with personal-account sessions excluded — and `agent.reportSessionMoved` records a content-free `chat_session:move` audit event when the device agent moves a session to another harness. Both are gated behind the new `session_portability` product feature.
