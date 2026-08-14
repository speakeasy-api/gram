---
"server": patch
---

Extending an enterprise trial now writes an entry to the organization's audit
feed, so it is no longer the one trial lifecycle event that leaves no trace
alongside a trial being armed, demoted and re-armed. The entry names the
Speakeasy team rather than the operator who acted, and carries the end date the
trial held before the extension, the end date it holds now, and the number of
days applied. The write and the entry commit together, so an extension can never
land silently.
