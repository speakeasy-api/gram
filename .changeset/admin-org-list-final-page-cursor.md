---
"server": patch
---

Stop the admin organizations list from returning a next-page cursor on the last full page. When the number of matching organizations was an exact multiple of the page size, the final page still carried a cursor, so the next page came back empty. The endpoint now reads one row past the page to decide whether a next page exists.
