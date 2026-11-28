---
"server": minor
"admin": minor
---

Add a bulk disable to the admin organizations page. Upload a CSV with an `id` or `slug` column, review the matched organizations (id, slug, name, account type, and whether each is already disabled) along with any the file named that could not be found, then confirm. Each confirmed organization is removed from the whitelist and disabled in a single transaction, with the same audit events as disabling one at a time.
