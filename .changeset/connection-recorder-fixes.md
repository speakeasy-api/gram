---
"server": patch
---

Connection history fixes:

- Token requests from Claude Code, Claude, ChatGPT and Codex now show the app's name instead of "other". These apps send a generic User-Agent, so Speakeasy names them from their verified client ID. A script that borrows an app's client ID from a local program still shows as a script.
- A tunneled gateway member that times out is now recorded as unreachable instead of as a Speakeasy internal error.
- Rows written later for held-back repeat requests keep their request's trace id.
- A failed provider refresh no longer waits up to 5 seconds on a binding lookup, and skips it for organizations with logs off.
- On shutdown, Speakeasy finishes writing connection rows before ClickHouse closes, and counts rows it could not write as dropped.
