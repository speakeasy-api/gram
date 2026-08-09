---
"server": patch
---

Reject organization names that contain fewer than two letters or numbers. A name made up entirely of punctuation, such as `-----` or `___`, previously passed validation and produced an organization whose URL slug was empty. The rule is enforced by the shared name check, so it covers both the sign-up parameter on login and the register endpoint, including requests that skip the sign-up form. Existing organizations are unaffected.
