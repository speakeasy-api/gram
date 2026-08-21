---
"server": patch
---

The consent page's connect and refresh actions now derive the RFC 8707 resource per remote session client instead of reusing the single endpoint-level resource. Under multi-binding the endpoint-level value belongs to at most one client, so reusing it sent the wrong resource to the wrong upstream authorization server and recorded wrong `remote_sessions.resource` values, breaking resource-matched token routing. A client with no unambiguous upstream now sends and records no resource rather than a wrong one.
