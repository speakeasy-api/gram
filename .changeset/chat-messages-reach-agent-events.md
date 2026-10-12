---
"server": patch
---

Every chat message Speakeasy captures, whether from agent hooks, the Anthropic inference hook, compliance imports or assistants, now reaches session analytics through the same telemetry pipeline as agent-exported events, keyed by session, role and text so a message seen by two channels counts once.
