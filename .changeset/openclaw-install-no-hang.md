---
"server": patch
---

The OpenClaw observability plugin starts its hooks process on the first hook instead of when OpenClaw loads it, so `openclaw plugins install` and other CLI commands no longer hang after installing it. The agent install command asks for a Gateway restart only when OpenClaw did not load the plugin itself.
