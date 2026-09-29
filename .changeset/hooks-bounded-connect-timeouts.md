---
"hooks": patch
---

Stop an unreachable Gram control plane from stalling coding agents. Connection setup (DNS, TCP and TLS) is now bounded at 2s instead of `http.DefaultTransport`'s 30s, which is six times the 5s gate budget and left no room for the transport replays the relay already had, so a gating hook decided the event on a single attempt. Transport replays are also owned by one bounded loop now rather than stacking under the SDK's own connection retries: an observed event such as `PostToolUse` used to hold the agent for the full 45s send budget across sixteen connection attempts, and now costs three bounded attempts. When the attempts do run out, the block explains that Gram was unreachable and why, instead of reporting `HTTP 0`.
