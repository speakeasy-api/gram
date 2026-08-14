---
"admin": patch
---

The admin organizations endpoint can now sort and page. A caller names a column, a direction and a 1-based page number, and gets that page back together with the number of organizations the filters matched. Sortable columns are name, slug, account type, member count, created date, disabled date and trial end date; an unknown column or direction falls back to the default order rather than failing a shared link. Cursor paging is untouched and still runs the deployed dashboard, so both walks work until the client half lands.
