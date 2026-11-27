---
"server": patch
"tunnel": patch
---

Adds a shared Redis endpoint connector for Go (`tunnel/redisconn`) and pystreams (`pystreams.deps.redisconn`). An endpoint names its address, how it authenticates (nothing, a password, or a Google Cloud IAM access token sent as the `default` user), and an optional CA certificate that turns on TLS 1.2+ with server name verification. IAM tokens are cached and refreshed five minutes before expiry; an IAM endpoint without TLS is refused. Nothing uses the connector yet, so running processes are unchanged.
