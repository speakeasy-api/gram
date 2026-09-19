---
"dashboard": patch
---

Selecting a risk finding's row in Risk Events or a Risk Overview category now opens the session transcript on that finding's own message — highlighted, with a strip naming the finding and a "Jump to message" control — instead of dropping the reader at the session's first flagged turn. When the matched value is withheld because the viewer lacks the `chat:read` scope, the strip and the flagged message both say so, so a transcript with no highlighted span no longer reads as if nothing was found.
