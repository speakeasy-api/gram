---
"server": minor
"dashboard": patch
---

Freeze external key identity: `externalKeys.updateAwsKms` and `externalKeys.updateGcpKms` no longer accept `key_arn` / `resource_name` or `algorithm` and cover only `name`, `external_credential_id` and `customer_grant_reference`, so changing what a key is now means deleting it and creating a new one (a breaking change to those two methods). Deleting a key is refused with a conflict while a JSON Web Key Set or published key still references it, and `createGcpKms` now requires a fully-qualified crypto key version path.
