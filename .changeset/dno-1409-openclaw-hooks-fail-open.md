---
"server": patch
---

OpenClaw hooks honor the configured install-failure policy when the relay is unavailable, include bootstrap diagnostics in blocked calls, and restart failed relays with bounded backoff. Broken pipes and missing launch commands no longer crash the Gateway. Explicit policy denies still block under either setting.
