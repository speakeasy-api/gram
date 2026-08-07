---
"server": patch
---

Keep an enforcement block even when its optional links cannot be resolved. The
block URL is handed to the agent before the row is written, so a rejected
insert leaves the user opening a page that does not exist. Enforcement runs
before the hook's chat and finding rows are persisted, so a block early in a
session races its own chat row and the foreign key rejects — silently, since
the insert is detached and only logged. The write now drops whichever link the
database names and retries, so the block always lands and only its enrichment
is lost. Applies to every provider: all block paths share this writer.
