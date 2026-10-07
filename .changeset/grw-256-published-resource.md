---
"server": patch
---

Connecting to a remote MCP server now sends the RFC 8707 resource indicator the server publishes in its RFC 9728 metadata whenever it differs from the registered URL only in trailing slashes. A server registered as `https://host` that publishes `https://host/`, or the reverse, no longer rejects sign-in. When the metadata cannot be read within three seconds or names a different resource, Gram sends the registered URL as before.
