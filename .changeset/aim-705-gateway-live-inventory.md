---
"server": patch
"dashboard": patch
---

Direct gateway `tools/list` now returns the whole tool inventory in one response instead of 100-tool pages, so a member whose tools change between pages can no longer invalidate a listing partway through. A member that is rate limited, briefly unreachable, or returns a server error is retried within the listing's three seconds, honoring `Retry-After`. A member that still cannot be listed is left out so the other members' tools are returned, and the gateway's Activity section records it under "Discovery diagnostics" with when and why. Members the caller cannot see or has not connected are still omitted without a record. The consent screen's tool inventory uses the same three-second budget and retries, so a member slower than that shows as unavailable there.
