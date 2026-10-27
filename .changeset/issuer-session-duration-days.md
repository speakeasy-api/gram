---
"server": patch
---

Signing in through a user session issuer no longer fails with a server error when the issuer's session length is stored in days. Days count as 24 hours when minting and refreshing sessions, in the consent screen's session-length choices, and in the issuer details. Months are still refused because they have no fixed length. The seeded partner gateway issuer now stores its 30-day session length in hours.
