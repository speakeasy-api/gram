---
"server": patch
---

Connection history now names the client, such as Claude Code, Claude, ChatGPT or Codex, when a request's User-Agent doesn't. It uses the app's verified client ID instead. Token requests from these apps send a generic User-Agent, so those rows used to show "other". A script that borrows an app's client ID from a local program still shows as a script.
