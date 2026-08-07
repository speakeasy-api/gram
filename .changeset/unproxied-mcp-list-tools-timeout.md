---
"server": patch
---

Fix a live tool-listing probe for unproxied MCP servers taking up to a minute or more against an unreachable vendor server instead of the intended ~10 second bound. Two independent retry layers (HTTP-transport retries and the MCP SDK's own reconnect retries) compounded on top of each other, and the SDK's own cleanup after a context deadline could itself run well past that deadline. The probe now disables both retry layers for this one-shot check and time-boxes the response to the caller independently of how long the SDK's internal cleanup takes.
