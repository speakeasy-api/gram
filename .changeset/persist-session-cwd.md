---
"server": patch
---

Persist the captured agent session's working directory (`session.cwd`) onto chats at hook ingest. The value was already on the wire for canonical (hook.ingest.v1), legacy Claude, and legacy Codex events but previously discarded; it is groundwork for session portability (materializing a moved session into the right project directory).
