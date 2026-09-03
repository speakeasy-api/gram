---
"dashboard": patch
---

Gateway endpoints: fix dashboard issues found in prod E2E testing. Deleting a tunneled MCP source no longer leaves its confirm dialog stuck on "Deleting…" — the mutation no longer blocks on refetching the just-deleted resource, so the dialog closes and navigates on success. The gateway add-member sheet no longer implies unproxied or slugless servers can be added: the copy is corrected and their Add button is disabled (the backend rejects them), while disabled servers can still be added but stay excluded from serving.
