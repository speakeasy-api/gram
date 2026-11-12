---
"server": patch
---

In an organization that enforces upstream auth, Speakeasy now decides a connection's upstream choices from the auth clients each server offers, and no longer from the older upstream OAuth client associations. A server whose OAuth offer was removed now works without authentication even when an old association is left behind, and a client offered to a server is recorded for it even when no association was ever made. This applies to new sign-ins, dashboard sessions, gateway reviews, and gateway discovery. Automatic choices still only use clients bound to the session's sign-in issuer. The upstream OAuth migration report and the choice backfill read the same state, so on an enforcing organization they no longer report these connections as gaps.
