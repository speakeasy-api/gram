---
"server": minor
---

Distribute observability hooks through a pinned, checksum-verified Go binary bootstrapper. Organizations can choose an install-failure policy: a new "Allow on Install Failure" setting lets hook events pass when the binary can't be installed on a developer machine, instead of the default fail-closed behavior. In observability mode no hook can block or fail an agent action — install failures always pass — and the one-time binary install is capped at 45 seconds and runs in the background wherever the agent supports asynchronous hooks. The binary downloads from your Speakeasy server domain — the same domain hooks already send telemetry to — so restricted or sandboxed developer environments only ever need that one domain allowed.
