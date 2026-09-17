---
"admin": patch
---

Rename the organization meter usage helpers so they no longer share a case-insensitive stem with the `MeterUsage` component, which made the production build resolve `./MeterUsage` to the helper module on macOS checkouts.
