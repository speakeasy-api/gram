---
"server": minor
"dashboard": patch
---

Organization names accept punctuation and every script. The old rule allowed
only letters, digits, spaces, hyphens and underscores, which turned away
"Acme, Inc.", "Bob's Bakery", "Café Zoë", and — more importantly — every
company whose name is not written in the Latin alphabet, since a name in
Japanese, Chinese, Korean, Cyrillic, Arabic or Hebrew could not clear the rule
at all. Names are now capped at 100 characters (counted in characters, so a
non-Latin name gets the same room a Latin one does), must carry at least two
letters or numbers, and may use anything that renders: control characters, bidi
overrides and other invisible formatting are still rejected, and whitespace is
normalized. The URL slug is unaffected in shape — it is still derived
separately, with a generated fallback for names that contain fewer than two
URL-safe characters.
