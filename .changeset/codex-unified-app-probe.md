---
"server": patch
---

Probe the unified ChatGPT desktop app when installing the Codex plugin
(DNO-737). OpenAI merged the standalone Codex app into the ChatGPT desktop
app, which ships the codex CLI at
`/Applications/ChatGPT.app/Contents/Resources/codex`; the install script only
probed the legacy `/Applications/Codex.app` path, so on any machine with just
the post-merge app it failed to find the binary and degraded to printing
manual instructions. The unified bundle is now probed first, with the legacy
path kept for pre-merge installs.
