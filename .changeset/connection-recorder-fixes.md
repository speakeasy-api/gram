---
"server": patch
---

Connection history fixes:

- Token requests from Claude Code, Claude, ChatGPT and Codex now show the app's name instead of "other". These apps send a generic User-Agent, so Speakeasy names them from their verified client ID. A script that borrows a local app's client ID still shows as a script, and a client that names itself something else shows as "other".
- A tunneled gateway member that times out after its server started answering is now recorded as unreachable instead of as a Speakeasy internal error. A timeout before the tunnel passed on any answer is still recorded as a Speakeasy internal error.
- Rows written later for held-back repeat requests keep their request's trace id.
- A failed provider refresh no longer waits up to 5 seconds on a binding lookup or the logs setting, and skips the lookup for organizations known to have logs off.
- On shutdown, Speakeasy finishes writing connection rows before ClickHouse closes, and counts rows it could not write as dropped.
