---
"server": patch
---

Modified deployment logging so that non-https server urls in openapi documents are logged as warnings instead of errors. These urls do not block deployment processing. They are ignored when present.
