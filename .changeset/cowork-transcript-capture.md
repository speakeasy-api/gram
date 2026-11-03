---
"server": patch
---

Capture Claude sessions from the unified Chat/Cowork mode, where Claude runs plugin hooks only inside the Cowork VM. `speakeasy-hooks` gains a transcript-only mode, switched on by `transcript_only` in `speakeasy.json`. In this mode it handles only the Stop hook and, each time Stop fires, uploads the transcript lines written since its last successful upload to the new `POST /hooks/claude-transcript` endpoint. The server stores those lines as the session's chat history, keyed by transcript entry, so a re-sent line is never stored twice. No plugin sets the flag yet; the Cowork plugin package follows separately.
