---
"server": patch
---

The OpenClaw observability plugin now lets tool calls and agent runs through when the Speakeasy hooks relay is unavailable, and prints one warning to stderr, instead of blocking every call. Verdicts the relay returns, including policy blocks, still block.
