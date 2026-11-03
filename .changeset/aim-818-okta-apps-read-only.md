---
"server": patch
---

Okta connections now request only the okta.apps.read scope. Speakeasy no longer asks for okta.users.read or okta.groups.read, which it did not use beyond verifying the connection.
