---
"server": patch
---

The MCP consent page marks a service whose access identity chaining is configured to supply as "Managed by your identity provider". The card keeps a secondary "Connect manually" action, and a configured service counts toward enabling "Give access" because the runtime obtains its access on first use; an interactive connection still takes precedence. A card is marked only when exactly one ready binding serves the upstream through that card's authorization server, never when the bindings are ambiguous or the card's existing grant cannot be routed, and a lookup failure leaves the card unmarked instead of failing the page. On gateway endpoints a card is marked only when identity chaining serves every gateway member that uses its authorization server.
