---
"hooks": patch
---

Look for the codex binary inside the unified ChatGPT app when resolving a
Codex user's email. OpenAI merged the standalone Codex app into the ChatGPT
app, so on a machine that has only the unified app and no `codex` on PATH the
relay could not shell out to `codex app-server` and fell through to the
`auth.json` and payload fallbacks. The install script already learned this
path; the relay probe had not.
