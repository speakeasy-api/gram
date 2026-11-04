---
"server": patch
"dashboard": patch
---

Capture Claude sessions from the unified Chat/Cowork mode, where Claude runs plugin hooks only inside the Cowork VM. The plugins page offers a new Claude Cowork download, built for Claude's admin plugin settings. That package registers only the Stop hook and sets `transcript_only` in its `speakeasy.json`. Each time Stop fires, `speakeasy-hooks` uploads the transcript lines written since its last successful upload to the new `POST /transcript` endpoint, and relays no other event. The server stores those lines as the session's chat history, keyed by transcript entry, so a re-sent line is never stored twice. Only the packaged `speakeasy.json` can turn this mode on; no environment variable does, because the mode switches policy enforcement off.
