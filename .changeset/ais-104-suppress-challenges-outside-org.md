---
"server": patch
---

The Challenge UI now suppresses challenges raised by users outside the organization. Previously, when a Speakeasy staff member impersonated a customer org their authz decisions appeared as challenge entries — and because internal users switch accounts frequently, these entries repeatedly cluttered the list. `access.listChallenges` and `access.listChallengeBuckets` now only return challenges whose principal is an active member of the organization or has no Gram user identity (e.g. API keys and external end-users); challenges from Gram users who are not members of the org are filtered out in ClickHouse so counts and pagination stay correct.
