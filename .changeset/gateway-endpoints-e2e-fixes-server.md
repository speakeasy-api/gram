---
"server": patch
---

Gateway endpoints: fix issues found in prod E2E testing. Removing a gateway member now unbinds that member's upstream identity provider from the gateway's user-session issuer once no remaining consumer of that issuer still fronts the upstream, so a removed member's provider no longer lingers on the consent screen (correct even when an issuer is shared across gateways). A malformed (unparseable) JSON-RPC request body now returns the spec's parse-error code (-32700) rather than invalid-request (-32600). The consent screen shows a helper line explaining that access is disabled until a service is connected.
