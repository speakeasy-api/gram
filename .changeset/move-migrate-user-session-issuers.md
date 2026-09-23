---
"server": minor
---

Allow organization administrators to move user-session issuers between project and organization scope, or consolidate one issuer into another after reviewing an impact and conflict preflight. Migration preserves clients, sessions, consents, CIMD allowlists, remote-session credentials, and attached resources while auditing the source retirement. Access tokens bound to the source issuer are rejected by repointed servers until the client refreshes, which succeeds against the migrated session. Client registration, session minting, refresh rotation, consent, and remote-login callbacks now lock and recheck the issuer they write under, so a write that raced a migration or delete is rejected instead of landing on the retired issuer.
