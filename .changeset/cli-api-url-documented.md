---
"cli": patch
---

The `--api-url` flag now appears in help output, and the README explains how to sign in to another Speakeasy host. `speakeasy whoami` now uses the host saved by `speakeasy auth` when `--api-url` and `SPEAKEASY_AI_API_URL` are not set, instead of always checking `https://app.getgram.ai`, and it shows the host it checked (`api_url` in `--json` output).
