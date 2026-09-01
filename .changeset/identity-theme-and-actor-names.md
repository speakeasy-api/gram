---
"dashboard": patch
"server": patch
---

Three fixes to shared chrome. Identity tints (avatar initials) now follow the theme instead of always emitting light-theme values, which made them the brightest thing on a dark page. The sidebar's current page keeps a marker of its own, so hovering elsewhere no longer removes the only sign of where you are, and the collapsed icon rail shows one at all. Audit log actor names resolve against the directory at read time: every writer stores the acting user's email, so the feed and its actor filter both read as columns of addresses.
