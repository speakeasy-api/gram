---
"server": minor
"dashboard": patch
---

Device integrations: enabling a connection (and "Sync now") triggers the
sync coordinator immediately instead of waiting for its next tick; the
configure sheet disables the connection test while the draft has unsaved
changes and explains that tests run against saved credentials; managed
device and schedule tables get properly spaced empty states; and the Iru
provider rejects the tenant console URL with an error naming the correct
API URL.
