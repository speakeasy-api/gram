---
"server": patch
"tunnel": patch
---

Mirror the live routes of tunnels that carry OAuth back-channel calls to the auth Redis role, so tunneled OAuth keeps working while the operations instance is down or being moved. Off until the gateway has an auth endpoint and the server enables `GRAM_TUNNEL_AUTH_ROUTE_MIRROR`.
