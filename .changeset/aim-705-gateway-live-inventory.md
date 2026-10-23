---
"server": patch
---

Direct gateway `tools/list` now returns the whole tool inventory in one response instead of 100-tool pages, so a member whose tools change between pages can no longer invalidate a listing partway through. When a member does not answer, for example because its upstream is rate limited, times out, or returns a failed or unreadable response, the listing fails so the client can retry, instead of succeeding without that member's tools. Members the caller cannot see or has not connected are still omitted.
