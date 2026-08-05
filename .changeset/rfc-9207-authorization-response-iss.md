---
"server": minor
---

The user-session OAuth authorization server now emits the RFC 9207 `iss` parameter on every authorization response, success and error alike, and advertises `authorization_response_iss_parameter_supported` in its metadata document. This satisfies the MCP 2026-07-28 Authorization Response Validation requirement and lets MCP clients holding concurrent flows against several authorization servers detect a mix-up attack.
