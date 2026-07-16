---
"server": minor
---

Distribute observability hooks through a pinned, checksum-verified Go binary bootstrapper. The one-time binary install is capped at 45 seconds and runs in the background wherever the agent supports asynchronous hooks. When the binary can't be installed on a developer machine, the outcome follows the org's "Fail Open During Outages" setting: fail open lets hook events pass, the fail-closed default blocks per provider semantics. The binary downloads from your Speakeasy server domain — the same domain hooks already send telemetry to — so restricted or sandboxed developer environments only ever need that one domain allowed.
