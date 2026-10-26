---
"server": minor
"dashboard": minor
---

Organization admins can set the Organization Gateway's default tool discovery mode (Progressive, Direct, or Code Mode) from a new **Settings** tab on the Organization Gateway page. `organizations.getGateway` now returns `discovery_mode` and `code_mode_enabled`, and `organizations.setGatewayDiscoveryMode` sets the mode with `org:admin` and records an `organization:gateway_discovery_mode_updated` audit event. Code Mode can be chosen only while the organization has the Code Mode entitlement and the code runtime is available; a saved Code Mode default serves Progressive while either is missing. Connections that keep the gateway default at sign-in get the organization's default, the consent page labels it "Gateway default (<mode>)", and existing connections keep their mode until they reconnect. The Platform MCP's `get_my_organization_gateway` reports the default as `default_discovery_mode`. Existing organizations keep Progressive.
