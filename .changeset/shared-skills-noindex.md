---
"dashboard": patch
---

Send a robots noindex signal for public share pages: nginx now adds
`X-Robots-Tag: noindex, nofollow` to `/shared/*` responses, and the shared
skill page injects a matching robots meta tag at runtime.
