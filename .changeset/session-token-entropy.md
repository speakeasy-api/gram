---
"server": patch
---

Dashboard session tokens are now generated from 256 bits of `crypto/rand` entropy (base64url-encoded) instead of a v4 UUID. Session tokens are bearer credentials validated by a bare cache lookup, so they must be unguessable; a UUID carries only 122 bits of entropy in a recognizable, structured format and is not intended for use as a security token. Existing sessions remain valid, this only affects newly issued tokens.
