---
"server": patch
---

Fix chat search pagination when a chat also has activity outside the selected time range or filters. Oldest-first paging could show the same chat on every page and never reach the rest, and newest-first paging could stop early. Next-page cursors now carry the exact start time the page ended on; cursors issued before this change keep working.
