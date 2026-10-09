---
"server": patch
"dashboard": patch
---

Remote MCP servers no longer receive Speakeasy's own request headers. Requests proxied to a remote MCP server used to copy the caller's `Gram-Key`, `Gram-Session`, `Gram-Chat-Session`, `Gram-Project`, `Gram-Consent-*`, tunnel transport and caller assertion headers to the upstream; those are now dropped. A remote server header can no longer be populated from `Authorization` or any of those headers: an existing optional row of that kind is no longer sent, and an existing required one fails each request with an error naming the header until it is changed or removed. Forward an upstream credential in a separate request header, store it as a static value, or configure upstream OAuth instead. Header writes now also reject names that are not valid HTTP field names, values containing control characters, `Set-Cookie` and `Proxy-Authorization` destinations, a `Cookie` populated from the request, and a name that differs from an existing header only in case; new and updated names are stored in canonical form. The dashboard marks existing rows the proxy refuses and explains how to replace them.
