---
"server": minor
---

Environment entries can now be marked non-secret so their values stay readable after save. Secret entries keep today's encrypt-and-redact behavior; flipping a secret entry to non-secret requires supplying a new value, while flipping a non-secret entry to secret encrypts the stored value in place. Callers that never send the new is_secret flag behave exactly as before (entries default to secret).
