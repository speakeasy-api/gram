---
"server": minor
"dashboard": minor
---

Cross App Access readiness now reflects what identity chaining exchanges observe. A confirmed server reads Verified once an exchange succeeds, returns to Not confirmed when Okta rejects the target, and reads Not working when Okta refuses the scopes or the agent app's authentication, or the server's authorization server refuses Okta's assertion. A confirmation whose issuer URL does not match the server's authorization server now reads Not working until an exchange succeeds, including existing confirmations. Result changes are recorded in the audit log as okta-resource-connection:observe, and confirming again clears an earlier result.
