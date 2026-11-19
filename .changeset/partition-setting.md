---
"server": minor
"tunnel": minor
---

The Speakeasy server binary (every command, including mcp, worker, streams and admin), pystreams and the tunnel gateway read a `GRAM_PARTITION` setting (`us` or `eu`, default `us`) naming the partition they run in. Each rejects an unknown or empty value at startup and tags its logs with `gram.partition`; the server and pystreams also put it on their traces and metrics. Nothing else changes yet: existing deployments run as `us`.
