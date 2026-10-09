---
"server": minor
---

Remote session clients can now be created with `credential_owner: self`, meaning the client holds one upstream credential for itself (the `client_credentials` grant) instead of each caller connecting their own account. Using that credential when proxying requests comes in a later release. A `self` client needs a client secret or a key set, and an issuer with a token endpoint, which can be entered by hand without metadata discovery. Both create methods also accept `json_web_key_set_id`, so a `private_key_jwt` client can be created in one call.
