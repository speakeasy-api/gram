---
"server": patch
---

Make external credentials and external keys exercisable in local development.
The real GCP resolver screens customer-supplied service accounts against Gram's
own project, which requires Gram to be running as a user-managed service
account, so on a developer machine every credential and key write failed closed
and the feature could not be run at all. Local development now gets a stub
identity and an in-process KMS signer, selected the same way the billing and
WorkOS stubs already are.

The stand-in signs with RS256 by default, configurable through
`GRAM_LOCAL_KMS_SIGNING_ALGORITHM`. It is deliberately independent of what any
key records, so a key recorded with the other algorithm still reports a
mismatch: agreeing by construction is what the verify probe exists to disprove.
