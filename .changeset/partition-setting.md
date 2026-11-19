---
"server": minor
"tunnel": minor
---

The Speakeasy server binary (every command, including mcp, worker, streams and admin), pystreams and the tunnel gateway require a `GRAM_PARTITION` setting (`us` or `eu`) naming the partition they run in. There is no default: each refuses to start when it is missing, empty or unknown, so a deployment cannot silently run as the wrong partition. Each tags its logs with `gram.partition`; the server and pystreams also put it on their traces and metrics. `gram version` and help output work without it. Local development sets `GRAM_PARTITION=us` in `mise.toml`.
