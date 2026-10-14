---
"server": patch
---

After a compromised-key replacement, the issuer's JWKS shows the new key on every server within about a second, and a vendor's rejection of an assertion signed by a recently published key is no longer cached. When every EMA provider fails and one asks for reauthentication, the client gets the reconnect challenge. Sending more than 10 EMA providers returns a readable error.
