---
"dashboard": patch
---

The admit dialog now owns a wildcard rule's trailing `*` instead of asking for it twice. Selecting Wildcard is the only place breadth is stated: the field holds the stem, the terminator is shown after it, and the composed value is what gets validated and stored. Pasting a rule that already carries its terminator switches the match kind rather than storing a literal star that matches nothing — except under an issuer that forbids wildcards, where there is nothing to switch to and the warning still stands.
