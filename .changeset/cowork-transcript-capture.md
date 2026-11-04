---
"server": patch
---

Capture Claude sessions from the unified Chat/Cowork mode, where Claude runs plugin hooks only inside the Cowork VM. When the standard Claude observability plugin runs there, `speakeasy-hooks` now relays no per-event hook and instead, each time Stop fires, uploads the transcript lines written since its last successful upload to the new `POST /transcript` endpoint. The server stores those lines as the session's chat history, keyed by transcript entry, so a re-sent line is never stored twice. The binary recognizes the Cowork VM from Claude Code's entrypoint, so the same plugin keeps relaying per-event hooks, and enforcing policy, in Claude Code. Policy is not enforced inside the Cowork VM.
