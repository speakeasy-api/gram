---
"dashboard": patch
---

Fix natural-language input in the dashboard time range picker. The picker's
"type any date" parsing POSTs to `/chat/completions`, which requires both
`Gram-Session` and `Gram-Project` headers, but most pages rendered the picker
without a `projectSlug`, so the request 401ed and parsing silently did nothing.
The `DashboardTimeRangePicker` wrapper now injects the request project slug
via `useProjectSlugForRequests()` (callers can still override it), fixing the
project home, MCP overview, security overview, watchdog, and risk overview
pages in one place — and org-scoped pages like Billing through the same
default-project fallback every other SDK request uses.
