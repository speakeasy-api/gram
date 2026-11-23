---
"server": patch
---

The tailnet gateway can give each tsnet node a fixed UDP port from a range (`GRAM_TAILNET_GATEWAY_TSNET_PORT_BASE`), so a deployment can publish the range and clients behind strict NATs reach the gateway directly instead of through a DERP relay. tsnet's own log now goes to a child logger tagged `gram.component=tsnet`, with endpoint and path changes at info.
