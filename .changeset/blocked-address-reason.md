---
"server": patch
---

Add two MCP connection reason codes, `address_not_allowed` and `address_not_found`, fixed by an admin, for a server or sign-in service whose address Speakeasy refuses or that does not exist. Failures recorded until now as provider outages move to these codes. End users get a plain message that the server can't connect because of how it is set up and to ask an administrator; administrators get the reason on the connection row and in the sign-in error page's technical details. Status codes and error types are unchanged; a gateway no longer retries listing a member whose name does not exist.
