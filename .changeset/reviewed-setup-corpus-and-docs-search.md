---
"server": patch
"dashboard": patch
---

Serve reviewed provider setup guides as Platform MCP resources and add `search_gram_docs`, which answers from that pinned corpus with cited excerpts and links back to the full guide. Content past its revalidation date is flagged, then withheld, rather than presented as current, and a query nothing reviewed answers returns `guide_unavailable` instead of invented steps. Documentation citations now render as passages with resource links in the assistant rather than as raw tool output.
