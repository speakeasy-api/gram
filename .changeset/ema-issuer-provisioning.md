---
"server": minor
"dashboard": patch
---

Organizations with single sign-on can enable a Speakeasy enterprise authorization issuer: a permanent issuer URL with a dedicated Speakeasy-managed signing key, key rotation, emergency key replacement, a disable switch that keeps the URL, and a platform kill switch. The issuer's keys are never visible to customer key APIs or usable for client authentication. A read-only Platform MCP tool reports the issuer URL.
