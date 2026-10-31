---
"server": patch
---

The agent install script asks to store `SPEAKEASY_AGENT_KEY` only when it configured Claude Code or Codex, which read the key from the environment. An OpenClaw-only install keeps the key in its own config, so the script no longer prints that note.
