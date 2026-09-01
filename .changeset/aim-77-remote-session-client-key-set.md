---
"server": minor
"dashboard": minor
---

Attach a JSON Web Key Set to a remote session client. `remoteSessionClients` and `organizationRemoteSessionClients` each gain `attachKeySet` and `detachKeySet`, which set and clear the client's `json_web_key_set_id` behind the `customer_managed_encryption_keys` entitlement; the rest of client management stays ungated. The set must belong to the client's organization, a client that declares `private_key_jwt` cannot be left without one, and `jsonWebKeySets.delete` now refuses a set a live client still references, with `jsonWebKeySets.getDeletePreflight` reporting the clients that would block it.
