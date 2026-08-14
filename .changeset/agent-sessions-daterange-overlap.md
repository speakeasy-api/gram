---
"server": patch
---

Keep actively-running agent sessions listed under a date-range filter. The
chat list previously required a session's newest message to fall inside the
requested range, so a session that logged a message after the client's frozen
`to` bound vanished from the Agent Sessions page until the range was
re-selected. The range test is now interval overlap: last activity after the
range opens and created before it closes.
