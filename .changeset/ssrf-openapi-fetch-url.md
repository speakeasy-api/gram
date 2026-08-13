---
"server": patch
---

Harden OpenAPI-from-URL (and in-process image-from-URL) fetches against SSRF. User-supplied URLs are rejected unless they are https with a host that passes the guardian blocklist (loopback, RFC 1918, link-local/metadata, and other reserved ranges), and redirects are capped and re-checked so a hostile target cannot chain into private address space or downgrade to plaintext HTTP.
