---
"server": patch
---

Default the device-agent command list in the generated observability plugin
`identity.sh` to `speakeasyd`, the binary the daemon actually ships as. The
previous default (`device-agent,speakeasy-device-agent`) never resolved on a
standard install, so identity enrichment was skipped and hook events reached
Gram anonymously (no `user_email`). The fix applies to the Claude Code, Cursor,
and Codex plugin templates. Installs that still use a differently-named binary
can override via `GRAM_DEVICE_AGENT_COMMANDS`.
