---
"server": minor
---

Add the `network-gateway` binary: a dedicated deployment that runs one embedded overlay-network node per enabled network ingress (Tailscale via tsnet first, behind a provider abstraction) and reverse-proxies each organization's MCP surface into gram-server carrying the `X-Gram-Netingress-*` forward-token trust headers. Node identity persists in Postgres so replicas resume as the same device on the customer's network; per-ingress Redis leases prepare multi-replica failover. Build with `mise build:network-gateway`.
