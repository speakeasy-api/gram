---
"server": patch
---

Add two MCP connection reason codes, `address_not_allowed` and `address_not_found`, fixed by an admin, for a server or sign-in service whose address Speakeasy refuses or that does not exist. Failures recorded until now as provider outages move to these codes. End users are told in plain words whether the server's or its sign-in's address is private or internal or doesn't exist, that waiting won't fix it, and what to ask an administrator to fix; administrators get the exact reason on the connection row and in the sign-in error page's technical details. Status codes and error types are unchanged; a gateway no longer retries listing a member whose name does not exist.
